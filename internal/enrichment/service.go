package enrichment

import "context"

type Cache interface {
	Get(context.Context, Request) (Card, bool, error)
	Put(context.Context, Request, Card) error
}

type cachedService struct {
	remote Service
	cache  Cache
}

func NewCachedService(remote Service, cache Cache) Service {
	return &cachedService{remote: remote, cache: cache}
}

func (service *cachedService) Enrich(ctx context.Context, request Request) (Card, bool, error) {
	if card, ok, err := service.cache.Get(ctx, request); err == nil && ok {
		return card, true, nil
	}
	card, ok, err := service.remote.Enrich(ctx, request)
	if err != nil || !ok {
		return card, ok, err
	}
	_ = service.cache.Put(ctx, request, card)
	return card, true, nil
}
