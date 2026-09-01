package storage

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"parfumes_finder/internal/domain"
	"parfumes_finder/internal/enrichment"
)

func TestEnrichmentCacheRoundTripAndExpiresAtTTL(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	cache := NewEnrichmentCache(openTestDB(t), 7*24*time.Hour, func() time.Time { return now })
	request := enrichment.Request{Brand: "Dior", Name: "Sauvage", Concentration: domain.ConcentrationEDP}
	want := enrichment.Card{
		SourceURL: "https://www.fragrantica.ru/perfume/Dior/Sauvage-Eau-de-Parfum-48100.html",
		Title:     "Sauvage Eau de Parfum Dior",
		Accords:   []enrichment.Accord{{Name: "свежий пряный", Color: "#d8c69a", Width: 100}},
		PNG:       tinyPNG(t),
	}

	if err := cache.Put(context.Background(), request, want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := cache.Get(context.Background(), enrichment.Request{
		Brand:         "  dior ",
		Name:          "SAUVAGE",
		Concentration: domain.ConcentrationEDP,
	})
	if err != nil || !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("Get() = %+v, %v, %v; want %+v, true, nil", got, ok, err, want)
	}

	now = now.Add(7 * 24 * time.Hour)
	if got, ok, err := cache.Get(context.Background(), request); err != nil || ok || !reflect.DeepEqual(got, enrichment.Card{}) {
		t.Fatalf("expired Get() = %+v, %v, %v; want zero card, false, nil", got, ok, err)
	}
}

func TestEnrichmentCacheRejectsPNGOverFiveMiB(t *testing.T) {
	cache := NewEnrichmentCache(openTestDB(t), 7*24*time.Hour, time.Now)
	request := enrichment.Request{Brand: "Dior", Name: "Sauvage", Concentration: domain.ConcentrationEDP}
	card := enrichment.Card{PNG: make([]byte, 5<<20+1)}

	if err := cache.Put(context.Background(), request, card); !errors.Is(err, enrichment.ErrImageTooLarge) {
		t.Fatalf("Put() error = %v, want ErrImageTooLarge", err)
	}
}

func TestEnrichmentCachePrunesOldestPayloadsAfterCrossingSizeLimit(t *testing.T) {
	const imageSize = 4 << 20
	const rows = 64 // Images are exactly 256 MiB; metadata must make the full payload cross the limit.

	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	db := openTestDB(t)
	cache := NewEnrichmentCache(db, 7*24*time.Hour, func() time.Time { return now })
	image := make([]byte, imageSize)
	for i := 0; i < rows; i++ {
		request := enrichment.Request{Brand: "brand", Name: fmt.Sprintf("perfume-%d", i), Concentration: domain.ConcentrationEDP}
		card := enrichment.Card{
			SourceURL: "https://www.fragrantica.ru/perfume/Brand/Perfume-12345.html",
			Title:     "Perfume",
			Accords:   []enrichment.Accord{{Name: "woody", Color: "#aabbcc", Width: 80}},
			PNG:       image,
		}
		if err := cache.Put(context.Background(), request, card); err != nil {
			t.Fatalf("Put() row %d: %v", i, err)
		}
		now = now.Add(time.Nanosecond)
	}

	var total int64
	if err := db.QueryRow(`
        SELECT COALESCE(SUM(length(source_url) + length(title) + length(accords_json) + length(image_png)), 0)
        FROM fragrantica_enrichment_cache
    `).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total > 192<<20 {
		t.Fatalf("cached payload total = %d, want at most %d", total, 192<<20)
	}

	for i := 0; i < rows-47; i++ {
		_, ok, err := cache.Get(context.Background(), enrichment.Request{Brand: "brand", Name: fmt.Sprintf("perfume-%d", i), Concentration: domain.ConcentrationEDP})
		if err != nil || ok {
			t.Fatalf("old row %d remains: ok=%v err=%v", i, ok, err)
		}
	}
	_, ok, err := cache.Get(context.Background(), enrichment.Request{Brand: "brand", Name: fmt.Sprintf("perfume-%d", rows-1), Concentration: domain.ConcentrationEDP})
	if err != nil || !ok {
		t.Fatalf("newest row missing: ok=%v err=%v", ok, err)
	}
}

func TestOpenAppliesEnrichmentMigrationWithoutReplacingExistingData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`
        CREATE TABLE search_cache (
            cache_key TEXT PRIMARY KEY,
            query_json BLOB NOT NULL,
            offers_json BLOB NOT NULL,
            created_at INTEGER NOT NULL
        );
        INSERT INTO search_cache(cache_key, query_json, offers_json, created_at)
        VALUES ('existing', 'query', 'offers', 1);
    `); err != nil {
		_ = legacy.Close()
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var offers string
	if err := db.QueryRow(`SELECT offers_json FROM search_cache WHERE cache_key = 'existing'`).Scan(&offers); err != nil || offers != "offers" {
		t.Fatalf("existing search cache = %q, %v; want offers, nil", offers, err)
	}
	var tableName string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'fragrantica_enrichment_cache'`).Scan(&tableName); err != nil || tableName != "fragrantica_enrichment_cache" {
		t.Fatalf("enrichment table = %q, %v", tableName, err)
	}
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()

	data, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	return data
}
