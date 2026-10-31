// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

// Package identity matches local tracks against connector candidates.
package identity

import (
	"strings"
)

// Method names how a match was made.
type Method string

const (
	MethodISRC   Method = "isrc"
	MethodMBID   Method = "mbid"
	MethodAcoust Method = "acoustid"
	MethodFuzzy  Method = "fuzzy"
	MethodNone   Method = "none"
)

// Track is the metadata used for matching.
type Track struct {
	MBID       string
	ISRC       string
	Title      string
	Artist     string
	DurationMS int
}

// Match is the confidence and method for one comparison.
type Match struct {
	Confidence float64
	Method     Method
}

// NormalizeTitle lower-cases a title and removes a trailing remaster suffix.
// Live and acoustic markers are preserved because they identify another recording.
func NormalizeTitle(raw string) string {
	s := normalizeSpace(strings.ToLower(raw))
	for {
		trimmed := s
		if trimmed == "" {
			return ""
		}

		changed := false
		if strings.HasSuffix(trimmed, ")") || strings.HasSuffix(trimmed, "]") {
			if open := strings.LastIndexAny(trimmed, "(["); open >= 0 {
				inner := trimmed[open+1 : len(trimmed)-1]
				if strings.Contains(inner, "remaster") {
					s = strings.TrimSpace(trimmed[:open])
					changed = true
				}
			}
		}
		if !changed {
			if idx := strings.LastIndex(trimmed, " - "); idx >= 0 && strings.Contains(trimmed[idx+3:], "remaster") {
				s = strings.TrimSpace(trimmed[:idx])
				changed = true
			}
		}
		if !changed {
			for _, suffix := range []string{" remastered", " remaster"} {
				if strings.HasSuffix(trimmed, suffix) {
					s = strings.TrimSpace(strings.TrimSuffix(trimmed, suffix))
					changed = true
					break
				}
			}
		}
		if !changed {
			return trimmed
		}
	}
}

// NormalizeArtist lower-cases an artist name and collapses whitespace.
func NormalizeArtist(raw string) string {
	return normalizeSpace(strings.ToLower(raw))
}

// Score compares two tracks. Shared identifiers win, conflicting identifiers
// reject the pair, and metadata falls back to a duration-aware fuzzy score.
func Score(local, remote Track) Match {
	localISRC := normalizeID(local.ISRC)
	remoteISRC := normalizeID(remote.ISRC)
	if localISRC != "" && remoteISRC != "" {
		if localISRC != remoteISRC {
			return Match{Method: MethodNone}
		}
		return Match{Confidence: 1, Method: MethodISRC}
	}

	localMBID := normalizeID(local.MBID)
	remoteMBID := normalizeID(remote.MBID)
	if localMBID != "" && remoteMBID != "" {
		if localMBID != remoteMBID {
			return Match{Method: MethodNone}
		}
		return Match{Confidence: 1, Method: MethodMBID}
	}

	title := NormalizeTitle(local.Title)
	if title == "" || title != NormalizeTitle(remote.Title) {
		return Match{Method: MethodNone}
	}
	artistMatch := artistsMatch(NormalizeArtist(local.Artist), NormalizeArtist(remote.Artist))
	gap, haveDuration := durationGap(local.DurationMS, remote.DurationMS)

	switch {
	case artistMatch && haveDuration && gap <= 3000:
		return Match{Confidence: 0.95, Method: MethodFuzzy}
	case artistMatch && haveDuration && gap <= 10000:
		return Match{Confidence: 0.85, Method: MethodFuzzy}
	case artistMatch:
		return Match{Confidence: 0.7, Method: MethodFuzzy}
	case haveDuration && gap <= 3000:
		return Match{Confidence: 0.6, Method: MethodFuzzy}
	default:
		return Match{Confidence: 0.5, Method: MethodFuzzy}
	}
}

// Resolve picks the best candidate and reports whether manual review is needed.
func Resolve(local Track, candidates []Track, threshold float64) (int, Match, bool) {
	if len(candidates) == 0 {
		return -1, Match{Method: MethodNone}, true
	}
	bestIndex := 0
	best := Score(local, candidates[0])
	for i := 1; i < len(candidates); i++ {
		match := Score(local, candidates[i])
		if match.Confidence > best.Confidence {
			bestIndex = i
			best = match
		}
	}
	needsReview := best.Confidence <= 0 || best.Confidence < threshold
	return bestIndex, best, needsReview
}

func artistsMatch(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	return strings.Contains(a, b) || strings.Contains(b, a)
}

func durationGap(a, b int) (int, bool) {
	if a <= 0 || b <= 0 {
		return 0, false
	}
	gap := a - b
	if gap < 0 {
		return -gap, true
	}
	return gap, true
}

func normalizeID(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func normalizeSpace(raw string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
}
