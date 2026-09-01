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
	"parfumes_finder/internal/enrichment"
)

const (
	maxEnrichmentImageSize = 5 << 20
	maxEnrichmentCacheSize = 256 << 20
	targetEnrichmentSize   = 192 << 20
)

type EnrichmentCache struct {
	db  *sql.DB
	ttl time.Duration
	now func() time.Time
}

func NewEnrichmentCache(db *sql.DB, ttl time.Duration, now func() time.Time) *EnrichmentCache {
	return &EnrichmentCache{db: db, ttl: ttl, now: now}
}

func (cache *EnrichmentCache) Get(ctx context.Context, request enrichment.Request) (enrichment.Card, bool, error) {
	key, requestJSON, err := enrichmentCacheKey(request)
	if err != nil {
		return enrichment.Card{}, false, err
	}

	var card enrichment.Card
	var accordsJSON []byte
	var createdAt int64
	err = cache.db.QueryRowContext(ctx, `
        SELECT source_url, title, accords_json, image_png, created_at
        FROM fragrantica_enrichment_cache
        WHERE cache_key = ?
    `, key).Scan(&card.SourceURL, &card.Title, &accordsJSON, &card.PNG, &createdAt)
	if err == sql.ErrNoRows {
		return enrichment.Card{}, false, nil
	}
	if err != nil {
		return enrichment.Card{}, false, fmt.Errorf("read enrichment cache: %w", err)
	}
	if !time.Unix(0, createdAt).Add(cache.ttl).After(cache.now()) {
		_, _ = cache.db.ExecContext(ctx, `DELETE FROM fragrantica_enrichment_cache WHERE cache_key = ?`, key)
		return enrichment.Card{}, false, nil
	}
	if err := json.Unmarshal(accordsJSON, &card.Accords); err != nil {
		return enrichment.Card{}, false, fmt.Errorf("decode cached enrichment for %s: %w", string(requestJSON), err)
	}
	return card, true, nil
}

func (cache *EnrichmentCache) Put(ctx context.Context, request enrichment.Request, card enrichment.Card) error {
	if len(card.PNG) > maxEnrichmentImageSize {
		return fmt.Errorf("cache enrichment image: %w", enrichment.ErrImageTooLarge)
	}

	key, _, err := enrichmentCacheKey(request)
	if err != nil {
		return err
	}
	accordsJSON, err := json.Marshal(card.Accords)
	if err != nil {
		return fmt.Errorf("encode cached accords: %w", err)
	}

	tx, err := cache.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin enrichment cache write: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
        INSERT INTO fragrantica_enrichment_cache(cache_key, source_url, title, accords_json, image_png, created_at)
        VALUES(?, ?, ?, ?, ?, ?)
        ON CONFLICT(cache_key) DO UPDATE SET
            source_url = excluded.source_url,
            title = excluded.title,
            accords_json = excluded.accords_json,
            image_png = excluded.image_png,
            created_at = excluded.created_at
    `, key, card.SourceURL, card.Title, accordsJSON, card.PNG, cache.now().UnixNano()); err != nil {
		return fmt.Errorf("write enrichment cache: %w", err)
	}

	var total int64
	if err := tx.QueryRowContext(ctx, `
        SELECT COALESCE(SUM(length(source_url) + length(title) + length(accords_json) + length(image_png)), 0)
        FROM fragrantica_enrichment_cache
    `).Scan(&total); err != nil {
		return fmt.Errorf("measure enrichment cache: %w", err)
	}
	if total > maxEnrichmentCacheSize {
		rows, err := tx.QueryContext(ctx, `
			SELECT cache_key, length(source_url) + length(title) + length(accords_json) + length(image_png)
            FROM fragrantica_enrichment_cache
            ORDER BY created_at ASC, cache_key ASC
        `)
		if err != nil {
			return fmt.Errorf("list enrichment cache for pruning: %w", err)
		}

		keys := make([]string, 0)
		for rows.Next() && total > targetEnrichmentSize {
			var oldestKey string
			var imageSize int64
			if err := rows.Scan(&oldestKey, &imageSize); err != nil {
				_ = rows.Close()
				return fmt.Errorf("read enrichment cache for pruning: %w", err)
			}
			keys = append(keys, oldestKey)
			total -= imageSize
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("list enrichment cache for pruning: %w", err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close enrichment cache pruning rows: %w", err)
		}
		for _, oldestKey := range keys {
			if _, err := tx.ExecContext(ctx, `DELETE FROM fragrantica_enrichment_cache WHERE cache_key = ?`, oldestKey); err != nil {
				return fmt.Errorf("prune enrichment cache: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit enrichment cache write: %w", err)
	}
	return nil
}

func enrichmentCacheKey(request enrichment.Request) (string, []byte, error) {
	canonical := struct {
		Brand         string               `json:"brand"`
		Name          string               `json:"name"`
		Edition       string               `json:"edition"`
		Concentration domain.Concentration `json:"concentration"`
	}{
		Brand:         domain.NormalizeText(request.Brand),
		Name:          domain.NormalizeText(request.Name),
		Edition:       domain.NormalizeText(request.Edition),
		Concentration: request.Concentration,
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", nil, fmt.Errorf("encode enrichment request: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), encoded, nil
}
