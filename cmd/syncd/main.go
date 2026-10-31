package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Vic-41148/groovesync/internal/acquisition"
	"github.com/Vic-41148/groovesync/internal/api"
	"github.com/Vic-41148/groovesync/internal/config"
	"github.com/Vic-41148/groovesync/internal/connectors"
	"github.com/Vic-41148/groovesync/internal/devices"
	"github.com/Vic-41148/groovesync/internal/library"
	"github.com/Vic-41148/groovesync/internal/listenbrainz"
	"github.com/Vic-41148/groovesync/internal/logging"
	"github.com/Vic-41148/groovesync/internal/metadata"
	"github.com/Vic-41148/groovesync/internal/navidrome"
	"github.com/Vic-41148/groovesync/internal/store"
	"github.com/Vic-41148/groovesync/internal/taste"
	"github.com/Vic-41148/groovesync/internal/transfer"
	"github.com/Vic-41148/groovesync/internal/writeback"
	"github.com/Vic-41148/groovesync/internal/ytmusic"
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

	logger := logging.New(cfg.LogLevel)
	db, err := store.Open(parent, cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()

	connectorRepo := connectors.NewRepo(db)
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
	if cfg.NavidromeURL != "" {
		if cfg.NavidromeUser == "" || cfg.NavidromePassword == "" {
			return errors.New("syncd: a Navidrome URL needs SYNCD_NAVIDROME_USER and SYNCD_NAVIDROME_PASSWORD")
		}
		client, err := navidrome.NewClient(cfg.NavidromeURL, cfg.NavidromeUser, cfg.NavidromePassword)
		if err != nil {
			return err
		}
		libraryMirror = navidrome.Mirror{DB: db, Client: client, Ignore: cfg.IgnoreGlobs}
		features = append(features, "library_sync")
	}

	var libraryHashes api.LibraryHasher
	if cfg.MusicRoot != "" || cfg.HashInterval > 0 {
		libraryHashes = library.HashBackfill{DB: db, Root: cfg.MusicRoot}
		features = append(features, "library_hash")
	}

	var enricher api.MetadataEnricher
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

	h := api.New(api.Deps{
		Logger:             logger,
		Version:            version(),
		Proto:              cfg.Proto,
		Schema:             store.SchemaVersion,
		ReleasesURL:        cfg.ReleasesURL,
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
		Mix:                eventStore,
		Acquisition:        acquisitionRepo,
		AcquisitionSwitch:  acquisitionRepo,
		LibrarySync:        libraryMirror,
		Library:            libraryRepo,
		LibraryHashes:      libraryHashes,
		Enrich:             enricher,
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
		Playlists:          libraryRepo,
		YouTubeImport:      youtubeImport,
		YouTubePlaylists:   youtubePlaylists,
		YouTubeHealth:      youtubeHealth,
		ListenBrainzImport: listenBrainz,
		ConnectorHealth:    connectors.HealthRepo{DB: db},
		Transfer:           transfer.Planner{DB: db, Devices: deviceRepo},
		Rollout:            queueRepo,
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
