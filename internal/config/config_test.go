package config

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadParsesBuildFloors(t *testing.T) {
	t.Parallel()

	cfg, err := Load(func(key string) string {
		switch key {
		case "SYNCD_MIN_BUILD_ANDROID":
			return "123"
		case "SYNCD_RECOMMENDED_BUILD_DESKTOP":
			return "456"
		default:
			return ""
		}
	}, nil, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got := cfg.MinBuild["android"]; got != 123 {
		t.Fatalf("MinBuild[android] = %d, want 123", got)
	}
	if got := cfg.RecommendedBuild["desktop"]; got != 456 {
		t.Fatalf("RecommendedBuild[desktop] = %d, want 456", got)
	}
}

func TestLoadRejectsNegativeBuildFloor(t *testing.T) {
	t.Parallel()

	_, err := Load(func(key string) string {
		if key == "SYNCD_MIN_BUILD_ANDROID" {
			return "-1"
		}
		return ""
	}, nil, &bytes.Buffer{})
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
}

func TestLoadParsesNavidromeCredentials(t *testing.T) {
	t.Parallel()

	cfg, err := Load(func(key string) string {
		switch key {
		case "SYNCD_NAVIDROME_URL":
			return "http://navidrome.lan:4533"
		case "SYNCD_NAVIDROME_USER":
			return "vic"
		case "SYNCD_NAVIDROME_PASSWORD":
			return "secret"
		default:
			return ""
		}
	}, nil, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.NavidromeURL != "http://navidrome.lan:4533" || cfg.NavidromeUser != "vic" || cfg.NavidromePassword != "secret" {
		t.Fatalf("navidrome config = %q %q %q, want the environment values", cfg.NavidromeURL, cfg.NavidromeUser, cfg.NavidromePassword)
	}
}

func TestLoadParsesLibrarySyncInterval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    time.Duration
		wantErr bool
	}{
		{name: "disabled by default", want: 0},
		{name: "hours", raw: "6h", want: 6 * time.Hour},
		{name: "minutes", raw: "15m", want: 15 * time.Minute},
		{name: "negative", raw: "-1h", wantErr: true},
		{name: "garbage", raw: "often", wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := Load(func(key string) string {
				if key == "SYNCD_LIBRARY_SYNC_INTERVAL" {
					return tc.raw
				}
				return ""
			}, nil, &bytes.Buffer{})
			if tc.wantErr {
				if err == nil {
					t.Fatal("Load() error = nil, want failure")
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.LibrarySyncInterval != tc.want {
				t.Fatalf("LibrarySyncInterval = %v, want %v", cfg.LibrarySyncInterval, tc.want)
			}
		})
	}
}

func TestLoadParsesEnrichSettings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		interval  string
		limit     string
		want      time.Duration
		wantLimit int
		wantErr   bool
	}{
		{name: "disabled by default", wantLimit: 25},
		{name: "interval and limit", interval: "12h", limit: "50", want: 12 * time.Hour, wantLimit: 50},
		{name: "negative interval", interval: "-1m", wantLimit: 25, wantErr: true},
		{name: "garbage limit", limit: "many", wantLimit: 25, wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := Load(func(key string) string {
				switch key {
				case "SYNCD_ENRICH_INTERVAL":
					return tc.interval
				case "SYNCD_ENRICH_LIMIT":
					return tc.limit
				default:
					return ""
				}
			}, nil, &bytes.Buffer{})
			if tc.wantErr {
				if err == nil {
					t.Fatal("Load() error = nil, want failure")
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.EnrichInterval != tc.want || cfg.EnrichLimit != tc.wantLimit {
				t.Fatalf("enrich settings = %v %d, want %v %d", cfg.EnrichInterval, cfg.EnrichLimit, tc.want, tc.wantLimit)
			}
		})
	}
}

