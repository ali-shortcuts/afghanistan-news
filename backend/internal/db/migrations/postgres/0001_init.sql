-- Migration 0001: canonical PostgreSQL schema (architecture v0.4 §241-§256).

CREATE TABLE IF NOT EXISTS sources (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    website_url TEXT,
    source_type TEXT NOT NULL,
    default_language TEXT,
    trust_weight SMALLINT NOT NULL DEFAULT 3,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (trust_weight BETWEEN 1 AND 5)
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
    priority SMALLINT NOT NULL DEFAULT 3,
    poll_tier TEXT NOT NULL DEFAULT 'NORMAL',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    health_status TEXT NOT NULL DEFAULT 'UNKNOWN',
    health_score INTEGER NOT NULL DEFAULT 100,
    etag TEXT,
    last_modified TEXT,
    last_checked_at TIMESTAMPTZ,
    last_success_at TIMESTAMPTZ,
    newest_item_at TIMESTAMPTZ,
    next_poll_at TIMESTAMPTZ,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    needs_review BOOLEAN NOT NULL DEFAULT FALSE,
    lease_owner TEXT,
    lease_until TIMESTAMPTZ,
    feed_pack_version TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (priority BETWEEN 1 AND 5)
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
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
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
    first_seen_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
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
    published_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ,
    discovered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    language TEXT,
    is_breaking BOOLEAN NOT NULL DEFAULT FALSE,
    breaking_score REAL NOT NULL DEFAULT 0,
    cluster_id TEXT REFERENCES article_clusters(id),
    content_hash TEXT,
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_articles_published ON articles(published_at DESC NULLS LAST, discovered_at DESC);
CREATE INDEX IF NOT EXISTS idx_articles_source ON articles(source_id, published_at DESC);
CREATE INDEX IF NOT EXISTS idx_articles_feed ON articles(feed_id, published_at DESC);
CREATE INDEX IF NOT EXISTS idx_articles_url ON articles(normalized_url);
CREATE INDEX IF NOT EXISTS idx_articles_hash ON articles(content_hash);
CREATE INDEX IF NOT EXISTS idx_articles_cluster ON articles(cluster_id);
CREATE INDEX IF NOT EXISTS idx_articles_language ON articles(language, published_at DESC);
CREATE INDEX IF NOT EXISTS idx_articles_breaking ON articles(is_breaking, published_at DESC) WHERE is_breaking = TRUE;
CREATE INDEX IF NOT EXISTS idx_articles_guid ON articles(feed_id, external_guid);
CREATE INDEX IF NOT EXISTS idx_articles_search ON articles USING GIN (to_tsvector('simple', title || ' ' || coalesce(summary, '')));

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
    id BIGSERIAL PRIMARY KEY,
    feed_id TEXT NOT NULL REFERENCES feeds(id),
    checked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    http_status INTEGER,
    parse_ok BOOLEAN,
    item_count INTEGER,
    duration_ms INTEGER,
    event_type TEXT NOT NULL,
    error_code TEXT,
    message TEXT
);

CREATE INDEX IF NOT EXISTS idx_feed_health_events_feed_time ON feed_health_events(feed_id, checked_at DESC);

CREATE TABLE IF NOT EXISTS feed_fetch_runs (
    id BIGSERIAL PRIMARY KEY,
    feed_id TEXT NOT NULL REFERENCES feeds(id),
    started_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    result TEXT NOT NULL,
    http_status INTEGER,
    bytes_received BIGINT,
    items_seen INTEGER,
    items_inserted INTEGER,
    duplicates INTEGER,
    request_id TEXT
);

CREATE INDEX IF NOT EXISTS idx_feed_fetch_runs_feed_time ON feed_fetch_runs(feed_id, started_at DESC);

CREATE TABLE IF NOT EXISTS feed_pack_imports (
    id BIGSERIAL PRIMARY KEY,
    version TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    imported_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    imported_by TEXT,
    feed_count INTEGER NOT NULL,
    inserted_count INTEGER NOT NULL DEFAULT 0,
    updated_count INTEGER NOT NULL DEFAULT 0,
    unchanged_count INTEGER NOT NULL DEFAULT 0,
    missing_count INTEGER NOT NULL DEFAULT 0,
    invalid_count INTEGER NOT NULL DEFAULT 0,
    disabled_count INTEGER NOT NULL DEFAULT 0,
    committed BOOLEAN NOT NULL DEFAULT FALSE,
    validation_errors TEXT,
    dry_run JSONB
);

CREATE TABLE IF NOT EXISTS article_opportunities (
    article_id TEXT PRIMARY KEY REFERENCES articles(id) ON DELETE CASCADE,
    organization TEXT,
    location TEXT,
    deadline TIMESTAMPTZ,
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
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS push_subscriptions (
    registration_id TEXT NOT NULL REFERENCES push_registrations(id) ON DELETE CASCADE,
    topic TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (registration_id, topic)
);

CREATE INDEX IF NOT EXISTS idx_push_subscriptions_topic ON push_subscriptions(topic);

CREATE TABLE IF NOT EXISTS push_events (
    id TEXT PRIMARY KEY,
    article_id TEXT REFERENCES articles(id),
    topic TEXT NOT NULL,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    sent_at TIMESTAMPTZ,
    status TEXT NOT NULL,
    dedup_key TEXT NOT NULL UNIQUE,
    audience INTEGER NOT NULL DEFAULT 0,
    actor TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_push_events_created ON push_events(created_at DESC);

CREATE TABLE IF NOT EXISTS notification_rules (
    id TEXT PRIMARY KEY,
    topic TEXT NOT NULL,
    category_key TEXT,
    min_priority SMALLINT NOT NULL DEFAULT 4,
    min_breaking_score REAL NOT NULL DEFAULT 0.6,
    max_per_hour INTEGER NOT NULL DEFAULT 2,
    auto_send BOOLEAN NOT NULL DEFAULT FALSE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS app_config (
    key TEXT PRIMARY KEY,
    value JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by TEXT
);

CREATE TABLE IF NOT EXISTS admin_users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    mfa_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    last_login_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS admin_audit_log (
    id BIGSERIAL PRIMARY KEY,
    actor TEXT NOT NULL,
    action TEXT NOT NULL,
    entity TEXT NOT NULL,
    entity_id TEXT NOT NULL DEFAULT '',
    before_state TEXT,
    after_state TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_admin_audit_created ON admin_audit_log(created_at DESC);
