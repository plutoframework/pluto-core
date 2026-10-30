// Package catalog shows typed read-through caching over a repository.
package catalog

import (
	"context"
	"time"

	"github.com/plutoframework/pluto-core/cache"
)

type Product struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Repository interface {
	Read(context.Context, string, string) (Product, error)
}

type Catalog struct {
	loader *cache.Loader[Product]
	repo   Repository
}

func New(store cache.Store, repo Repository) (*Catalog, error) {
	if repo == nil {
		return nil, cache.ErrInvalid
	}

	loader, err := cache.NewLoader[Product](store, cache.DefaultLoaderOptions())
	if err != nil {
		return nil, err
	}

	return &Catalog{loader: loader, repo: repo}, nil
}

// Get follows authentication and current resource authorization in the operation.
// Scope comes from verified application context, never arbitrary client headers.
func (catalog *Catalog) Get(
	ctx context.Context,
	scope cache.Scope,
	id string,
) (Product, error) {
	key, err := cache.Key(scope, id)
	if err != nil {
		return Product{}, err
	}

	return catalog.loader.Remember(
		ctx,
		key,
		time.Minute,
		func(ctx context.Context) (Product, error) {
			return catalog.repo.Read(ctx, scope.TenantID, id)
		},
	)
}
