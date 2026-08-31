package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"parfumes_finder/internal/shop"
)

type ProbeKind string

const (
	ProbeHomepage ProbeKind = "homepage"
	ProbeSearch   ProbeKind = "search"
)

type HealthRecord struct {
	ShopID              string
	ProbeKind           ProbeKind
	Status              shop.HealthStatus
	CanonicalURL        string
	ErrorMessage        string
	ConsecutiveFailures int
	CheckedAt           time.Time
}

type Health struct {
	db  *sql.DB
	now func() time.Time
}

func NewHealth(db *sql.DB, now func() time.Time) *Health {
	return &Health{db: db, now: now}
}

func (health *Health) Record(ctx context.Context, shopID string, probeKind ProbeKind, result shop.HealthResult) (HealthRecord, error) {
	current, exists, err := health.Current(ctx, shopID, probeKind)
	if err != nil {
		return HealthRecord{}, err
	}

	record := HealthRecord{
		ShopID:       shopID,
		ProbeKind:    probeKind,
		Status:       result.Status,
		CanonicalURL: result.CanonicalURL,
		CheckedAt:    result.CheckedAt,
	}
	if record.CheckedAt.IsZero() {
		record.CheckedAt = health.now()
	}
	if result.Err != nil {
		record.ErrorMessage = result.Err.Error()
		if exists {
			record.ConsecutiveFailures = current.ConsecutiveFailures
		}
		record.ConsecutiveFailures++
		if record.Status == "" {
			record.Status = shop.HealthDegraded
		}
		if record.ConsecutiveFailures >= 3 {
			record.Status = shop.HealthDown
		}
	}

	_, err = health.db.ExecContext(ctx, `
        INSERT INTO shop_health(shop_id, probe_kind, status, canonical_url, error_message, consecutive_failures, checked_at)
        VALUES(?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(shop_id, probe_kind) DO UPDATE SET
            status = excluded.status,
            canonical_url = excluded.canonical_url,
            error_message = excluded.error_message,
            consecutive_failures = excluded.consecutive_failures,
            checked_at = excluded.checked_at
    `, record.ShopID, record.ProbeKind, record.Status, record.CanonicalURL, record.ErrorMessage, record.ConsecutiveFailures, record.CheckedAt.UnixNano())
	if err != nil {
		return HealthRecord{}, fmt.Errorf("record shop health: %w", err)
	}
	return record, nil
}

func (health *Health) Current(ctx context.Context, shopID string, probeKind ProbeKind) (HealthRecord, bool, error) {
	var record HealthRecord
	var checkedAt int64
	err := health.db.QueryRowContext(ctx, `
        SELECT shop_id, probe_kind, status, canonical_url, error_message, consecutive_failures, checked_at
        FROM shop_health
        WHERE shop_id = ? AND probe_kind = ?
    `, shopID, probeKind).Scan(
		&record.ShopID,
		&record.ProbeKind,
		&record.Status,
		&record.CanonicalURL,
		&record.ErrorMessage,
		&record.ConsecutiveFailures,
		&checkedAt,
	)
	if err == sql.ErrNoRows {
		return HealthRecord{}, false, nil
	}
	if err != nil {
		return HealthRecord{}, false, fmt.Errorf("read shop health: %w", err)
	}
	record.CheckedAt = time.Unix(0, checkedAt)
	return record, true, nil
}
