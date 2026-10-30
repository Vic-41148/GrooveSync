// Package writeback plans and guards state reconciliation before any remote write.
package writeback

import (
	"fmt"
	"time"
)

// RolloutMode controls whether a plan writes anything.
type RolloutMode string

const (
	ModeOff    RolloutMode = "off"
	ModeShadow RolloutMode = "shadow"
	ModeLive   RolloutMode = "live"
)

// State is the like state tracked for a service.
type State string

const (
	StateUnknown State = "unknown"
	StateLiked   State = "liked"
	StateUnliked State = "unliked"
)

// Snapshot captures the three-way comparison for one item.
type Snapshot struct {
	Provider   string    `json:"provider"`
	ItemID     string    `json:"item_id"`
	Base       State     `json:"base"`
	Local      State     `json:"local"`
	Remote     State     `json:"remote"`
	LocalAt    time.Time `json:"local_at"`
	RemoteAt   time.Time `json:"remote_at"`
	Confidence float64   `json:"confidence"`
}

// Action is the outcome of a reconcile step.
type Action string

const (
	ActionNoop           Action = "noop"
	ActionPushLike       Action = "push_like"
	ActionPushUnlike     Action = "push_unlike"
	ActionPullLike       Action = "pull_like"
	ActionPullUnlike     Action = "pull_unlike"
	ActionConflictNewest Action = "conflict_newest_wins"
)

// PlanItem records one decision and whether it would write remotely.
type PlanItem struct {
	Provider    string
	ItemID      string
	Action      Action
	Write       bool
	Reason      string
	WinnerAt    time.Time
	LocalAt     time.Time
	RemoteAt    time.Time
	Confidence  float64
	NeedsReview bool
	UndoBefore  State
	UndoAfter   State
}

// Plan summarizes a reconcile pass.
type Plan struct {
	Mode       RolloutMode
	Items      []PlanItem
	Warnings   []string
	WouldWrite bool
	DryRun     bool
}

// Reconcile returns the action for one snapshot and a plan item.
func Reconcile(s Snapshot, confidenceThreshold float64, mode RolloutMode) PlanItem {
	item := PlanItem{
		Provider:   s.Provider,
		ItemID:     s.ItemID,
		Confidence: s.Confidence,
		UndoBefore: s.Base,
	}

	if confidenceThreshold < 0 {
		confidenceThreshold = 0
	}
	if s.Confidence < confidenceThreshold {
		item.NeedsReview = true
	}

	switch {
	case s.Base == s.Local && s.Base == s.Remote:
		item.Action = ActionNoop
		item.Reason = "all states already agree"
		item.UndoAfter = s.Local
	case s.Base == StateLiked && s.Local == StateLiked && s.Remote == StateUnliked:
		item.Action = ActionPullLike
		item.Reason = "remote removed it, pull local like state"
		item.Write = mode == ModeLive
		item.UndoAfter = StateLiked
	case s.Base == StateLiked && s.Local == StateUnliked && s.Remote == StateLiked:
		item.Action = ActionPushUnlike
		item.Reason = "local removed it, push unlike"
		item.Write = mode == ModeLive
		item.UndoAfter = StateUnliked
	case s.Base == StateUnliked && s.Local == StateLiked && s.Remote == StateUnliked:
		item.Action = ActionPushLike
		item.Reason = "local added it, push like"
		item.Write = mode == ModeLive
		item.UndoAfter = StateLiked
	case s.Base == StateUnliked && s.Local == StateUnliked && s.Remote == StateLiked:
		item.Action = ActionPullLike
		item.Reason = "remote added it, pull like"
		item.Write = mode == ModeLive
		item.UndoAfter = StateLiked
	case s.Local != s.Remote:
		item.Action = ActionConflictNewest
		item.Reason = "local and remote changed differently, newest timestamp wins"
		item.Write = mode == ModeLive
		if s.RemoteAt.After(s.LocalAt) {
			item.WinnerAt = s.RemoteAt
			item.UndoAfter = s.Remote
		} else {
			item.WinnerAt = s.LocalAt
			item.UndoAfter = s.Local
		}
	default:
		item.Action = ActionNoop
		item.Reason = "no reconcile action needed"
		item.UndoAfter = s.Local
	}

	if item.NeedsReview {
		item.Write = false
	}
	if mode != ModeLive {
		item.Write = false
	}
	return item
}

