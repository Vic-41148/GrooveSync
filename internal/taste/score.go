package taste

import "sort"

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
	Type          EventType
	Value         float64
	PercentPlayed float64
	PositionMs    int64
}

type Config struct {
	LikeWeight        float64
	UnlikeWeight      float64
	CompletionWeight  float64
	SkipWeight        float64
	ReplayWeight      float64
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
		PlaylistAddWeight: 2,
		DownloadWeight:    3,
		DeleteWeight:      -4,
	}
}

func Score(events []Event, cfg Config) Result {
	var out Result
	for _, e := range events {
		switch e.Type {
		case EventLike:
			add(&out, "like", cfg.LikeWeight)
		case EventUnlike:
			add(&out, "unlike", cfg.UnlikeWeight)
		case EventPlay:
			add(&out, "play", cfg.playContribution(e))
		case EventSkip:
			add(&out, "skip", cfg.skipContribution(e))
		case EventPlaylist:
			add(&out, "playlist_add", cfg.PlaylistAddWeight)
		case EventDownload:
			add(&out, "download", cfg.DownloadWeight)
		case EventDelete:
			add(&out, "delete", cfg.DeleteWeight)
		}
	}
	sort.SliceStable(out.Contributions, func(i, j int) bool {
		if out.Contributions[i].Value == out.Contributions[j].Value {
			return out.Contributions[i].Name < out.Contributions[j].Name
		}
		return out.Contributions[i].Value > out.Contributions[j].Value
	})
	return out
}

func add(out *Result, name string, value float64) {
	if value == 0 {
		return
	}
	out.Score += value
	out.Contributions = append(out.Contributions, Contribution{Name: name, Value: value})
}

func (c Config) playContribution(e Event) float64 {
	completion := e.PercentPlayed
	if completion < 0 {
		completion = 0
	}
	if completion > 1 {
		completion = 1
	}
	return completion*c.CompletionWeight + c.ReplayWeight
}

func (c Config) skipContribution(e Event) float64 {
	position := float64(e.PositionMs) / 1000
	if position < 0 {
		position = 0
	}
	return c.SkipWeight - position/120
}
