// Package config loads and validates runtime configuration for the API and worker processes.
//
// All configuration is environment-variable based so that the same binaries can run
// in local, staging and production without modification.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the complete runtime configuration.
type Config struct {
	Env      string // local | staging | production
	HTTPAddr string

	// Storage
	DatabaseURL string
	DBDriver    string // postgres | sqlite
	SQLitePath  string

	// Feed ingestion
	FeedPackPath       string
	ActivationWave     int
	WorkerEnabled      bool
	WorkerConcurrency  int
	DomainConcurrency  int
	MaxResponseBytes   int64
	FetchTimeout       time.Duration
	UserAgent          string
	AllowPrivateFetch  bool // test-only escape hatch; never enable in production
	IngestBatchSize    int
	SchedulerTick      time.Duration
	AdaptivePolling    bool
	MaxItemsPerFeedRun int
	QuarantineAfter    int
	MaxBreakingPerRun  int

	// API
	RateLimitPerMinute int
	CORSAllowedOrigins []string

	// Admin
	AdminUsername     string
	AdminPasswordHash string
	AdminSessionKey   string
	AdminSessionTTL   time.Duration
	AllowDevAdminSeed bool

	// Push
	FCMCredentialsFile string
	FCMProjectID       string
	PushDryRun         bool
	PushMaxPerHour     int
	PushMaxPerDay      int

	// Observability
	LogLevel string
}

// Load reads configuration from the environment, applying safe defaults.
func Load() (*Config, error) {
	c := &Config{
		Env:                env("APP_ENV", "local"),
		HTTPAddr:           env("HTTP_ADDR", "0.0.0.0:8080"),
		DatabaseURL:        env("DATABASE_URL", "postgres://afnews:afnews@127.0.0.1:5432/afnews?sslmode=disable"),
		DBDriver:           env("DB_DRIVER", ""),
		SQLitePath:         env("SQLITE_PATH", "data/afnews.db"),
		FeedPackPath:       env("FEED_PACK_PATH", "resources/feedpacks/afghanistan-global-news-master-v0.3.opml"),
		ActivationWave:     envInt("FEED_ACTIVATION_WAVE", 1),
		WorkerEnabled:      envBool("WORKER_ENABLED", true),
		WorkerConcurrency:  envInt("WORKER_CONCURRENCY", 8),
		DomainConcurrency:  envInt("DOMAIN_CONCURRENCY", 2),
		MaxResponseBytes:   int64(envInt("MAX_RESPONSE_BYTES", 2*1024*1024)),
		FetchTimeout:       envDuration("FETCH_TIMEOUT", 20*time.Second),
		UserAgent:          env("USER_AGENT", "AfghanistanNewsBot/1.0 (+https://afnews.example/bot)"),
		AllowPrivateFetch:  envBool("ALLOW_PRIVATE_FETCH", false),
		IngestBatchSize:    envInt("INGEST_BATCH_SIZE", 24),
		SchedulerTick:      envDuration("SCHEDULER_TICK", 5*time.Second),
		AdaptivePolling:    envBool("ADAPTIVE_POLLING", true),
		MaxItemsPerFeedRun: envInt("MAX_ITEMS_PER_FEED_RUN", 60),
		QuarantineAfter:    envInt("QUARANTINE_AFTER", 12),
		MaxBreakingPerRun:  envInt("BREAKING_MAX_PER_RUN", 2),
		RateLimitPerMinute: envInt("RATE_LIMIT_PER_MINUTE", 240),
		CORSAllowedOrigins: envList("CORS_ALLOWED_ORIGINS", []string{"*"}),
		AdminUsername:      env("ADMIN_USERNAME", "admin"),
		AdminPasswordHash:  env("ADMIN_PASSWORD_HASH", ""),
		AdminSessionKey:    env("ADMIN_SESSION_KEY", ""),
		AdminSessionTTL:    envDuration("ADMIN_SESSION_TTL", 8*time.Hour),
		AllowDevAdminSeed:  envBool("ALLOW_DEV_ADMIN_SEED", true),
		FCMCredentialsFile: env("FCM_CREDENTIALS_FILE", ""),
		FCMProjectID:       env("FCM_PROJECT_ID", ""),
		PushDryRun:         envBool("PUSH_DRY_RUN", true),
		PushMaxPerHour:     envInt("PUSH_MAX_PER_HOUR", 6),
		PushMaxPerDay:      envInt("PUSH_MAX_PER_DAY", 40),
		LogLevel:           env("LOG_LEVEL", "info"),
	}

	if c.DBDriver == "" {
		if strings.HasPrefix(c.DatabaseURL, "sqlite") || strings.HasSuffix(c.DatabaseURL, ".db") {
			c.DBDriver = "sqlite"
		} else {
			c.DBDriver = "postgres"
		}
	}
	if c.DBDriver == "sqlite" && strings.HasPrefix(c.DatabaseURL, "sqlite") {
		c.SQLitePath = strings.TrimPrefix(c.DatabaseURL, "sqlite://")
	}

	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// Validate rejects configurations that are unsafe or incoherent.
func (c *Config) Validate() error {
	switch c.DBDriver {
	case "postgres", "sqlite":
	default:
		return fmt.Errorf("config: unsupported DB_DRIVER %q (want postgres or sqlite)", c.DBDriver)
	}
	if c.WorkerConcurrency < 1 || c.WorkerConcurrency > 128 {
		return fmt.Errorf("config: WORKER_CONCURRENCY out of range: %d", c.WorkerConcurrency)
	}
	if c.DomainConcurrency < 1 || c.DomainConcurrency > 16 {
		return fmt.Errorf("config: DOMAIN_CONCURRENCY out of range: %d", c.DomainConcurrency)
	}
	if c.MaxResponseBytes < 64*1024 {
		return fmt.Errorf("config: MAX_RESPONSE_BYTES too small: %d", c.MaxResponseBytes)
	}
	if c.Env == "production" {
		if c.AllowPrivateFetch {
			return fmt.Errorf("config: ALLOW_PRIVATE_FETCH must be false in production (SSRF guard disabled)")
		}
		if c.AdminSessionKey == "" || len(c.AdminSessionKey) < 32 {
			return fmt.Errorf("config: ADMIN_SESSION_KEY must be set to at least 32 characters in production")
		}
		if c.AdminPasswordHash == "" {
			return fmt.Errorf("config: ADMIN_PASSWORD_HASH must be set in production")
		}
		if c.PushDryRun {
			return fmt.Errorf("config: PUSH_DRY_RUN must be false in production")
		}
	}
	return nil
}

// IsSQLite reports whether the process is backed by the embedded SQLite dialect.
func (c *Config) IsSQLite() bool { return c.DBDriver == "sqlite" }

// IsProduction reports whether the process is running in production mode.
func (c *Config) IsProduction() bool { return c.Env == "production" }

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func envInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(strings.TrimSpace(v)); err == nil {
			return d
		}
	}
	return def
}

func envList(key string, def []string) []string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		parts := strings.Split(v, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if s := strings.TrimSpace(p); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return def
}
