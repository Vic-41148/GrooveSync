package writeback

import (
	"testing"
	"time"
)

func TestReconcile(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		snap  Snapshot
		mode  RolloutMode
		want  Action
		write bool
	}{
		{
			name:  "remote removed liked item",
			snap:  Snapshot{Provider: "spotify", ItemID: "1", Base: StateLiked, Local: StateLiked, Remote: StateUnliked, RemoteAt: asOf.Add(time.Minute)},
			mode:  ModeLive,
			want:  ActionPullLike,
			write: true,
		},
		{
			name:  "local added item",
			snap:  Snapshot{Provider: "spotify", ItemID: "2", Base: StateUnliked, Local: StateLiked, Remote: StateUnliked},
			mode:  ModeShadow,
			want:  ActionPushLike,
			write: false,
		},
		{
			name:  "changed differently",
			snap:  Snapshot{Provider: "spotify", ItemID: "3", Base: StateUnknown, Local: StateLiked, Remote: StateUnliked, LocalAt: asOf, RemoteAt: asOf.Add(time.Minute)},
			mode:  ModeLive,
			want:  ActionConflictNewest,
			write: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Reconcile(tc.snap, 0.9, tc.mode)
			if got.Action != tc.want {
				t.Fatalf("Action = %q, want %q", got.Action, tc.want)
			}
			if got.Write != tc.write {
				t.Fatalf("Write = %v, want %v", got.Write, tc.write)
			}
		})
	}
}

func TestBuildPlanAppliesSafetyRails(t *testing.T) {
	t.Parallel()

	_, err := BuildPlan([]Snapshot{{Provider: "spotify", ItemID: "1"}}, 0.8, ModeLive, 100, 20)
	if err == nil {
		t.Fatal("BuildPlan() error = nil, want mass-change guard failure")
	}
}

func TestBuildPlanWarningsAndDryRun(t *testing.T) {
	t.Parallel()

	plan, err := BuildPlan([]Snapshot{{Provider: "spotify", ItemID: "1", Base: StateUnliked, Local: StateLiked, Remote: StateUnliked}}, 0.95, ModeShadow, 1, 1)
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	if !plan.DryRun {
		t.Fatal("BuildPlan() dry run = false, want true")
	}
	if len(plan.Warnings) != 0 {
		t.Fatalf("BuildPlan() warnings = %d, want 0", len(plan.Warnings))
	}
	if len(plan.Items) != 1 || plan.Items[0].Write {
		t.Fatalf("BuildPlan() item write = %v, want false", plan.Items[0].Write)
	}
}

func TestGuardPartialFetch(t *testing.T) {
	t.Parallel()

	if err := GuardPartialFetch(100, 20, 0.7); err == nil {
		t.Fatal("GuardPartialFetch() error = nil, want failure")
	}
	if err := GuardPartialFetch(100, 80, 0.7); err != nil {
		t.Fatalf("GuardPartialFetch() error = %v, want nil", err)
	}
}

func TestTombstoneAndUndo(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	tombstone := Tombstone{Provider: "spotify", ExternalID: "abc"}.Renew(base, 365*24*time.Hour)
	if !tombstone.Active(base.Add(24 * time.Hour)) {
		t.Fatal("Tombstone should be active before expiry")
	}
	if tombstone.Active(base.Add(366 * 24 * time.Hour)) {
		t.Fatal("Tombstone should expire")
	}

	before, after, ok := WriteLog{Before: StateLiked, After: StateUnliked}.Undo()
	if !ok || before != StateUnliked || after != StateLiked {
		t.Fatalf("Undo() = %v %v %v, want unliked liked true", before, after, ok)
	}
}
