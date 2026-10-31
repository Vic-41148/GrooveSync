package config

import (
	"bytes"
	"testing"
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
