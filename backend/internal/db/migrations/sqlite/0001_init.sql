-- Migration 0001 (SQLite dialect): development/CI mirror of the PostgreSQL schema.
-- Timestamps are stored as fixed-width UTC text for correct lexical comparison.

CREATE TABLE IF NOT EXISTS sources (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    website_url TEXT,
    source_type TEXT NOT NULL,
    default_language TEXT,
    trust_weight INTEGER NOT NULL DEFAULT 3,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS feeds (
    id TEXT PRIMARY KEY,
    source_id TEXT NOT NULL REFERENCES sources(id),
    title TEXT NOT NULL DEFAULT '',
    xml_url TEXT NOT NULL,
    normalized_xml_url TEXT NOT NULL UNIQUE,
    html_url TEXT,
    language TEXT,
    scope TEXT,
    category_key TEXT,
    source_type TEXT NOT NULL,
    priority INTEGER NOT NULL DEFAULT 3,
    poll_tier TEXT NOT NULL DEFAULT 'NORMAL',
    enabled INTEGER NOT NULL DEFAULT 1,
    health_status TEXT NOT NULL DEFAULT 'UNKNOWN',
    health_score INTEGER NOT NULL DEFAULT 100,
    etag TEXT,
    last_modified TEXT,
    last_checked_at TEXT,
    last_success_at TEXT,
    newest_item_at TEXT,
    next_poll_at TEXT,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    needs_review INTEGER NOT NULL DEFAULT 0,
    lease_owner TEXT,
    lease_until TEXT,
    feed_pack_version TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_feeds_due ON feeds(enabled, next_poll_at);
CREATE INDEX IF NOT EXISTS idx_feeds_source ON feeds(source_id);
CREATE INDEX IF NOT EXISTS idx_feeds_health ON feeds(health_status);
CREATE INDEX IF NOT EXISTS idx_feeds_category ON feeds(category_key);
CREATE INDEX IF NOT EXISTS idx_feeds_lease ON feeds(enabled, lease_until);

CREATE TABLE IF NOT EXISTS categories (
    id TEXT PRIMARY KEY,
    name_en TEXT NOT NULL,
    name_fa TEXT,
    name_ps TEXT,
    enabled INTEGER NOT NULL DEFAULT 1,
    sort_order INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS provinces (
    id TEXT PRIMARY KEY,
    name_en TEXT NOT NULL,
    name_fa TEXT,
    name_ps TEXT,
    sort_order INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS article_clusters (
    id TEXT PRIMARY KEY,
    representative_article_id TEXT,
    normalized_topic_key TEXT,
    first_seen_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    article_count INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS articles (
    id TEXT PRIMARY KEY,
    source_id TEXT NOT NULL REFERENCES sources(id),
    feed_id TEXT REFERENCES feeds(id),
    external_guid TEXT,
    canonical_url TEXT NOT NULL,
    original_url TEXT NOT NULL,
    normalized_url TEXT NOT NULL,
    title TEXT NOT NULL,
    normalized_title TEXT NOT NULL,
    summary TEXT,
    feed_content TEXT,
    image_url TEXT,
    author TEXT,
    published_at TEXT,
    updated_at TEXT,
    discovered_at TEXT NOT NULL,
    language TEXT,
    is_breaking INTEGER NOT NULL DEFAULT 0,
    breaking_score REAL NOT NULL DEFAULT 0,
    cluster_id TEXT,
    content_hash TEXT,
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_articles_published ON articles(published_at DESC, discovered_at DESC);
CREATE INDEX IF NOT EXISTS idx_articles_source ON articles(source_id, published_at DESC);
CREATE INDEX IF NOT EXISTS idx_articles_feed ON articles(feed_id, published_at DESC);
CREATE INDEX IF NOT EXISTS idx_articles_url ON articles(normalized_url);
CREATE INDEX IF NOT EXISTS idx_articles_hash ON articles(content_hash);
CREATE INDEX IF NOT EXISTS idx_articles_cluster ON articles(cluster_id);
CREATE INDEX IF NOT EXISTS idx_articles_language ON articles(language, published_at DESC);
CREATE INDEX IF NOT EXISTS idx_articles_breaking ON articles(is_breaking, published_at DESC);
CREATE INDEX IF NOT EXISTS idx_articles_guid ON articles(feed_id, external_guid);

CREATE TABLE IF NOT EXISTS article_categories (
    article_id TEXT NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    category_id TEXT NOT NULL REFERENCES categories(id),
    confidence REAL,
    origin TEXT NOT NULL,
    PRIMARY KEY (article_id, category_id)
);

CREATE INDEX IF NOT EXISTS idx_article_categories_category ON article_categories(category_id, article_id);

CREATE TABLE IF NOT EXISTS article_provinces (
    article_id TEXT NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    province_id TEXT NOT NULL REFERENCES provinces(id),
    confidence REAL,
    origin TEXT NOT NULL,
    PRIMARY KEY (article_id, province_id)
);

CREATE INDEX IF NOT EXISTS idx_article_provinces_province ON article_provinces(province_id, article_id);

CREATE TABLE IF NOT EXISTS feed_health_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    feed_id TEXT NOT NULL REFERENCES feeds(id),
    checked_at TEXT NOT NULL,
    http_status INTEGER,
    parse_ok INTEGER,
    item_count INTEGER,
    duration_ms INTEGER,
    event_type TEXT NOT NULL,
    error_code TEXT,
    message TEXT
);

CREATE INDEX IF NOT EXISTS idx_feed_health_events_feed_time ON feed_health_events(feed_id, checked_at DESC);

CREATE TABLE IF NOT EXISTS feed_fetch_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    feed_id TEXT NOT NULL REFERENCES feeds(id),
    started_at TEXT NOT NULL,
    finished_at TEXT,
    result TEXT NOT NULL,
    http_status INTEGER,
    bytes_received INTEGER,
    items_seen INTEGER,
    items_inserted INTEGER,
    duplicates INTEGER,
    request_id TEXT
);

CREATE INDEX IF NOT EXISTS idx_feed_fetch_runs_feed_time ON feed_fetch_runs(feed_id, started_at DESC);

CREATE TABLE IF NOT EXISTS feed_pack_imports (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    version TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    imported_at TEXT NOT NULL,
    imported_by TEXT,
    feed_count INTEGER NOT NULL,
    inserted_count INTEGER NOT NULL DEFAULT 0,
    updated_count INTEGER NOT NULL DEFAULT 0,
    unchanged_count INTEGER NOT NULL DEFAULT 0,
    missing_count INTEGER NOT NULL DEFAULT 0,
    invalid_count INTEGER NOT NULL DEFAULT 0,
    disabled_count INTEGER NOT NULL DEFAULT 0,
    committed INTEGER NOT NULL DEFAULT 0,
    validation_errors TEXT,
    dry_run TEXT
);

CREATE TABLE IF NOT EXISTS article_opportunities (
    article_id TEXT PRIMARY KEY REFERENCES articles(id) ON DELETE CASCADE,
    organization TEXT,
    location TEXT,
    deadline TEXT,
    employment_type TEXT,
    opportunity_type TEXT,
    reference_number TEXT
);

CREATE INDEX IF NOT EXISTS idx_article_opportunities_deadline ON article_opportunities(deadline);

CREATE TABLE IF NOT EXISTS source_aliases (
    alias TEXT PRIMARY KEY,
    source_id TEXT NOT NULL REFERENCES sources(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS push_registrations (
    id TEXT PRIMARY KEY,
    token TEXT NOT NULL UNIQUE,
    platform TEXT NOT NULL,
    app_version TEXT,
    language TEXT,
    active INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    last_seen_at TEXT
);

CREATE TABLE IF NOT EXISTS push_subscriptions (
    registration_id TEXT NOT NULL REFERENCES push_registrations(id) ON DELETE CASCADE,
    topic TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (registration_id, topic)
);

CREATE INDEX IF NOT EXISTS idx_push_subscriptions_topic ON push_subscriptions(topic);

CREATE TABLE IF NOT EXISTS push_events (
    id TEXT PRIMARY KEY,
    article_id TEXT,
    topic TEXT NOT NULL,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    sent_at TEXT,
    status TEXT NOT NULL,
    dedup_key TEXT NOT NULL UNIQUE,
    audience INTEGER NOT NULL DEFAULT 0,
    actor TEXT,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_push_events_created ON push_events(created_at DESC);

CREATE TABLE IF NOT EXISTS notification_rules (
    id TEXT PRIMARY KEY,
    topic TEXT NOT NULL,
    category_key TEXT,
    min_priority INTEGER NOT NULL DEFAULT 4,
    min_breaking_score REAL NOT NULL DEFAULT 0.6,
    max_per_hour INTEGER NOT NULL DEFAULT 2,
    auto_send INTEGER NOT NULL DEFAULT 0,
    enabled INTEGER NOT NULL DEFAULT 1,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS app_config (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    updated_by TEXT
);

CREATE TABLE IF NOT EXISTS admin_users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL,
    active INTEGER NOT NULL DEFAULT 1,
    mfa_enabled INTEGER NOT NULL DEFAULT 0,
    last_login_at TEXT,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS admin_audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    actor TEXT NOT NULL,
    action TEXT NOT NULL,
    entity TEXT NOT NULL,
    entity_id TEXT NOT NULL DEFAULT '',
    before_state TEXT,
    after_state TEXT,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_admin_audit_created ON admin_audit_log(created_at DESC);
