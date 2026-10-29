package taste

import (
	"sort"
	"strings"
	"time"
)

type Source string

const (
	SourceUnknown      Source = ""
	SourceLocal        Source = "local"
	SourceMusicBrainz  Source = "musicbrainz"
	SourceListenBrainz Source = "listenbrainz"
	SourceNavidrome    Source = "navidrome"
	SourceYouTube      Source = "youtube"
	SourceSpotify      Source = "spotify"
)

type EventType string

const (
	EventPlay     EventType = "play"
	EventSkip     EventType = "skip"
	EventLike     EventType = "like"
	EventUnlike   EventType = "unlike"
	EventPlaylist EventType = "playlist_add"
	EventDownload EventType = "download"
	EventDelete   EventType = "delete"
)

type Event struct {
	TrackID       string
	Source        Source
	Type          EventType
	Value         float64
	PercentPlayed float64
	PositionMs    int64
	At            time.Time
}

type Config struct {
	LikeWeight        float64
	UnlikeWeight      float64
	CompletionWeight  float64
	SkipWeight        float64
	ReplayWeight      float64
	ReplayHalfLife    time.Duration
	PlaylistAddWeight float64
	DownloadWeight    float64
	DeleteWeight      float64
}

type Contribution struct {
	Name  string
	Value float64
}

type Result struct {
	Score         float64
	Contributions []Contribution
}

func DefaultConfig() Config {
	return Config{
		LikeWeight:        10,
		UnlikeWeight:      -16,
		CompletionWeight:  6,
		SkipWeight:        -5,
		ReplayWeight:      1.5,
		ReplayHalfLife:    14 * 24 * time.Hour,
		PlaylistAddWeight: 2,
		DownloadWeight:    3,
		DeleteWeight:      -4,
	}
}

func Score(events []Event, cfg Config) Result {
	return ScoreAt(events, cfg, time.Now())
}

func ScoreAt(events []Event, cfg Config, asOf time.Time) Result {
	var out Result
	registers := make(map[string]registerState)
	ordered := make([]Contribution, 0, len(events))
	for _, e := range events {
		if !AllowedSource(e.Source) {
			continue
		}
		switch e.Type {
		case EventLike:
			registers[e.TrackID] = registers[e.TrackID].write(e, Contribution{Name: contributionName(e.TrackID, "like"), Value: cfg.LikeWeight})
		case EventUnlike:
			registers[e.TrackID] = registers[e.TrackID].write(e, Contribution{Name: contributionName(e.TrackID, "unlike"), Value: cfg.UnlikeWeight})
		case EventPlay:
			addContribution(&out, &ordered, contributionName(e.TrackID, "play"), cfg.playContribution(e, asOf))
		case EventSkip:
			addContribution(&out, &ordered, contributionName(e.TrackID, "skip"), cfg.skipContribution(e))
		case EventPlaylist:
			addContribution(&out, &ordered, contributionName(e.TrackID, "playlist_add"), cfg.PlaylistAddWeight)
		case EventDownload:
			addContribution(&out, &ordered, contributionName(e.TrackID, "download"), cfg.DownloadWeight)
		case EventDelete:
			addContribution(&out, &ordered, contributionName(e.TrackID, "delete"), cfg.DeleteWeight)
		}
	}
	for _, reg := range registers {
		if reg.contribution.Name != "" {
			addContribution(&out, &ordered, reg.contribution.Name, reg.contribution.Value)
		}
	}
	out.Contributions = append(out.Contributions, ordered...)
	sort.SliceStable(out.Contributions, func(i, j int) bool {
		if out.Contributions[i].Value == out.Contributions[j].Value {
			return out.Contributions[i].Name < out.Contributions[j].Name
		}
		return out.Contributions[i].Value > out.Contributions[j].Value
	})
	return out
}

func AllowedSource(source Source) bool {
	switch source {
	case SourceUnknown, SourceLocal, SourceMusicBrainz, SourceListenBrainz, SourceNavidrome, SourceYouTube:
		return true
	default:
		return false
	}
}

func contributionName(trackID, name string) string {
	if trackID == "" {
		return name
	}
	return trackID + ": " + name
}

func addContribution(out *Result, ordered *[]Contribution, name string, value float64) {
	if value == 0 {
		return
	}
	out.Score += value
	*ordered = append(*ordered, Contribution{Name: name, Value: value})
}

func (c Config) playContribution(e Event, asOf time.Time) float64 {
	completion := e.PercentPlayed
	if completion < 0 {
		completion = 0
	}
	if completion > 1 {
		completion = 1
	}
	return completion*c.CompletionWeight + c.replayContribution(e.At, asOf)
}

func (c Config) skipContribution(e Event) float64 {
	position := float64(e.PositionMs) / 1000
	if position < 0 {
		position = 0
	}
	return c.SkipWeight - position/120
}

func (c Config) replayContribution(at, asOf time.Time) float64 {
	if c.ReplayWeight == 0 {
		return 0
	}
	if at.IsZero() || asOf.IsZero() || c.ReplayHalfLife <= 0 {
		return c.ReplayWeight
	}
	age := asOf.Sub(at)
	if age < 0 {
		age = 0
	}
	decay := 1.0
	if age > 0 {
		decay = 1.0 / (1.0 + age.Hours()/c.ReplayHalfLife.Hours())
	}
	return c.ReplayWeight * decay
}

type registerState struct {
	seen         bool
	stamp        time.Time
	seq          int
	contribution Contribution
}

func (r registerState) write(e Event, c Contribution) registerState {
	stamp := e.At
	if stamp.IsZero() {
		stamp = time.Unix(0, 0)
	}
	seq := r.seq + 1
	if !r.seen || stamp.After(r.stamp) || (stamp.Equal(r.stamp) && seq >= r.seq) {
		return registerState{seen: true, stamp: stamp, seq: seq, contribution: c}
	}
	return registerState{seen: true, stamp: r.stamp, seq: seq, contribution: r.contribution}
}

type Metadata struct {
	Title  string
	Artist string
	Album  string
	Tags   []string
}

type BacktestSample struct {
	Candidate MixCandidate
	Favorite  bool
}

func Backtest(samples []BacktestSample, seed int64) []MixCandidate {
	items := make([]MixCandidate, 0, len(samples))
	for _, sample := range samples {
		items = append(items, sample.Candidate)
	}
	return BacktestRank(items, seed)
}

func ColdStartScore(candidate, library Metadata) float64 {
	score := 0.0
	if equalFold(candidate.Title, library.Title) {
		score += 2
	}
	if equalFold(candidate.Artist, library.Artist) {
		score += 2
	}
	if equalFold(candidate.Album, library.Album) {
		score += 1
	}
	score += overlapScore(candidate.Tags, library.Tags)
	return score
}

func overlapScore(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	seen := make(map[string]struct{}, len(a))
	for _, v := range a {
		seen[strings.ToLower(strings.TrimSpace(v))] = struct{}{}
	}
	var score float64
	for _, v := range b {
		if _, ok := seen[strings.ToLower(strings.TrimSpace(v))]; ok {
			score += 0.5
		}
	}
	return score
}

func equalFold(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}
