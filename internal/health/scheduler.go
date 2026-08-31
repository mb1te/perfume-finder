package health

import (
	"context"
	"parfumes_finder/internal/shop"
	"parfumes_finder/internal/storage"
	"time"
)

type Repository interface {
	Record(context.Context, string, storage.ProbeKind, shop.HealthResult) (storage.HealthRecord, error)
}
type Scheduler struct {
	adapters   []shop.Adapter
	repository Repository
	interval   time.Duration
}

func NewScheduler(adapters []shop.Adapter, repository Repository, interval time.Duration) *Scheduler {
	return &Scheduler{append([]shop.Adapter(nil), adapters...), repository, interval}
}
func (s *Scheduler) Run(ctx context.Context) {
	s.RunOnce(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.RunOnce(ctx)
		}
	}
}
func (s *Scheduler) RunOnce(ctx context.Context) {
	for _, adapter := range s.adapters {
		if ctx.Err() != nil {
			return
		}
		result := adapter.Health(ctx)
		_, _ = s.repository.Record(ctx, adapter.ID(), storage.ProbeSearch, result)
	}
}
