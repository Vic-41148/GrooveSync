// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Vic-41148/groovesync/server/internal/acquisition"
	"github.com/Vic-41148/groovesync/server/internal/api"
	"github.com/Vic-41148/groovesync/server/internal/config"
	"github.com/Vic-41148/groovesync/server/internal/connectors"
	"github.com/Vic-41148/groovesync/server/internal/devices"
	"github.com/Vic-41148/groovesync/server/internal/library"
	"github.com/Vic-41148/groovesync/server/internal/listenbrainz"
	"github.com/Vic-41148/groovesync/server/internal/logging"
	"github.com/Vic-41148/groovesync/server/internal/metadata"
	"github.com/Vic-41148/groovesync/server/internal/navidrome"
	"github.com/Vic-41148/groovesync/server/internal/store"
	"github.com/Vic-41148/groovesync/server/internal/taste"
	"github.com/Vic-41148/groovesync/server/internal/transfer"
	"github.com/Vic-41148/groovesync/server/internal/writeback"
	"github.com/Vic-41148/groovesync/server/internal/ytmusic"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Getenv, os.Stderr); err != nil {
		log.Fatal(err)
	}
}

func run(parent context.Context, args []string, getenv func(string) string, stderr io.Writer) error {
	cfg, err := config.Load(getenv, args, stderr)
	if err != nil {
		return err
	}

	adminToken := strings.TrimSpace(getenv("SYNCD_ADMIN_TOKEN"))
	if err := config.CheckExposure(cfg.Addr, adminToken); err != nil {
		return err
	}

	logger := logging.New(cfg.LogLevel)
	db, err := store.Open(parent, cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()

	connectorRepo := connectors.NewRepo(db)

	// Optional: connector token encryption key from environment. Expect a hex
	// encoded AES key (16/24/32 bytes). If provided, validate and set it on
	// the repository so tokens are encrypted at rest.
	if raw := strings.TrimSpace(getenv("SYNCD_CONNECTOR_KEY_HEX")); raw != "" {
		key, err := hex.DecodeString(raw)
		if err != nil {
			return fmt.Errorf("syncd: invalid SYNCD_CONNECTOR_KEY_HEX: %w", err)
		}
		if len(key) != 16 && len(key) != 24 && len(key) != 32 {
			return fmt.Errorf("syncd: SYNCD_CONNECTOR_KEY_HEX must decode to 16, 24 or 32 bytes, got %d", len(key))
		}
		connectorRepo.SetKey(key)
	}
	deviceRepo := devices.Repo{DB: db}
	eventStore := taste.NewStore(db)
	acquisitionRepo := acquisition.Repo{DB: db}
	libraryRepo := library.Repo{DB: db}
	queueRepo := writeback.NewRepo(db)
	processor := writeback.Processor{
		Store:     queueRepo,
		Now:       time.Now,
		BaseDelay: 5 * time.Second,
		MaxDelay:  5 * time.Minute,
		Limiter:   writeback.RateLimiter{MinInterval: 5 * time.Second},
		Executors: map[string]writeback.Executor{
			connectors.ProviderSpotify: connectors.SpotifyRemoteWriteExecutor(
				connectorRepo,
				connectors.SpotifyClient{},
				connectors.SpotifyRepoTokenSource{Repo: connectorRepo},
			),
			connectors.ProviderYouTube:      connectors.YouTubeWriteExecutor(db),
			connectors.ProviderListenBrainz: connectors.ListenBrainzWriteExecutor(db),
		},
	}

	features := []string{"meta", "review_queue", "connectors", "spotify_library", "spotify_import", "plan", "write_log", "devices", "events", "score", "mix", "library", "acquisition", "settings"}
	var libraryMirror api.LibraryMirror
	var navidromeHealth api.HealthChecker
	if cfg.NavidromeURL != "" {
		if cfg.NavidromeUser == "" || cfg.NavidromePassword == "" {
			return errors.New("syncd: a Navidrome URL needs SYNCD_NAVIDROME_USER and SYNCD_NAVIDROME_PASSWORD")
		}
		client, err := navidrome.NewClient(cfg.NavidromeURL, cfg.NavidromeUser, cfg.NavidromePassword)
		if err != nil {
			return err
		}
		libraryMirror = navidrome.Mirror{DB: db, Client: client, Ignore: cfg.IgnoreGlobs}
		navidromeHealth = client
		features = append(features, "library_sync")
	}

	var libraryHashes api.LibraryHasher
	if cfg.MusicRoot != "" || cfg.HashInterval > 0 {
		libraryHashes = library.HashBackfill{DB: db, Root: cfg.MusicRoot}
		features = append(features, "library_hash")
	}

	var libraryFiles api.LibraryFiles
	// An upload carries an absolute path, so downloads work without a music
	// root; relative library paths still resolve against it.
	if cfg.MusicRoot != "" || cfg.UploadDir != "" {
		libraryFiles = library.FileStore{DB: db, Root: cfg.MusicRoot}
		features = append(features, "library_files")
	}

	var libraryUploads api.LibraryUploads
	var libraryIngest api.LibraryIngestor
	if cfg.UploadDir != "" {
		libraryUploads = library.NewUploadStore(cfg.UploadDir)
		libraryIngest = library.Ingestor{DB: db}
		features = append(features, "library_upload")
	}

	var enricher api.MetadataEnricher
	var enrichStats api.MetadataStats
	if cfg.MusicBrainzURL != "" {
		client, err := metadata.NewMusicBrainz(cfg.MusicBrainzURL, cfg.MusicBrainzAgent)
		if err != nil {
			return err
		}
		backfill := metadata.Backfill{DB: db, Lookup: client}
		if cfg.AcoustIDKey != "" {
			acoustID, err := metadata.NewAcoustID(cfg.AcoustIDURL, cfg.AcoustIDKey)
			if err != nil {
				return err
			}
			backfill.Fingerprinter = metadata.FPCalc{Path: cfg.FPCalcPath}
			backfill.Fingerprints = acoustID
			features = append(features, "fingerprint")
		}
		enricher = backfill
		enrichStats = backfill
		features = append(features, "metadata_enrich")
	}

	var youtubeImport api.YouTubeLikeImport
	var youtubePlaylists api.YouTubePlaylistImport
	var youtubeHealth api.HealthChecker
	if cfg.YTMusicURL != "" {
		client, err := ytmusic.NewClient(cfg.YTMusicURL)
		if err != nil {
			return err
		}
		importer := connectors.YouTubeImporter{DB: db, Client: client}
		youtubeImport = importer
		youtubePlaylists = importer
		youtubeHealth = client
		processor.Executors[connectors.ProviderYouTube] = connectors.YouTubeRemoteWriteExecutor(db, client)
		features = append(features, "youtube")
	}

	var listenBrainz api.ListenBrainzImport
	if cfg.ListenBrainzURL != "" {
		client, err := listenbrainz.NewClient(cfg.ListenBrainzURL)
		if err != nil {
			return err
		}
		listenBrainz = connectors.ListenBrainzImporter{DB: db, Client: client}
		if cfg.ListenBrainzToken != "" {
			processor.Executors[connectors.ProviderListenBrainz] = connectors.ListenBrainzRemoteWriteExecutor(db, client, cfg.ListenBrainzToken)
		}
		features = append(features, "listenbrainz")
	}

	stream := api.NewEventStream()
	h := api.New(api.Deps{
		Logger:             logger,
		Version:            version(),
		Proto:              cfg.Proto,
		Schema:             store.SchemaVersion,
		ReleasesURL:        cfg.ReleasesURL,
		SourceURL:          cfg.SourceURL,
		MinBuild:           cfg.MinBuild,
		RecommendedBuild:   cfg.RecommendedBuild,
		Features:           features,
		DB:                 db,
		ReviewQueue:        queueRepo,
		ReviewQueueActions: queueRepo,
		Connectors:         connectorRepo,
		ConnectorActions:   connectorRepo,
		SpotifyLibrary:     connectorRepo,
		Devices:            deviceRepo,
		EventLog:           eventStore,
		EventStream:        stream,
		Mix:                eventStore,
		Acquisition:        acquisitionRepo,
		AcquisitionSwitch:  acquisitionRepo,
		LibrarySync:        libraryMirror,
		Library:            libraryRepo,
		LibraryHashes:      libraryHashes,
		LibraryFiles:       libraryFiles,
		LibraryUploads:     libraryUploads,
		LibraryIngest:      libraryIngest,
		Enrich:             enricher,
		EnrichStats:        enrichStats,
		SpotifyImport: connectors.SpotifyImporter{
			DB:     db,
			Client: connectors.SpotifyClient{},
			Tokens: connectors.SpotifyRepoTokenSource{Repo: connectorRepo},
		},
		SpotifyPlaylists: connectors.SpotifyImporter{
			DB:     db,
			Client: connectors.SpotifyClient{},
			Tokens: connectors.SpotifyRepoTokenSource{Repo: connectorRepo},
		},
		Playlists:                libraryRepo,
		YouTubeImport:            youtubeImport,
		YouTubePlaylists:         youtubePlaylists,
		YouTubeHealth:            youtubeHealth,
		NavidromeHealth:          navidromeHealth,
		ListenBrainzImport:       listenBrainz,
		ConnectorHealth:          connectors.HealthRepo{DB: db},
		Transfer:                 transfer.Planner{DB: db, Devices: deviceRepo},
		Rollout:                  queueRepo,
		AdminToken:               adminToken,
		SpotifyClientID:          strings.TrimSpace(getenv("SYNCD_SPOTIFY_CLIENT_ID")),
		SpotifyClientSecret:      strings.TrimSpace(getenv("SYNCD_SPOTIFY_CLIENT_SECRET")),
		SpotifyReturnToWhitelist: strings.TrimSpace(getenv("SYNCD_SPOTIFY_RETURN_TO_WHITELIST")),
	})

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()
	go runQueueLoop(ctx, logger, processor)
	go runLibraryLoop(ctx, logger, libraryMirror, cfg.LibrarySyncInterval)
	go runHashLoop(ctx, logger, libraryHashes, cfg.HashInterval, cfg.HashLimit)
	go runEnrichLoop(ctx, logger, enricher, cfg.EnrichInterval, cfg.EnrichLimit)
	// PKCE janitor cleans up expired short-lived PKCE states.
	go runPKCEJanitor(ctx, logger, connectorRepo, 5*time.Minute)

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("syncd: serve: %w", err)
		}
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownWait)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("syncd: shutdown: %w", err)
	}

	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("syncd: serve: %w", err)
	}
	return nil
}

