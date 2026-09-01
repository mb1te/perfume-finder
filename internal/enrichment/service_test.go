package enrichment

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestCachedServiceUsesFreshCardWithoutRemoteCall(t *testing.T) {
	cache := &fakeCache{card: wantedCard, ok: true}
	remote := &fakeService{}
	got, ok, err := NewCachedService(remote, cache).Enrich(context.Background(), request)
	if err != nil || !ok || remote.calls != 0 || !reflect.DeepEqual(got, wantedCard) {
		t.Fatalf("got %+v, ok=%v, err=%v, calls=%d", got, ok, err, remote.calls)
	}
}

func TestCachedServiceFetchesMissAndWritesValidRemoteCard(t *testing.T) {
	cache := &fakeCache{}
	remote := &fakeService{card: wantedCard, ok: true}
	got, ok, err := NewCachedService(remote, cache).Enrich(context.Background(), request)
	if err != nil || !ok || remote.calls != 1 || cache.puts != 1 || !reflect.DeepEqual(got, wantedCard) {
		t.Fatalf("got %+v, ok=%v, err=%v, remote calls=%d, cache puts=%d", got, ok, err, remote.calls, cache.puts)
	}
}

func TestCachedServiceReturnsRemoteCardWhenCacheWriteFails(t *testing.T) {
	cache := &fakeCache{putErr: errors.New("disk full")}
	remote := &fakeService{card: wantedCard, ok: true}
	got, ok, err := NewCachedService(remote, cache).Enrich(context.Background(), request)
	if err != nil || !ok || cache.puts != 1 || !reflect.DeepEqual(got, wantedCard) {
		t.Fatalf("got %+v, ok=%v, err=%v, cache puts=%d", got, ok, err, cache.puts)
	}
}

func TestCachedServiceFallsBackToRemoteWhenCacheReadFails(t *testing.T) {
	cache := &fakeCache{getErr: errors.New("database busy")}
	remote := &fakeService{card: wantedCard, ok: true}
	got, ok, err := NewCachedService(remote, cache).Enrich(context.Background(), request)
	if err != nil || !ok || remote.calls != 1 || !reflect.DeepEqual(got, wantedCard) {
		t.Fatalf("got %+v, ok=%v, err=%v, calls=%d", got, ok, err, remote.calls)
	}
}

type fakeService struct {
	card  Card
	ok    bool
	err   error
	calls int
}

func (service *fakeService) Enrich(context.Context, Request) (Card, bool, error) {
	service.calls++
	return service.card, service.ok, service.err
}

type fakeCache struct {
	card   Card
	ok     bool
	getErr error
	putErr error
	puts   int
}

func (cache *fakeCache) Get(context.Context, Request) (Card, bool, error) {
	return cache.card, cache.ok, cache.getErr
}

func (cache *fakeCache) Put(context.Context, Request, Card) error {
	cache.puts++
	return cache.putErr
}
