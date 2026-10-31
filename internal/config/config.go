// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

// Package config loads syncd configuration from environment variables and
// command-line flags, in that order of precedence (flags win).
package config

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Defaults are deliberately loopback-only: the server is expected to sit behind
// a private network, not on a public interface (handbook NET-1).
const (
	DefaultAddr     = "127.0.0.1:8080"
	DefaultDBPath   = "syncd.db"
	DefaultLogLevel = "info"
	DefaultProto    = 1
	// MusicBrainz asks for a meaningful User-Agent and about one request per
	// second. The agent links back to the project so they can reach us.
	DefaultMusicBrainzURL   = "https://musicbrainz.org/ws/2"
	DefaultMusicBrainzAgent = "GrooveSync/1.0 (https://github.com/Vic-41148/groovesync)"
	DefaultListenBrainzURL  = "https://api.listenbrainz.org"
	DefaultAcoustIDURL      = "https://api.acoustid.org/v2/lookup"
	DefaultFPCalcPath       = "fpcalc"
)

// Config is the single resolved configuration for a syncd process.
type Config struct {
	Addr         string
	DBPath       string
	LogLevel     string
	NavidromeURL string
	// Credentials come from the environment only, so a password never appears
	// in the process list.
	NavidromeUser     string
	NavidromePassword string
	MusicBrainzURL    string
	MusicBrainzAgent  string
	ListenBrainzURL   string
	// ListenBrainzToken comes from the environment only.
	ListenBrainzToken string
	AcoustIDURL       string
	// AcoustIDKey is an application key from the environment only. Empty
	// disables fingerprint lookup.
	AcoustIDKey string
	FPCalcPath  string
	YTMusicURL  string
	ReleasesURL string
	// LibrarySyncInterval is zero when the mirror only runs on request.
	LibrarySyncInterval time.Duration
	// MusicRoot resolves relative paths reported by Navidrome.
	MusicRoot string
	// UploadDir receives files contributed by devices. Empty disables uploads.
	UploadDir string
	// IgnoreGlobs adds library paths the mirror must never index.
	IgnoreGlobs []string
	// HashInterval is zero when content hashing only runs on request.
	HashInterval time.Duration
	HashLimit    int
	// EnrichInterval is zero when MusicBrainz backfill only runs on request.
	EnrichInterval time.Duration
	EnrichLimit    int
	Proto          int
	ShutdownWait   time.Duration
	// Build floors per platform, 0 meaning "any build". Kept in config so a
	// release can raise a floor without a rebuild.
	MinBuild         map[string]int
	RecommendedBuild map[string]int
}

