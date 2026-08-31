PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;
PRAGMA busy_timeout = 5000;

CREATE TABLE IF NOT EXISTS shops (
    shop_id TEXT PRIMARY KEY,
    display_name TEXT NOT NULL DEFAULT '',
    network_domain TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 0,
    trust_state TEXT NOT NULL DEFAULT 'candidate',
    updated_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS search_cache (
    cache_key TEXT PRIMARY KEY,
    query_json BLOB NOT NULL,
    offers_json BLOB NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS telegram_sessions (
    chat_id INTEGER PRIMARY KEY,
    stage TEXT NOT NULL,
    query_json BLOB NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS shop_health (
    shop_id TEXT NOT NULL,
    probe_kind TEXT NOT NULL,
    status TEXT NOT NULL,
    canonical_url TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    checked_at INTEGER NOT NULL,
    PRIMARY KEY (shop_id, probe_kind)
);
