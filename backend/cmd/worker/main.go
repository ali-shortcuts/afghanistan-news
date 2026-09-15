// Command worker runs the standalone ingestion process (§66, §113).
//
// Running the worker separately keeps feed polling, parsing and push generation away
// from public API latency. It uses the same code path as the embedded worker.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/afnews/backend/internal/config"
	"github.com/afnews/backend/internal/db"
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
	log := observability.NewLogger(cfg.LogLevel, "worker")
	metrics := observability.NewMetrics()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	database, err := db.Open(ctx, cfg.DBDriver, cfg.DatabaseURL, cfg.SQLitePath)
	if err != nil {
		log.Error("database connection failed", "error", err)
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

	var sender push.Sender = &push.DryRunSender{Log: log}
	if creds, err := os.ReadFile(cfg.FCMCredentialsFile); err == nil && !cfg.PushDryRun {
		sender = &push.FCMSender{ProjectID: cfg.FCMProjectID, Credentials: creds, Log: log}
	}

	pipeline := ingest.New(st, sender, metrics, log, ingest.Options{
		Owner:             hostname(),
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
		// The worker keeps composing and recording notifications in dry-run mode; only the
		// sender decides whether anything is actually delivered (§39).
		PushEnabled:    true,
		PushDryRun:     cfg.PushDryRun,
		PushMaxPerHour: cfg.PushMaxPerHour,
		PushMaxPerDay:  cfg.PushMaxPerDay,
	})

	// Periodic metrics flush so the worker's counters are visible to monitoring (§174).
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				stats, err := st.DashboardStats(ctx)
				if err != nil {
					log.Warn("stats refresh failed", "error", err)
					continue
				}
				metrics.Set("articles_last_hour", float64(stats.Articles1h))
				metrics.Set("articles_last_24h", float64(stats.Articles24h))
				metrics.Set("feeds_enabled", float64(stats.FeedsEnabled))
				for status, n := range stats.Feeds {
					metrics.Set("feeds_by_health_"+status, float64(n))
				}
				log.Info("worker heartbeat", "feeds_enabled", stats.FeedsEnabled,
					"articles_24h", stats.Articles24h, "push_24h", stats.PushSent24h)
			}
		}
	}()

	if err := pipeline.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("worker stopped with error", "error", err)
		os.Exit(1)
	}
	log.Info("worker stopped")
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "worker"
	}
	return h
}

var _ = slog.LevelInfo
