package taste

import "testing"

func TestScore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		events []Event
		want   float64
	}{
		{
			name:   "like and complete play",
			events: []Event{{Type: EventLike}, {Type: EventPlay, PercentPlayed: 1}},
			want:   17.5,
		},
		{
			name:   "skip and unlike",
			events: []Event{{Type: EventSkip, PositionMs: 45000}, {Type: EventUnlike}},
			want:   -21.375,
		},
		{
			name:   "playlist and download",
			events: []Event{{Type: EventPlaylist}, {Type: EventDownload}},
			want:   5,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Score(tc.events, DefaultConfig())
			if got.Score != tc.want {
				t.Fatalf("Score() = %v, want %v", got.Score, tc.want)
			}
		})
	}
}

func TestScoreKeepsContributionsSorted(t *testing.T) {
	t.Parallel()

	got := Score([]Event{{Type: EventUnlike}, {Type: EventLike}, {Type: EventPlay, PercentPlayed: 0.5}}, DefaultConfig())
	if len(got.Contributions) == 0 {
		t.Fatal("Score() contributions are empty")
	}
	if got.Contributions[0].Name != "like" {
		t.Fatalf("first contribution = %q, want like", got.Contributions[0].Name)
	}
}
