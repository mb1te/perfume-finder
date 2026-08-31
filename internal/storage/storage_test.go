package storage

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"parfumes_finder/internal/domain"
	"parfumes_finder/internal/shop"
)

func TestCacheExpiresAtTTL(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	db := openTestDB(t)
	cache := NewCache(db, 15*time.Minute, func() time.Time { return now })
	query := domain.SearchQuery{Raw: "Dior  Sauvage"}
	offers := []domain.Offer{{ShopID: "orental", PriceKopecks: 123400, RetrievedAt: now}}

	if err := cache.Put(context.Background(), query, offers); err != nil {
		t.Fatal(err)
	}
	got, ok, err := cache.Get(context.Background(), domain.SearchQuery{Raw: "dior sauvage"})
	if err != nil || !ok || !reflect.DeepEqual(got, offers) {
		t.Fatalf("fresh cache = %+v, %v, %v; want %+v, true, nil", got, ok, err, offers)
	}

	now = now.Add(15 * time.Minute)
	if got, ok, err = cache.Get(context.Background(), query); err != nil || ok || got != nil {
		t.Fatalf("expired cache = %+v, %v, %v; want nil, false, nil", got, ok, err)
	}
}

func TestSessionsRoundTripAndDelete(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	sessions := NewSessions(db)
	want := Session{
		ChatID: 42,
		ID:     "session-1",
		Stage:  "choose_volume",
		Query: domain.SearchQuery{
			Brand:         "Christian Dior",
			Name:          "Sauvage",
			Concentration: domain.ConcentrationEDT,
		},
		Options: []domain.SearchQuery{{Brand: "Tom Ford", Name: "Ombre Leather"}},
	}

	if err := sessions.Save(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := sessions.Load(context.Background(), want.ChatID)
	if err != nil || !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() = %+v, %v, %v; want %+v, true, nil", got, ok, err, want)
	}
	if err := sessions.Delete(context.Background(), want.ChatID); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := sessions.Load(context.Background(), want.ChatID); err != nil || ok {
		t.Fatalf("Load() after delete = ok %v, err %v", ok, err)
	}
}

func TestHealthMarksDownOnThirdFailureAndResetsOnSuccess(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	db := openTestDB(t)
	health := NewHealth(db, func() time.Time { return now })
	failure := shop.HealthResult{Status: shop.HealthDegraded, Err: errors.New("timeout")}

	for attempt := 1; attempt <= 3; attempt++ {
		record, err := health.Record(context.Background(), "randewoo", ProbeSearch, failure)
		if err != nil {
			t.Fatal(err)
		}
		wantStatus := shop.HealthDegraded
		if attempt == 3 {
			wantStatus = shop.HealthDown
		}
		if record.Status != wantStatus || record.ConsecutiveFailures != attempt {
			t.Fatalf("attempt %d = status %q, failures %d; want %q, %d", attempt, record.Status, record.ConsecutiveFailures, wantStatus, attempt)
		}
		now = now.Add(time.Minute)
	}

	record, err := health.Record(context.Background(), "randewoo", ProbeSearch, shop.HealthResult{Status: shop.HealthHealthy})
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != shop.HealthHealthy || record.ConsecutiveFailures != 0 {
		t.Fatalf("success = status %q, failures %d", record.Status, record.ConsecutiveFailures)
	}

	current, ok, err := health.Current(context.Background(), "randewoo", ProbeSearch)
	if err != nil || !ok || current.Status != shop.HealthHealthy {
		t.Fatalf("Current() = %+v, %v, %v", current, ok, err)
	}
}

func TestHealthKeepsHomepageAndSearchProbesSeparate(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	health := NewHealth(db, time.Now)
	if _, err := health.Record(context.Background(), "randewoo", ProbeHomepage, shop.HealthResult{Status: shop.HealthHealthy}); err != nil {
		t.Fatal(err)
	}
	if _, err := health.Record(context.Background(), "randewoo", ProbeSearch, shop.HealthResult{Status: shop.HealthDegraded, Err: errors.New("parse")}); err != nil {
		t.Fatal(err)
	}

	homepage, _, err := health.Current(context.Background(), "randewoo", ProbeHomepage)
	if err != nil {
		t.Fatal(err)
	}
	search, _, err := health.Current(context.Background(), "randewoo", ProbeSearch)
	if err != nil {
		t.Fatal(err)
	}
	if homepage.Status != shop.HealthHealthy || search.Status != shop.HealthDegraded {
		t.Fatalf("homepage=%q search=%q", homepage.Status, search.Status)
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
