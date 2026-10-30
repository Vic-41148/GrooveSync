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

	"github.com/Vic-41148/groovesync/internal/api"
	"github.com/Vic-41148/groovesync/internal/config"
	"github.com/Vic-41148/groovesync/internal/logging"
	"github.com/Vic-41148/groovesync/internal/store"
	"github.com/Vic-41148/groovesync/internal/writeback"
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

	queueRepo := writeback.NewRepo(db)
	processor := writeback.Processor{
		Store: queueRepo,
		Now:   time.Now,
	}

	h := api.New(api.Deps{
		Logger:           logger,
		Version:          version(),
		Proto:            cfg.Proto,
		Schema:           store.SchemaVersion,
		ReleasesURL:      cfg.ReleasesURL,
		MinBuild:         cfg.MinBuild,
		RecommendedBuild: cfg.RecommendedBuild,
		Features:         []string{"meta", "review_queue"},
		ReviewQueue:      queueRepo,
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

func version() string {
	if v := os.Getenv("SYNCD_VERSION"); v != "" {
		return v
	}
	return "dev"
}
