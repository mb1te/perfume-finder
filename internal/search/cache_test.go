package search

import (
	"context"
	"parfumes_finder/internal/domain"
	"parfumes_finder/internal/shop"
	"sync/atomic"
	"testing"
	"time"
)

func TestCoordinatorReturnsFreshCacheWithoutCallingAdapters(t *testing.T) {
	var calls atomic.Int32
	adapter := fakeAdapter{id: "shop", search: func(context.Context, domain.SearchQuery) ([]domain.Offer, error) { calls.Add(1); return nil, nil }}
	cached := []domain.Offer{{ShopID: "cached", PriceKopecks: 100}}
	cache := &fakeCache{offers: cached, hit: true}
	result := NewCoordinator([]shop.Adapter{adapter}, time.Second, cache).Search(context.Background(), domain.SearchQuery{Raw: "Dior Sauvage"})
	if !result.Cached || len(result.Offers) != 1 || calls.Load() != 0 {
		t.Fatalf("result=%+v calls=%d", result, calls.Load())
	}
}

func TestCoordinatorCachesSuccessfulOffersAfterMiss(t *testing.T) {
	offers := []domain.Offer{{ShopID: "live", PriceKopecks: 100}}
	adapter := fakeAdapter{id: "shop", search: func(context.Context, domain.SearchQuery) ([]domain.Offer, error) { return offers, nil }}
	cache := &fakeCache{}
	result := NewCoordinator([]shop.Adapter{adapter}, time.Second, cache).Search(context.Background(), domain.SearchQuery{Raw: "Dior Sauvage"})
	if result.Cached || len(result.Offers) != 1 || cache.puts.Load() != 1 {
		t.Fatalf("result=%+v puts=%d", result, cache.puts.Load())
	}
}

type fakeCache struct {
	offers []domain.Offer
	hit    bool
	puts   atomic.Int32
}

func (c *fakeCache) Get(context.Context, domain.SearchQuery) ([]domain.Offer, bool, error) {
	return c.offers, c.hit, nil
}
func (c *fakeCache) Put(_ context.Context, _ domain.SearchQuery, offers []domain.Offer) error {
	c.offers = offers
	c.puts.Add(1)
	return nil
}
