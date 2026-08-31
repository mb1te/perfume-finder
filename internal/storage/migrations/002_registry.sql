CREATE TABLE IF NOT EXISTS shop_evidence (
    idempotency_key TEXT PRIMARY KEY, network_domain TEXT NOT NULL, display_domain TEXT NOT NULL,
    related_network_domain TEXT NOT NULL DEFAULT '', related_display_domain TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL, page INTEGER NOT NULL, post_url TEXT NOT NULL DEFAULT '', observed_at INTEGER NOT NULL DEFAULT 0, excerpt TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_shop_evidence_domain ON shop_evidence(network_domain);
CREATE TABLE IF NOT EXISTS shop_aliases (network_domain TEXT NOT NULL, related_network_domain TEXT NOT NULL, evidence_id TEXT NOT NULL UNIQUE);