func TestLoadParsesMusicBrainzSettings(t *testing.T) {
	t.Parallel()

	cfg, err := Load(func(key string) string {
		switch key {
		case "SYNCD_MUSICBRAINZ_URL":
			return "http://musicbrainz.internal/ws/2"
		case "SYNCD_MUSICBRAINZ_AGENT":
			return "GrooveSync/test"
		default:
			return ""
		}
	}, nil, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.MusicBrainzURL != "http://musicbrainz.internal/ws/2" || cfg.MusicBrainzAgent != "GrooveSync/test" {
		t.Fatalf("musicbrainz config = %q %q, want the environment values", cfg.MusicBrainzURL, cfg.MusicBrainzAgent)
	}
}

func TestLoadMusicBrainzDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := Load(func(string) string { return "" }, nil, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.MusicBrainzURL != DefaultMusicBrainzURL || cfg.MusicBrainzAgent != DefaultMusicBrainzAgent {
		t.Fatalf("musicbrainz defaults = %q %q, want the package defaults", cfg.MusicBrainzURL, cfg.MusicBrainzAgent)
	}
}

func TestLoadParsesYouTubeSettings(t *testing.T) {
	t.Parallel()

	cfg, err := Load(func(key string) string {
		if key == "SYNCD_YTMUSIC_URL" {
			return "http://127.0.0.1:8099"
		}
		return ""
	}, nil, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.YTMusicURL != "http://127.0.0.1:8099" {
		t.Fatalf("ytmusic url = %q, want the environment value", cfg.YTMusicURL)
	}
}

func TestLoadParsesListenBrainzSettings(t *testing.T) {
	t.Parallel()

	cfg, err := Load(func(key string) string {
		switch key {
		case "SYNCD_LISTENBRAINZ_URL":
			return "http://listenbrainz.internal"
		case "SYNCD_LISTENBRAINZ_TOKEN":
			return "secret"
		default:
			return ""
		}
	}, nil, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ListenBrainzURL != "http://listenbrainz.internal" {
		t.Fatalf("listenbrainz url = %q, want the environment value", cfg.ListenBrainzURL)
	}
	if cfg.ListenBrainzToken != "secret" {
		t.Fatalf("listenbrainz token = %q, want the environment value", cfg.ListenBrainzToken)
	}

	cfg, err = Load(func(string) string { return "" }, nil, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Load() defaults error = %v", err)
	}
	if cfg.ListenBrainzURL != DefaultListenBrainzURL {
		t.Fatalf("listenbrainz default = %q, want %q", cfg.ListenBrainzURL, DefaultListenBrainzURL)
	}
}

func TestLoadParsesAcoustIDSettings(t *testing.T) {
	t.Parallel()

	cfg, err := Load(func(key string) string {
		switch key {
		case "SYNCD_ACOUSTID_URL":
			return "http://acoustid.internal/v2/lookup"
		case "SYNCD_ACOUSTID_KEY":
			return "app-key"
		case "SYNCD_FPCALC":
			return "/usr/local/bin/fpcalc"
		default:
			return ""
		}
	}, nil, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AcoustIDURL != "http://acoustid.internal/v2/lookup" || cfg.AcoustIDKey != "app-key" {
		t.Fatalf("acoustid config = %q %q, want the environment values", cfg.AcoustIDURL, cfg.AcoustIDKey)
	}
	if cfg.FPCalcPath != "/usr/local/bin/fpcalc" {
		t.Fatalf("fpcalc path = %q, want the environment value", cfg.FPCalcPath)
	}

	cfg, err = Load(func(string) string { return "" }, nil, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Load() defaults error = %v", err)
	}
	if cfg.AcoustIDURL != DefaultAcoustIDURL || cfg.FPCalcPath != DefaultFPCalcPath || cfg.AcoustIDKey != "" {
		t.Fatalf("acoustid defaults = %q %q %q, want the package defaults", cfg.AcoustIDURL, cfg.FPCalcPath, cfg.AcoustIDKey)
	}
}

func TestLoadParsesUploadDir(t *testing.T) {
	t.Parallel()

	cfg, err := Load(func(key string) string {
		if key == "SYNCD_MUSIC_ROOT" {
			return "/srv/music"
		}
		return ""
	}, nil, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.UploadDir != filepath.Join("/srv/music", "Incoming") {
		t.Fatalf("UploadDir = %q, want the music root's Incoming folder", cfg.UploadDir)
	}

	cfg, err = Load(func(key string) string {
		switch key {
		case "SYNCD_MUSIC_ROOT":
			return "/srv/music"
		case "SYNCD_UPLOAD_DIR":
			return "/var/lib/syncd/incoming"
		}
		return ""
	}, nil, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Load() explicit error = %v", err)
	}
	if cfg.UploadDir != "/var/lib/syncd/incoming" {
		t.Fatalf("UploadDir = %q, want the environment override", cfg.UploadDir)
	}

	cfg, err = Load(func(string) string { return "" }, nil, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Load() defaults error = %v", err)
	}
	if cfg.UploadDir != "" {
		t.Fatalf("UploadDir = %q, want uploads disabled by default", cfg.UploadDir)
	}
}

func TestLoadParsesIgnoreGlobs(t *testing.T) {
	t.Parallel()

	cfg, err := Load(func(key string) string {
		if key == "SYNCD_IGNORE_GLOBS" {
			return " *.bak, .DS_Store ,,"
		}
		return ""
	}, nil, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.IgnoreGlobs) != 2 || cfg.IgnoreGlobs[0] != "*.bak" || cfg.IgnoreGlobs[1] != ".DS_Store" {
		t.Fatalf("IgnoreGlobs = %#v, want two trimmed patterns", cfg.IgnoreGlobs)
	}

	cfg, err = Load(func(string) string { return "" }, nil, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Load() defaults error = %v", err)
	}
	if len(cfg.IgnoreGlobs) != 0 {
		t.Fatalf("IgnoreGlobs = %#v, want none by default", cfg.IgnoreGlobs)
	}
}

func TestLoadParsesHashSettings(t *testing.T) {
	t.Parallel()

	cfg, err := Load(func(key string) string {
		switch key {
		case "SYNCD_MUSIC_ROOT":
			return "/srv/music"
		case "SYNCD_HASH_INTERVAL":
			return "15m"
		case "SYNCD_HASH_LIMIT":
			return "40"
		default:
			return ""
		}
	}, nil, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.MusicRoot != "/srv/music" || cfg.HashInterval != 15*time.Minute || cfg.HashLimit != 40 {
		t.Fatalf("hash config = %q %v %d, want the environment values", cfg.MusicRoot, cfg.HashInterval, cfg.HashLimit)
	}

	cfg, err = Load(func(string) string { return "" }, nil, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Load() defaults error = %v", err)
	}
	if cfg.MusicRoot != "" || cfg.HashInterval != 0 || cfg.HashLimit != 25 {
		t.Fatalf("hash defaults = %q %v %d, want no root, no interval and 25 files", cfg.MusicRoot, cfg.HashInterval, cfg.HashLimit)
	}
}

func TestLoadRejectsUnknownLogLevel(t *testing.T) {
	t.Parallel()

	_, err := Load(func(key string) string {
		if key == "SYNCD_LOG_LEVEL" {
			return "loud"
		}
		return ""
	}, nil, &bytes.Buffer{})
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
}