// BuildPlan reconciles a batch and applies the safety rails.
func BuildPlan(snaps []Snapshot, confidenceThreshold float64, mode RolloutMode, baselineCount, fetchedCount int) (Plan, error) {
	plan := Plan{Mode: mode, DryRun: mode != ModeLive}
	if err := GuardMassChange(baselineCount, len(snaps), 0.70); err != nil {
		return Plan{}, err
	}
	if err := GuardPartialFetch(baselineCount, fetchedCount, 0.70); err != nil {
		return Plan{}, err
	}
	for _, snap := range snaps {
		item := Reconcile(snap, confidenceThreshold, mode)
		if item.NeedsReview {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s/%s below confidence threshold", item.Provider, item.ItemID))
		}
		if item.Write {
			plan.WouldWrite = true
		}
		plan.Items = append(plan.Items, item)
	}
	return plan, nil
}

// GuardMassChange refuses a merge that would drop too much of the baseline.
func GuardMassChange(baselineCount, survivingCount int, maxDropFraction float64) error {
	if baselineCount <= 0 {
		return nil
	}
	if survivingCount < 0 {
		return fmt.Errorf("writeback: surviving count must not be negative")
	}
	dropFraction := float64(baselineCount-survivingCount) / float64(baselineCount)
	if dropFraction > maxDropFraction {
		return fmt.Errorf("writeback: refusing merge, %.0f%% drop exceeds %.0f%% limit", dropFraction*100, maxDropFraction*100)
	}
	return nil
}

// GuardPartialFetch refuses to infer remote deletions from too small a fetch.
func GuardPartialFetch(previousSyncedCount, fetchedCount int, minRetainedFraction float64) error {
	if previousSyncedCount <= 0 {
		return nil
	}
	if fetchedCount < 0 {
		return fmt.Errorf("writeback: fetched count must not be negative")
	}
	retainedFraction := float64(fetchedCount) / float64(previousSyncedCount)
	if retainedFraction < minRetainedFraction {
		return fmt.Errorf("writeback: partial fetch retained %.0f%%, below %.0f%% minimum", retainedFraction*100, minRetainedFraction*100)
	}
	return nil
}

// Tombstone keeps failed removals from resurrecting.
type Tombstone struct {
	Provider   string
	ExternalID string
	ExpiresAt  time.Time
}

// Active reports whether the tombstone still blocks resurrection.
func (t Tombstone) Active(asOf time.Time) bool {
	if t.ExpiresAt.IsZero() {
		return false
	}
	return asOf.Before(t.ExpiresAt)
}

// Renew extends the tombstone by the provided TTL from the current time.
func (t Tombstone) Renew(now time.Time, ttl time.Duration) Tombstone {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if ttl <= 0 {
		return t
	}
	t.ExpiresAt = now.UTC().Add(ttl)
	return t
}

// WriteLog records a write and keeps the before/after state for undo.
type WriteLog struct {
	ID          string
	Provider    string
	CanonicalID string
	Operation   string
	Before      State
	After       State
	Confidence  float64
	BatchID     string
	At          time.Time
}

// Undo returns the reverse transition for the write.
func (w WriteLog) Undo() (State, State, bool) {
	if w.Before == w.After {
		return w.Before, w.After, false
	}
	return w.After, w.Before, true
}

// UndoScope identifies a single write or a whole batch.
type UndoScope struct {
	WriteID string
	BatchID string
}
