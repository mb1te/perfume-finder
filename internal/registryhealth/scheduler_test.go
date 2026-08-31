package registryhealth

import (
	"context"
	"parfumes_finder/internal/registry"
	"parfumes_finder/internal/shop"
	"parfumes_finder/internal/storage"
	"sync"
	"testing"
)

func TestSchedulerSweepsAllRegistryShopsAsHomepageProbes(t *testing.T) {
	source := fakeSource{[]registry.Shop{{NetworkDomain: "a.ru"}, {NetworkDomain: "b.ru"}, {NetworkDomain: "c.ru"}}}
	repo := &sweepRepo{}
	scheduler := NewScheduler(source, fakeProbe{}, repo)
	if err := scheduler.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if repo.count() != 3 {
		t.Fatalf("records=%d", repo.count())
	}
}

type fakeSource struct{ shops []registry.Shop }

func (s fakeSource) ListShops(context.Context) ([]registry.Shop, error) { return s.shops, nil }

type fakeProbe struct{}

func (fakeProbe) Check(context.Context, registry.Shop) shop.HealthResult {
	return shop.HealthResult{Status: shop.HealthHealthy}
}

type sweepRepo struct {
	mu sync.Mutex
	n  int
}

func (r *sweepRepo) Record(_ context.Context, _ string, kind storage.ProbeKind, _ shop.HealthResult) (storage.HealthRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if kind != storage.ProbeHomepage {
		panic("wrong probe")
	}
	r.n++
	return storage.HealthRecord{}, nil
}
func (r *sweepRepo) count() int { r.mu.Lock(); defer r.mu.Unlock(); return r.n }
