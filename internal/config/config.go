// Package config loads syncd configuration from environment variables and
// command-line flags, in that order of precedence (flags win).
package config

import (
	"flag"
	"fmt"
	"io"
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
	ReleasesURL       string
	Proto             int
	ShutdownWait      time.Duration
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
		ReleasesURL:       envOr(getenv, "SYNCD_RELEASES_URL", "https://github.com/Vic-41148/groovesync/releases"),
		Proto:             DefaultProto,
		ShutdownWait:      10 * time.Second,
		MinBuild:          map[string]int{},
		RecommendedBuild:  map[string]int{},
	}

	fs := flag.NewFlagSet("syncd", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&cfg.Addr, "addr", cfg.Addr, "listen address (loopback by default)")
	fs.StringVar(&cfg.DBPath, "db", cfg.DBPath, "path to the SQLite database file")
	fs.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "debug, info, warn or error")
	fs.StringVar(&cfg.NavidromeURL, "navidrome-url", cfg.NavidromeURL, "base URL of the Navidrome instance")
	fs.StringVar(&cfg.ReleasesURL, "releases-url", cfg.ReleasesURL, "where clients download builds from")
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

func envOr(getenv func(string) string, key, fallback string) string {
	if v := strings.TrimSpace(getenv(key)); v != "" {
		return v
	}
	return fallback
}
