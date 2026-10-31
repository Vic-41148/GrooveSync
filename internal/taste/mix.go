// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package taste

import (
	"math/rand"
	"sort"
	"time"
)

type MixCandidate struct {
	TrackID      string
	Title        string
	Score        float64
	LastPlayedAt time.Time
	Local        bool
	Liked        bool
	ServiceName  string
}

func ScoreWeightedShuffle(items []MixCandidate, seed int64) []MixCandidate {
	out := append([]MixCandidate(nil), items...)
	r := rand.New(rand.NewSource(seed))
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			if out[i].TrackID == out[j].TrackID {
				return r.Intn(2) == 0
			}
			return out[i].TrackID < out[j].TrackID
		}
		return out[i].Score > out[j].Score
	})
	return out
}

func Rediscover(items []MixCandidate, asOf time.Time, days int) []MixCandidate {
	cutoff := asOf.Add(-time.Duration(days) * 24 * time.Hour)
	return filterMix(items, func(item MixCandidate) bool {
		if item.Liked {
			return true
		}
		if item.LastPlayedAt.IsZero() {
			return true
		}
		return item.LastPlayedAt.Before(cutoff)
	})
}

func LocalRarities(items []MixCandidate) []MixCandidate {
	return filterMix(items, func(item MixCandidate) bool {
		return item.Local && item.Score > 0 && !item.Liked
	})
}

func MissingLocally(items []MixCandidate) []MixCandidate {
	return filterMix(items, func(item MixCandidate) bool {
		return item.Liked && !item.Local
	})
}

func BacktestRank(items []MixCandidate, seed int64) []MixCandidate {
	return ScoreWeightedShuffle(items, seed)
}

func filterMix(items []MixCandidate, keep func(MixCandidate) bool) []MixCandidate {
	out := make([]MixCandidate, 0, len(items))
	for _, item := range items {
		if keep(item) {
			out = append(out, item)
		}
	}
	return out
}