func runQueueLoop(ctx context.Context, logger interface{ Warn(string, ...any) }, processor writeback.Processor) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if _, err := processor.ProcessDue(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Warn("queue processor failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runPKCEJanitor(ctx context.Context, logger interface{ Warn(string, ...any) }, repo *connectors.Repo, interval time.Duration) {
	if repo == nil || interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := repo.CleanupExpiredPKCEStates(ctx, time.Now()); err != nil {
				logger.Warn("pkce janitor failed", "error", err)
			}
		}
	}
}

func runLibraryLoop(ctx context.Context, logger interface{ Warn(string, ...any) }, mirror api.LibraryMirror, interval time.Duration) {
	if mirror == nil || interval <= 0 {
		return
	}
	syncLibrary(ctx, logger, mirror)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			syncLibrary(ctx, logger, mirror)
		}
	}
}

func syncLibrary(ctx context.Context, logger interface{ Warn(string, ...any) }, mirror api.LibraryMirror) {
	if _, err := mirror.Sync(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Warn("library sync failed", "error", err)
	}
}

func runHashLoop(ctx context.Context, logger interface{ Warn(string, ...any) }, hashes api.LibraryHasher, interval time.Duration, limit int) {
	if hashes == nil || interval <= 0 {
		return
	}
	hashFiles(ctx, logger, hashes, limit)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hashFiles(ctx, logger, hashes, limit)
		}
	}
}

func hashFiles(ctx context.Context, logger interface{ Warn(string, ...any) }, hashes api.LibraryHasher, limit int) {
	if _, err := hashes.Run(ctx, limit); err != nil && !errors.Is(err, context.Canceled) {
		logger.Warn("library hashing failed", "error", err)
	}
}

func runEnrichLoop(ctx context.Context, logger interface{ Warn(string, ...any) }, enricher api.MetadataEnricher, interval time.Duration, limit int) {
	if enricher == nil || interval <= 0 {
		return
	}
	enrichTracks(ctx, logger, enricher, limit)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			enrichTracks(ctx, logger, enricher, limit)
		}
	}
}

func enrichTracks(ctx context.Context, logger interface{ Warn(string, ...any) }, enricher api.MetadataEnricher, limit int) {
	if _, err := enricher.Run(ctx, limit); err != nil && !errors.Is(err, context.Canceled) {
		logger.Warn("metadata enrichment failed", "error", err)
	}
}

func version() string {
	if v := os.Getenv("SYNCD_VERSION"); v != "" {
		return v
	}
	return "dev"
}
