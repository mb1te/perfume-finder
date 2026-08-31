package health

import (
	"context"
	"parfumes_finder/internal/domain"
	"parfumes_finder/internal/shop"
	"parfumes_finder/internal/storage"
	"sync"
	"testing"
	"time"
)

func TestSchedulerRunsImmediatelyAndRepeats(t *testing.T) {
	repo := &fakeRepository{}
	scheduler := NewScheduler([]shop.Adapter{healthAdapter{id: "allure"}, healthAdapter{id: "orental"}}, repo, 10*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { scheduler.Run(ctx); close(done) }()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if repo.count() >= 4 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	if repo.count() < 4 {
		t.Fatalf("records=%d, want at least 4", repo.count())
	}
	for _, kind := range repo.kinds {
		if kind != storage.ProbeSearch {
			t.Fatalf("probe kind=%q", kind)
		}
	}
}

type healthAdapter struct{ id string }

func (a healthAdapter) ID() string { return a.id }
func (healthAdapter) Search(context.Context, domain.SearchQuery) ([]domain.Offer, error) {
	return nil, nil
}
func (healthAdapter) Health(context.Context) shop.HealthResult {
	return shop.HealthResult{Status: shop.HealthHealthy}
}

type fakeRepository struct {
	mu    sync.Mutex
	kinds []storage.ProbeKind
}

func (r *fakeRepository) Record(_ context.Context, _ string, kind storage.ProbeKind, result shop.HealthResult) (storage.HealthRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.kinds = append(r.kinds, kind)
	return storage.HealthRecord{Status: result.Status}, nil
}
func (r *fakeRepository) count() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.kinds) }
