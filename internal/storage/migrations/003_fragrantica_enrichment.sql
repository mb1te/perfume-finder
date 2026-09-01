CREATE TABLE IF NOT EXISTS fragrantica_enrichment_cache (
    cache_key TEXT PRIMARY KEY,
    source_url TEXT NOT NULL,
    title TEXT NOT NULL,
    accords_json BLOB NOT NULL,
    image_png BLOB NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_fragrantica_enrichment_created_at
    ON fragrantica_enrichment_cache(created_at);
