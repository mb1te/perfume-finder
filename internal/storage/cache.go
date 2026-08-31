package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"parfumes_finder/internal/domain"
)

type Cache struct {
	db  *sql.DB
	ttl time.Duration
	now func() time.Time
}

func NewCache(db *sql.DB, ttl time.Duration, now func() time.Time) *Cache {
	return &Cache{db: db, ttl: ttl, now: now}
}

func (cache *Cache) Get(ctx context.Context, query domain.SearchQuery) ([]domain.Offer, bool, error) {
	key, queryJSON, err := cacheKey(query)
	if err != nil {
		return nil, false, err
	}

	var offersJSON []byte
	var createdAt int64
	err = cache.db.QueryRowContext(ctx, `SELECT offers_json, created_at FROM search_cache WHERE cache_key = ?`, key).Scan(&offersJSON, &createdAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read search cache: %w", err)
	}
	if !time.Unix(0, createdAt).Add(cache.ttl).After(cache.now()) {
		_, _ = cache.db.ExecContext(ctx, `DELETE FROM search_cache WHERE cache_key = ?`, key)
		return nil, false, nil
	}

	var offers []domain.Offer
	if err := json.Unmarshal(offersJSON, &offers); err != nil {
		return nil, false, fmt.Errorf("decode cached offers for %s: %w", string(queryJSON), err)
	}
	return offers, true, nil
}

func (cache *Cache) Put(ctx context.Context, query domain.SearchQuery, offers []domain.Offer) error {
	key, queryJSON, err := cacheKey(query)
	if err != nil {
		return err
	}
	offersJSON, err := json.Marshal(offers)
	if err != nil {
		return fmt.Errorf("encode cached offers: %w", err)
	}
	_, err = cache.db.ExecContext(ctx, `
        INSERT INTO search_cache(cache_key, query_json, offers_json, created_at)
        VALUES(?, ?, ?, ?)
        ON CONFLICT(cache_key) DO UPDATE SET
            query_json = excluded.query_json,
            offers_json = excluded.offers_json,
            created_at = excluded.created_at
    `, key, queryJSON, offersJSON, cache.now().UnixNano())
	if err != nil {
		return fmt.Errorf("write search cache: %w", err)
	}
	return nil
}

func cacheKey(query domain.SearchQuery) (string, []byte, error) {
	canonical := struct {
		Raw               string               `json:"raw"`
		Brand             string               `json:"brand"`
		Name              string               `json:"name"`
		Edition           string               `json:"edition"`
		Concentration     domain.Concentration `json:"concentration"`
		VolumeMicroliters int                  `json:"volume_microliters"`
		Kind              domain.ProductKind   `json:"kind"`
	}{
		Raw:               domain.NormalizeText(query.Raw),
		Brand:             domain.NormalizeText(query.Brand),
		Name:              domain.NormalizeText(query.Name),
		Edition:           domain.NormalizeText(query.Edition),
		Concentration:     query.Concentration,
		VolumeMicroliters: query.VolumeMicroliters,
		Kind:              query.Kind,
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", nil, fmt.Errorf("encode cache query: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), encoded, nil
}
