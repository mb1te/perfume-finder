package registryhealth

import (
	"context"
	"golang.org/x/sync/errgroup"
	"parfumes_finder/internal/registry"
	"parfumes_finder/internal/shop"
	"parfumes_finder/internal/storage"
	"time"
)

type ShopSource interface {
	ListShops(context.Context) ([]registry.Shop, error)
}
type Probe interface {
	Check(context.Context, registry.Shop) shop.HealthResult
}
type HealthRepository interface {
	Record(context.Context, string, storage.ProbeKind, shop.HealthResult) (storage.HealthRecord, error)
}
type Scheduler struct {
	source ShopSource
	probe  Probe
	repo   HealthRepository
}

func NewScheduler(source ShopSource, probe Probe, repo HealthRepository) *Scheduler {
	return &Scheduler{source, probe, repo}
}
func (s *Scheduler) RunOnce(ctx context.Context) error {
	shops, err := s.source.ListShops(ctx)
	if err != nil {
		return err
	}
	group, gctx := errgroup.WithContext(ctx)
	group.SetLimit(10)
	for _, target := range shops {
		target := target
		group.Go(func() error {
			result := s.probe.Check(gctx, target)
			_, err := s.repo.Record(gctx, target.NetworkDomain, storage.ProbeHomepage, result)
			return err
		})
	}
	return group.Wait()
}
func (s *Scheduler) Run(ctx context.Context, interval time.Duration) {
	_ = s.RunOnce(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.RunOnce(ctx)
		}
	}
}
