// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package taste

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

type HLC struct {
	Wall    time.Time `json:"wall"`
	Counter uint32    `json:"counter"`
	Node    string    `json:"node"`
}

// Format renders the clock for storage. The node keeps any slashes it holds.
func (h HLC) Format() string {
	return fmt.Sprintf("%s/%d/%s", h.Wall.UTC().Format(time.RFC3339Nano), h.Counter, h.Node)
}

// ParseHLC reads a clock written by Format.
func ParseHLC(text string) (HLC, error) {
	parts := strings.SplitN(text, "/", 3)
	if len(parts) != 3 {
		return HLC{}, fmt.Errorf("taste: malformed clock %q", text)
	}
	wall, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return HLC{}, fmt.Errorf("taste: parse clock wall: %w", err)
	}
	counter, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return HLC{}, fmt.Errorf("taste: parse clock counter: %w", err)
	}
	if parts[2] == "" {
		return HLC{}, fmt.Errorf("taste: clock node is required")
	}
	return HLC{Wall: wall.UTC(), Counter: uint32(counter), Node: parts[2]}, nil
}

func NewHLC(node string, now time.Time) HLC {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return HLC{Wall: now.UTC(), Node: node}
}

func (h HLC) After(other HLC) bool {
	if h.Wall.After(other.Wall) {
		return true
	}
	if h.Wall.Before(other.Wall) {
		return false
	}
	if h.Counter != other.Counter {
		return h.Counter > other.Counter
	}
	return h.Node > other.Node
}

func (h HLC) Merge(remote HLC, now time.Time) HLC {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	if now.Before(h.Wall) {
		now = h.Wall
	}
	if now.Before(remote.Wall) {
		now = remote.Wall
	}
	if h.Wall.Equal(remote.Wall) {
		return HLC{Wall: now, Counter: max32(h.Counter, remote.Counter) + 1, Node: h.Node}
	}
	if h.Wall.After(remote.Wall) {
		return HLC{Wall: now, Counter: h.Counter + 1, Node: h.Node}
	}
	return HLC{Wall: now, Counter: remote.Counter + 1, Node: h.Node}
}

type EventRecord struct {
	TrackID       string    `json:"track_id,omitempty"`
	Source        Source    `json:"source"`
	ID            string    `json:"id"`
	DeviceID      string    `json:"device_id"`
	CanonicalID   string    `json:"canonical_id"`
	Type          EventType `json:"type"`
	Value         float64   `json:"value"`
	PercentPlayed float64   `json:"percent_played,omitempty"`
	PositionMs    int64     `json:"position_ms,omitempty"`
	HLC           HLC       `json:"hlc"`
}

func (e EventRecord) Validate() error {
	if !AllowedSource(e.Source) {
		return fmt.Errorf("eventlog: source %q is not allowed", e.Source)
	}
	if e.ID == "" {
		return fmt.Errorf("eventlog: id is required")
	}
	if e.DeviceID == "" {
		return fmt.Errorf("eventlog: device id is required")
	}
	if e.CanonicalID == "" {
		return fmt.Errorf("eventlog: canonical id is required")
	}
	if e.Type == "" {
		return fmt.Errorf("eventlog: type is required")
	}
	return nil
}

type Log struct {
	entries []EventRecord
}

func NewLog(entries ...EventRecord) Log {
	log := Log{entries: append([]EventRecord(nil), entries...)}
	log.normalize()
	return log
}

func (l Log) Entries() []EventRecord {
	out := append([]EventRecord(nil), l.entries...)
	return out
}

func (l Log) Score(cfg Config, asOf time.Time) Result {
	events := make([]Event, 0, len(l.entries))
	for _, entry := range l.entries {
		events = append(events, Event{
			TrackID:       entry.TrackID,
			Source:        entry.Source,
			Type:          entry.Type,
			Value:         entry.Value,
			PercentPlayed: entry.PercentPlayed,
			PositionMs:    entry.PositionMs,
			At:            entry.HLC.Wall,
		})
	}
	return ScoreAt(events, cfg, asOf)
}

// TrackScore is one track's affinity together with the contributions behind it.
type TrackScore struct {
	CanonicalID   string         `json:"canonical_id"`
	Score         float64        `json:"score"`
	Contributions []Contribution `json:"contributions"`
}

// ScoreTracks scores each track on its own events, highest first. Events are
// keyed by canonical id because the client track id never leaves the device.
func (l Log) ScoreTracks(cfg Config, asOf time.Time) []TrackScore {
	ordered := make([]string, 0, len(l.entries))
	grouped := make(map[string][]Event, len(l.entries))
	for _, entry := range l.entries {
		if _, ok := grouped[entry.CanonicalID]; !ok {
			ordered = append(ordered, entry.CanonicalID)
		}
		grouped[entry.CanonicalID] = append(grouped[entry.CanonicalID], Event{
			TrackID:       entry.CanonicalID,
			Source:        entry.Source,
			Type:          entry.Type,
			Value:         entry.Value,
			PercentPlayed: entry.PercentPlayed,
			PositionMs:    entry.PositionMs,
			At:            entry.HLC.Wall,
		})
	}
	out := make([]TrackScore, 0, len(ordered))
	for _, canonicalID := range ordered {
		result := ScoreAt(grouped[canonicalID], cfg, asOf)
		out = append(out, TrackScore{CanonicalID: canonicalID, Score: result.Score, Contributions: result.Contributions})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].CanonicalID < out[j].CanonicalID
		}
		return out[i].Score > out[j].Score
	})
	return out
}

func MergeLogs(a, b Log) Log {
	merged := make([]EventRecord, 0, len(a.entries)+len(b.entries))
	seen := make(map[string]EventRecord, len(a.entries)+len(b.entries))
	for _, entry := range append(a.entries, b.entries...) {
		if prev, ok := seen[entry.ID]; ok {
			if entry.HLC.After(prev.HLC) {
				seen[entry.ID] = entry
			}
			continue
		}
		seen[entry.ID] = entry
	}
	for _, entry := range seen {
		merged = append(merged, entry)
	}
	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].HLC.Wall.Equal(merged[j].HLC.Wall) {
			if merged[i].HLC.Counter == merged[j].HLC.Counter {
				return merged[i].ID < merged[j].ID
			}
			return merged[i].HLC.Counter < merged[j].HLC.Counter
		}
		return merged[i].HLC.Wall.Before(merged[j].HLC.Wall)
	})
	return NewLog(merged...)
}

func (l *Log) normalize() {
	filtered := l.entries[:0]
	if filtered == nil {
		filtered = make([]EventRecord, 0, len(l.entries))
	}
	for _, entry := range l.entries {
		if err := entry.Validate(); err != nil {
			continue
		}
		filtered = append(filtered, entry)
	}
	l.entries = filtered
	sort.SliceStable(l.entries, func(i, j int) bool {
		if l.entries[i].HLC.Wall.Equal(l.entries[j].HLC.Wall) {
			if l.entries[i].HLC.Counter == l.entries[j].HLC.Counter {
				return l.entries[i].ID < l.entries[j].ID
			}
			return l.entries[i].HLC.Counter < l.entries[j].HLC.Counter
		}
		return l.entries[i].HLC.Wall.Before(l.entries[j].HLC.Wall)
	})
}

func max32(a, b uint32) uint32 {
	if a > b {
		return a
	}
	return b
}