// Load resolves configuration. It never reads the real environment directly,
// so tests can pass a fake getenv.
func Load(getenv func(string) string, args []string, stderr io.Writer) (Config, error) {
	cfg := Config{
		Addr:              envOr(getenv, "SYNCD_ADDR", DefaultAddr),
		DBPath:            envOr(getenv, "SYNCD_DB", DefaultDBPath),
		LogLevel:          envOr(getenv, "SYNCD_LOG_LEVEL", DefaultLogLevel),
		NavidromeURL:      envOr(getenv, "SYNCD_NAVIDROME_URL", ""),
		NavidromeUser:     envOr(getenv, "SYNCD_NAVIDROME_USER", ""),
		NavidromePassword: envOr(getenv, "SYNCD_NAVIDROME_PASSWORD", ""),
		MusicBrainzURL:    envOr(getenv, "SYNCD_MUSICBRAINZ_URL", DefaultMusicBrainzURL),
		MusicBrainzAgent:  envOr(getenv, "SYNCD_MUSICBRAINZ_AGENT", DefaultMusicBrainzAgent),
		ListenBrainzURL:   envOr(getenv, "SYNCD_LISTENBRAINZ_URL", DefaultListenBrainzURL),
		ListenBrainzToken: envOr(getenv, "SYNCD_LISTENBRAINZ_TOKEN", ""),
		AcoustIDURL:       envOr(getenv, "SYNCD_ACOUSTID_URL", DefaultAcoustIDURL),
		AcoustIDKey:       envOr(getenv, "SYNCD_ACOUSTID_KEY", ""),
		FPCalcPath:        envOr(getenv, "SYNCD_FPCALC", DefaultFPCalcPath),
		MusicRoot:         envOr(getenv, "SYNCD_MUSIC_ROOT", ""),
		UploadDir:         envOr(getenv, "SYNCD_UPLOAD_DIR", ""),
		YTMusicURL:        envOr(getenv, "SYNCD_YTMUSIC_URL", ""),
		ReleasesURL:       envOr(getenv, "SYNCD_RELEASES_URL", "https://github.com/Vic-41148/groovesync/releases"),
		Proto:             DefaultProto,
		ShutdownWait:      10 * time.Second,
		MinBuild:          map[string]int{},
		RecommendedBuild:  map[string]int{},
	}

	interval, err := envDuration(getenv, "SYNCD_LIBRARY_SYNC_INTERVAL")
	if err != nil {
		return Config{}, err
	}
	cfg.LibrarySyncInterval = interval

	enrichInterval, err := envDuration(getenv, "SYNCD_ENRICH_INTERVAL")
	if err != nil {
		return Config{}, err
	}
	cfg.EnrichInterval = enrichInterval
	enrichLimit, err := envInt(getenv, "SYNCD_ENRICH_LIMIT")
	if err != nil {
		return Config{}, err
	}
	if enrichLimit == 0 {
		enrichLimit = 25
	}
	cfg.EnrichLimit = enrichLimit

	hashInterval, err := envDuration(getenv, "SYNCD_HASH_INTERVAL")
	if err != nil {
		return Config{}, err
	}
	cfg.HashInterval = hashInterval
	hashLimit, err := envInt(getenv, "SYNCD_HASH_LIMIT")
	if err != nil {
		return Config{}, err
	}
	if hashLimit == 0 {
		hashLimit = 25
	}
	cfg.HashLimit = hashLimit
	if cfg.UploadDir == "" && cfg.MusicRoot != "" {
		cfg.UploadDir = filepath.Join(cfg.MusicRoot, "Incoming")
	}

	if raw := strings.TrimSpace(getenv("SYNCD_IGNORE_GLOBS")); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			if pattern := strings.TrimSpace(part); pattern != "" {
				cfg.IgnoreGlobs = append(cfg.IgnoreGlobs, pattern)
			}
		}
	}

	fs := flag.NewFlagSet("syncd", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&cfg.Addr, "addr", cfg.Addr, "listen address (loopback by default)")
	fs.StringVar(&cfg.DBPath, "db", cfg.DBPath, "path to the SQLite database file")
	fs.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "debug, info, warn or error")
	fs.StringVar(&cfg.NavidromeURL, "navidrome-url", cfg.NavidromeURL, "base URL of the Navidrome instance")
	fs.StringVar(&cfg.MusicBrainzURL, "musicbrainz-url", cfg.MusicBrainzURL, "base URL of the MusicBrainz web service")
	fs.StringVar(&cfg.ListenBrainzURL, "listenbrainz-url", cfg.ListenBrainzURL, "base URL of the ListenBrainz API")
	fs.StringVar(&cfg.AcoustIDURL, "acoustid-url", cfg.AcoustIDURL, "base URL of the AcoustID lookup endpoint")
	fs.StringVar(&cfg.FPCalcPath, "fpcalc", cfg.FPCalcPath, "path to the Chromaprint fpcalc executable")
	fs.StringVar(&cfg.YTMusicURL, "ytmusic-url", cfg.YTMusicURL, "base URL of the YouTube Music sidecar")
	fs.StringVar(&cfg.ReleasesURL, "releases-url", cfg.ReleasesURL, "where clients download builds from")
	fs.DurationVar(&cfg.LibrarySyncInterval, "library-sync-interval", cfg.LibrarySyncInterval, "how often to refresh the library mirror, zero to disable")
	fs.DurationVar(&cfg.EnrichInterval, "enrich-interval", cfg.EnrichInterval, "how often to backfill metadata, zero to disable")
	fs.IntVar(&cfg.EnrichLimit, "enrich-limit", cfg.EnrichLimit, "tracks to backfill per pass")
	fs.StringVar(&cfg.MusicRoot, "music-root", cfg.MusicRoot, "root folder that resolves relative library paths")
	fs.StringVar(&cfg.UploadDir, "upload-dir", cfg.UploadDir, "folder that receives uploaded files, empty to disable")
	fs.DurationVar(&cfg.HashInterval, "hash-interval", cfg.HashInterval, "how often to hash library files, zero to disable")
	fs.IntVar(&cfg.HashLimit, "hash-limit", cfg.HashLimit, "files to hash per pass")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	cfg.LogLevel = strings.ToLower(strings.TrimSpace(cfg.LogLevel))
	switch cfg.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return Config{}, fmt.Errorf("config: unknown log level %q", cfg.LogLevel)
	}
	if cfg.Addr == "" {
		return Config{}, fmt.Errorf("config: addr must not be empty")
	}
	for _, platform := range []string{"android", "desktop"} {
		suffix := strings.ToUpper(platform)
		minBuild, err := envInt(getenv, "SYNCD_MIN_BUILD_"+suffix)
		if err != nil {
			return Config{}, err
		}
		recommended, err := envInt(getenv, "SYNCD_RECOMMENDED_BUILD_"+suffix)
		if err != nil {
			return Config{}, err
		}
		cfg.MinBuild[platform] = minBuild
		cfg.RecommendedBuild[platform] = recommended
	}
	return cfg, nil
}

func envInt(getenv func(string) string, key string) (int, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("config: %s: %w", key, err)
	}
	if n < 0 {
		return 0, fmt.Errorf("config: %s must not be negative", key)
	}
	return n, nil
}

func envDuration(getenv func(string) string, key string) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("config: %s: %w", key, err)
	}
	if d < 0 {
		return 0, fmt.Errorf("config: %s must not be negative", key)
	}
	return d, nil
}

func envOr(getenv func(string) string, key, fallback string) string {
	if v := strings.TrimSpace(getenv(key)); v != "" {
		return v
	}
	return fallback
}
