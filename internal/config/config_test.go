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
