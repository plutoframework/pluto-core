package telemetry

import (
	"context"
	"time"

	"github.com/plutoframework/pluto-core/cache"
)

// Cache instruments storage calls without recording keys, values or scopes.
// It preserves misses and failures exactly; authorization must precede caching.
func (r *Runtime) Cache(store cache.Store) cache.Store {
	return &cacheStore{runtime: r, store: store}
}

type cacheStore struct {
	runtime *Runtime
	store   cache.Store
}

func (s *cacheStore) Get(
	ctx context.Context,
	key string,
) ([]byte, bool, error) {
	ctx, done := s.runtime.Operation(ctx, "cache", "get")
	data, found, err := s.store.Get(ctx, key)
	done(err)

	return data, found, err
}

func (s *cacheStore) Set(
	ctx context.Context,
	key string,
	data []byte,
	ttl time.Duration,
) error {
	ctx, done := s.runtime.Operation(ctx, "cache", "set")
	err := s.store.Set(ctx, key, data, ttl)
	done(err)

	return err
}

func (s *cacheStore) Delete(ctx context.Context, key string) error {
	ctx, done := s.runtime.Operation(ctx, "cache", "delete")
	err := s.store.Delete(ctx, key)
	done(err)

	return err
}
