package search

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"parfumes_finder/internal/domain"
	"parfumes_finder/internal/shop"
)

func TestCoordinatorReturnsPartialResults(t *testing.T) {
	t.Parallel()

	good := fakeAdapter{id: "good", search: func(context.Context, domain.SearchQuery) ([]domain.Offer, error) {
		return []domain.Offer{{ShopID: "good", PriceKopecks: 10000}}, nil
	}}
	bad := fakeAdapter{id: "bad", search: func(context.Context, domain.SearchQuery) ([]domain.Offer, error) {
		return nil, shop.NewError(shop.ErrorParse, errors.New("markup changed"))
	}}
	slow := fakeAdapter{id: "slow", search: func(ctx context.Context, _ domain.SearchQuery) ([]domain.Offer, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	coordinator := NewCoordinator([]shop.Adapter{good, bad, slow}, 20*time.Millisecond, nil)

	result := coordinator.Search(context.Background(), domain.SearchQuery{Raw: "Dior Sauvage"})

	if len(result.Offers) != 1 || result.Offers[0].ShopID != "good" {
		t.Fatalf("offers = %+v, want one good offer", result.Offers)
	}
	if len(result.Failures) != 2 {
		t.Fatalf("got %d failures, want 2", len(result.Failures))
	}
	assertFailure(t, result.Failures, "bad", shop.ErrorParse)
	assertFailure(t, result.Failures, "slow", shop.ErrorTimeout)
}

func TestCoordinatorLimitsGlobalConcurrency(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	var active atomic.Int32
	var maximum atomic.Int32
	adapters := make([]shop.Adapter, 25)
	for index := range adapters {
		adapters[index] = blockingAdapter(fmt.Sprintf("shop-%02d", index), release, &active, &maximum)
	}
	coordinator := newCoordinator(adapters, time.Second, nil, 20, 2)
	done := make(chan struct{})
	go func() {
		defer close(done)
		coordinator.Search(context.Background(), domain.SearchQuery{Raw: "query"})
	}()

	waitForMaximum(t, &maximum, 20)
	close(release)
	<-done

	if got := maximum.Load(); got > 20 {
		t.Fatalf("maximum global concurrency = %d, want <= 20", got)
	}
}

func TestCoordinatorLimitsConcurrencyPerStore(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	var active atomic.Int32
	var maximum atomic.Int32
	adapter := blockingAdapter("same-shop", release, &active, &maximum)
	coordinator := newCoordinator([]shop.Adapter{adapter}, time.Second, nil, 20, 2)
	done := make(chan struct{}, 5)
	for range 5 {
		go func() {
			coordinator.Search(context.Background(), domain.SearchQuery{Raw: "query"})
			done <- struct{}{}
		}()
	}

	waitForMaximum(t, &maximum, 2)
	close(release)
	for range 5 {
		<-done
	}

	if got := maximum.Load(); got > 2 {
		t.Fatalf("maximum per-store concurrency = %d, want <= 2", got)
	}
}

type fakeAdapter struct {
	id     string
	search func(context.Context, domain.SearchQuery) ([]domain.Offer, error)
}

func (adapter fakeAdapter) ID() string {
	return adapter.id
}

func (adapter fakeAdapter) Search(ctx context.Context, query domain.SearchQuery) ([]domain.Offer, error) {
	return adapter.search(ctx, query)
}

func (fakeAdapter) Health(context.Context) shop.HealthResult {
	return shop.HealthResult{Status: shop.HealthHealthy}
}

func blockingAdapter(id string, release <-chan struct{}, active, maximum *atomic.Int32) shop.Adapter {
	return fakeAdapter{id: id, search: func(ctx context.Context, _ domain.SearchQuery) ([]domain.Offer, error) {
		current := active.Add(1)
		updateMaximum(maximum, current)
		defer active.Add(-1)
		select {
		case <-release:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}
}

func updateMaximum(maximum *atomic.Int32, value int32) {
	for {
		current := maximum.Load()
		if value <= current || maximum.CompareAndSwap(current, value) {
			return
		}
	}
}

func waitForMaximum(t *testing.T, maximum *atomic.Int32, want int32) {
	t.Helper()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if maximum.Load() >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("maximum concurrency reached %d, want %d", maximum.Load(), want)
}

func assertFailure(t *testing.T, failures []Failure, shopID string, kind shop.ErrorKind) {
	t.Helper()

	for _, failure := range failures {
		if failure.ShopID == shopID {
			if failure.Kind != kind {
				t.Fatalf("failure %s kind = %q, want %q", shopID, failure.Kind, kind)
			}
			return
		}
	}
	t.Fatalf("failure for %s not found", shopID)
}
