// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

// Package simulation exercises event replication under message loss,
// duplication, reordering, partitions, clock jumps and restarts. It is a
// seeded model, not a network harness: the transport faults are applied to the
// delivery of records before the replicas merge.
package simulation

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/Vic-41148/groovesync/server/internal/taste"
)

const (
	replicaCount  = 4
	defaultSeeds  = 200
	simulationEnv = "SIMULATION_SEEDS"
)

var epoch = time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)

func seedCount() int {
	raw := os.Getenv(simulationEnv)
	if raw == "" {
		return defaultSeeds
	}
	count, err := strconv.Atoi(raw)
	if err != nil || count <= 0 {
		return defaultSeeds
	}
	return count
}

func TestClusterConverges(t *testing.T) {
	t.Parallel()

	for seed := int64(0); seed < int64(seedCount()); seed++ {
		if err := run(seed); err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
	}
}

func run(seed int64) error {
	rng := rand.New(rand.NewSource(seed))
	generated := generate(rng)
	logs, delivered := deliver(rng, generated)

	partition(rng, logs)
	if err := restart(rng, logs); err != nil {
		return err
	}

	// Anti-entropy: every replica merges every other replica until a full
	// round changes nothing.
	for round := 0; round <= replicaCount; round++ {
		before := snapshot(logs)
		for i := range logs {
			for j := range logs {
				if i == j {
					continue
				}
				logs[i] = taste.MergeLogs(logs[i], logs[j])
			}
		}
		if reflect.DeepEqual(before, snapshot(logs)) {
			break
		}
	}

	want := latest(delivered)
	for i, log := range logs {
		got := log.Entries()
		if len(got) != len(want) {
			return fmt.Errorf("replica %d has %d events, want %d", i, len(got), len(want))
		}
		seen := make(map[string]bool, len(got))
		for _, event := range got {
			if seen[event.ID] {
				return fmt.Errorf("replica %d kept duplicate %q", i, event.ID)
			}
			seen[event.ID] = true
			expected, ok := want[event.ID]
			if !ok {
				return fmt.Errorf("replica %d invented %q", i, event.ID)
			}
			if !reflect.DeepEqual(event, expected) {
				return fmt.Errorf("replica %d has %q as %v, want %v", i, event.ID, event.HLC, expected.HLC)
			}
		}
		if i > 0 {
			if first := logs[0].Entries(); !reflect.DeepEqual(got, first) {
				return fmt.Errorf("replica %d diverged from replica 0", i)
			}
		}
	}
	return nil
}

func generate(rng *rand.Rand) []taste.EventRecord {
	count := 20 + rng.Intn(40)
	events := make([]taste.EventRecord, 0, count)
	for i := 0; i < count; i++ {
		wall := epoch.Add(time.Duration(rng.Intn(60000)) * time.Millisecond)
		if rng.Intn(20) == 0 {
			// A device clock jump must not break convergence.
			wall = wall.Add(time.Duration(1+rng.Intn(6)) * time.Hour)
		}
		events = append(events, taste.EventRecord{
			ID:          fmt.Sprintf("e%d", i),
			DeviceID:    fmt.Sprintf("d%d", rng.Intn(replicaCount)),
			CanonicalID: fmt.Sprintf("t%d", rng.Intn(5)),
			Source:      taste.SourceLocal,
			Type:        taste.EventPlay,
			Value:       1,
			HLC: taste.HLC{
				Wall:    wall,
				Counter: uint32(rng.Intn(5)),
				Node:    fmt.Sprintf("n%d", rng.Intn(replicaCount)),
			},
		})
	}
	return events
}

// deliver applies drops, duplicates and reordering. It returns the logs and the
// union of records that reached at least one replica.
func deliver(rng *rand.Rand, events []taste.EventRecord) ([]taste.Log, []taste.EventRecord) {
	logs := make([]taste.Log, replicaCount)
	delivered := make([]taste.EventRecord, 0, len(events))
	for _, event := range events {
		reached := false
		for replica := 0; replica < replicaCount; replica++ {
			if rng.Intn(3) == 0 {
				continue
			}
			reached = true
			entries := append(logs[replica].Entries(), event)
			if rng.Intn(5) == 0 {
				entries = append(entries, event)
			}
			rng.Shuffle(len(entries), func(i, j int) { entries[i], entries[j] = entries[j], entries[i] })
			logs[replica] = taste.NewLog(entries...)
		}
		if reached {
			delivered = append(delivered, event)
		}
	}
	return logs, delivered
}

// partition splits the replicas and merges each side in isolation. The later
// anti-entropy rounds heal the split.
func partition(rng *rand.Rand, logs []taste.Log) {
	split := 1 + rng.Intn(replicaCount-1)
	for _, group := range [][]int{rangeInts(0, split), rangeInts(split, replicaCount)} {
		for i := 1; i < len(group); i++ {
			logs[group[0]] = taste.MergeLogs(logs[group[0]], logs[group[i]])
		}
	}
}

// restart serialises one replica's log and reloads it, as a process restart
// would.
func restart(rng *rand.Rand, logs []taste.Log) error {
	replica := rng.Intn(replicaCount)
	raw, err := json.Marshal(logs[replica].Entries())
	if err != nil {
		return fmt.Errorf("encode replica %d: %w", replica, err)
	}
	var entries []taste.EventRecord
	if err := json.Unmarshal(raw, &entries); err != nil {
		return fmt.Errorf("decode replica %d: %w", replica, err)
	}
	logs[replica] = taste.NewLog(entries...)
	return nil
}

func rangeInts(start, end int) []int {
	out := make([]int, 0, end-start)
	for i := start; i < end; i++ {
		out = append(out, i)
	}
	return out
}

func latest(events []taste.EventRecord) map[string]taste.EventRecord {
	out := make(map[string]taste.EventRecord, len(events))
	for _, event := range events {
		if prev, ok := out[event.ID]; !ok || event.HLC.After(prev.HLC) {
			out[event.ID] = event
		}
	}
	return out
}

func snapshot(logs []taste.Log) [][]taste.EventRecord {
	out := make([][]taste.EventRecord, len(logs))
	for i, log := range logs {
		out[i] = log.Entries()
	}
	return out
}
