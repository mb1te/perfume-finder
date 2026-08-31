package search

import (
	"context"
	"errors"
	"time"

	"golang.org/x/sync/errgroup"

	"parfumes_finder/internal/domain"
	"parfumes_finder/internal/shop"
)

const (
	defaultGlobalConcurrency   = 20
	defaultPerStoreConcurrency = 2
)

type Cache interface {
	Get(context.Context, domain.SearchQuery) ([]domain.Offer, bool, error)
	Put(context.Context, domain.SearchQuery, []domain.Offer) error
}

type Failure struct {
	ShopID string
	Kind   shop.ErrorKind
	Err    error
}

type Result struct {
	Offers   []domain.Offer
	Failures []Failure
	Cached   bool
}

type Coordinator struct {
	adapters []shop.Adapter
	timeout  time.Duration
	cache    Cache
	global   chan struct{}
	perStore map[string]chan struct{}
}

func NewCoordinator(adapters []shop.Adapter, timeout time.Duration, cache Cache) *Coordinator {
	return newCoordinator(adapters, timeout, cache, defaultGlobalConcurrency, defaultPerStoreConcurrency)
}

func newCoordinator(adapters []shop.Adapter, timeout time.Duration, cache Cache, globalLimit, perStoreLimit int) *Coordinator {
	coordinator := &Coordinator{
		adapters: append([]shop.Adapter(nil), adapters...),
		timeout:  timeout,
		cache:    cache,
		global:   make(chan struct{}, globalLimit),
		perStore: make(map[string]chan struct{}, len(adapters)),
	}
	for _, adapter := range adapters {
		if _, exists := coordinator.perStore[adapter.ID()]; !exists {
			coordinator.perStore[adapter.ID()] = make(chan struct{}, perStoreLimit)
		}
	}
	return coordinator
}

func (coordinator *Coordinator) Search(ctx context.Context, query domain.SearchQuery) Result {
	if coordinator.cache != nil {
		if offers, hit, err := coordinator.cache.Get(ctx, query); err == nil && hit {
			return Result{Offers: offers, Cached: true}
		}
	}
	type adapterResult struct {
		offers  []domain.Offer
		failure *Failure
	}

	group, groupCtx := errgroup.WithContext(ctx)
	results := make(chan adapterResult, len(coordinator.adapters))
	for _, adapter := range coordinator.adapters {
		adapter := adapter
		group.Go(func() error {
			adapterCtx, cancel := context.WithTimeout(groupCtx, coordinator.timeout)
			defer cancel()

			storeSemaphore := coordinator.perStore[adapter.ID()]
			if err := acquire(adapterCtx, storeSemaphore); err != nil {
				results <- adapterResult{failure: newFailure(adapter.ID(), err)}
				return nil
			}
			defer release(storeSemaphore)

			if err := acquire(adapterCtx, coordinator.global); err != nil {
				results <- adapterResult{failure: newFailure(adapter.ID(), err)}
				return nil
			}
			defer release(coordinator.global)

			offers, err := adapter.Search(adapterCtx, query)
			if err != nil {
				results <- adapterResult{failure: newFailure(adapter.ID(), err)}
				return nil
			}
			results <- adapterResult{offers: offers}
			return nil
		})
	}

	go func() {
		_ = group.Wait()
		close(results)
	}()

	var result Result
	for adapterResult := range results {
		result.Offers = append(result.Offers, adapterResult.offers...)
		if adapterResult.failure != nil {
			result.Failures = append(result.Failures, *adapterResult.failure)
		}
	}
	if coordinator.cache != nil && len(result.Offers) > 0 {
		_ = coordinator.cache.Put(ctx, query, result.Offers)
	}
	return result
}

func acquire(ctx context.Context, semaphore chan struct{}) error {
	select {
	case semaphore <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func release(semaphore chan struct{}) {
	<-semaphore
}

func newFailure(shopID string, err error) *Failure {
	kind := shop.ErrorKindOf(err)
	if errors.Is(err, context.DeadlineExceeded) {
		kind = shop.ErrorTimeout
	}
	return &Failure{ShopID: shopID, Kind: kind, Err: err}
}
