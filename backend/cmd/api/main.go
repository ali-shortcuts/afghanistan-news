// Command api serves the public mobile API, the admin API, the admin console bundle and
// the mobile web preview (§113, §281).
//
// The API process never fetches external RSS in the request path (§112). When
// WORKER_ENABLED=true the ingestion worker runs inside the same process for
// single-node deployments; the worker can also run standalone from cmd/worker.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/afnews/backend/internal/config"
	"github.com/afnews/backend/internal/db"
	"github.com/afnews/backend/internal/feed/opml"
	"github.com/afnews/backend/internal/httpapi"
	"github.com/afnews/backend/internal/ingest"
	"github.com/afnews/backend/internal/observability"
	"github.com/afnews/backend/internal/push"
	"github.com/afnews/backend/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	log := observability.NewLogger(cfg.LogLevel, "api")
	metrics := observability.NewMetrics()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	database, err := db.Open(ctx, cfg.DBDriver, cfg.DatabaseURL, cfg.SQLitePath)
	if err != nil {
		log.Error("database connection failed", "error", err, "driver", cfg.DBDriver)
		os.Exit(1)
	}
	if err := database.Migrate(ctx); err != nil {
		log.Error("migration failed", "error", err)
		os.Exit(1)
	}
	// A restore or bulk load can leave a sequence behind its table; check before serving.
	if n, err := database.SyncSequences(ctx); err != nil {
		log.Warn("sequence verification failed", "error", err)
	} else if n > 0 {
		log.Info("sequences verified", "serial_columns", n)
	}
	st := store.New(database)
	if err := st.SeedReference(ctx); err != nil {
		log.Error("reference seed failed", "error", err)
		os.Exit(1)
	}
	if err := st.EnsureAdminSeed(ctx, cfg.AdminUsername, cfg.AdminPasswordHash, cfg.AllowDevAdminSeed); err != nil {
		log.Error("admin seed failed", "error", err)
		os.Exit(1)
	}

	// Push sender: FCM when credentials exist, dry-run otherwise (§39).
	var sender push.Sender = &push.DryRunSender{Log: log}
	if creds, err := os.ReadFile(cfg.FCMCredentialsFile); err == nil && !cfg.PushDryRun {
		sender = &push.FCMSender{ProjectID: cfg.FCMProjectID, Credentials: creds, Log: log}
		log.Info("FCM sender configured", "project", cfg.FCMProjectID)
	} else if !cfg.PushDryRun {
		log.Warn("PUSH_DRY_RUN is false but no FCM credentials were found; using dry-run sender")
	}

	// Optional in-process worker.
	var tester httpapi.FeedTester
	var pipeline *ingest.Pipeline
	if cfg.WorkerEnabled {
		pipeline = ingest.New(st, sender, metrics, log.With("component", "worker"), ingest.Options{
			Owner:             hostname() + "-embeddded",
			BatchSize:         cfg.IngestBatchSize,
			Concurrency:       cfg.WorkerConcurrency,
			SchedulerTick:     cfg.SchedulerTick,
			MaxItemsPerRun:    cfg.MaxItemsPerFeedRun,
			AdaptivePolling:   cfg.AdaptivePolling,
			QuarantineAfter:   cfg.QuarantineAfter,
			MaxBreakingPerRun: cfg.MaxBreakingPerRun,
			DomainConcurrency: cfg.DomainConcurrency,
			RequestsPerMinute: 30,
			UserAgent:         cfg.UserAgent,
			FetchTimeout:      cfg.FetchTimeout,
			MaxResponseBytes:  cfg.MaxResponseBytes,
			AllowPrivateFetch: cfg.AllowPrivateFetch,
			PushEnabled:       true, // delivery vs. dry-run is decided by the sender (see PushDryRun)
			PushDryRun:        cfg.PushDryRun,
			PushMaxPerHour:    cfg.PushMaxPerHour,
			PushMaxPerDay:     cfg.PushMaxPerDay,
		})
		tester = pipeline
		go func() {
			if err := pipeline.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.Error("ingestion worker stopped", "error", err)
			}
		}()
		log.Info("embedded ingestion worker enabled")
	}

	// Bootstrap the feed registry from the bundled feed pack when the registry is empty
	// (§31: the bundled OPML is bootstrap/fallback, the database is production authority).
	if cfg.FeedPackPath != "" {
		if n, err := st.EnabledFeedCount(ctx); err == nil && n == 0 {
			if doc, err := opml.ParseFile(cfg.FeedPackPath); err == nil {
				result, err := st.ImportFeedPack(ctx, doc, doc.FeedPackVer, "bootstrap", false)
				if err != nil {
					log.Warn("bootstrap feed pack import failed", "error", err)
				} else {
					log.Info("bootstrap feed pack imported", "version", result.Version,
						"total", result.Total, "inserted", result.Inserted, "invalid", result.Invalid)
					// Production activation is selective, never the whole catalog (§268, §310).
					enabledFeeds, disabledFeeds, waveErr := st.ApplyActivationWave(ctx, store.ActivationWave(cfg.ActivationWave))
					if waveErr != nil {
						log.Warn("activation wave failed", "error", waveErr)
					} else {
						log.Info("activation wave applied", "wave", cfg.ActivationWave,
							"enabled", enabledFeeds, "disabled", disabledFeeds)
					}
				}
			} else {
				log.Warn("bundled feed pack could not be parsed", "error", err, "path", cfg.FeedPackPath)
			}
		}
	}

	srv := httpapi.NewService(httpapi.Options{
		Config:     cfg,
		Store:      st,
		Metrics:    metrics,
		Log:        log,
		Pusher:     sender,
		Tester:     tester,
		SessionKey: cfg.AdminSessionKey,
		SessionTTL: cfg.AdminSessionTTL,
	})

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	go func() {
		log.Info("api listening", "addr", cfg.HTTPAddr, "env", cfg.Env, "driver", cfg.DBDriver)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server failed", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutdown requested")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Warn("graceful shutdown failed", "error", err)
	}
	if pipeline != nil {
		// Give in-flight feed fetches a moment to persist their results.
		time.Sleep(500 * time.Millisecond)
	}
	_ = database.Close()
	log.Info("api stopped")
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil || strings.TrimSpace(h) == "" {
		return "api"
	}
	return h
}

var _ = slog.LevelInfo
