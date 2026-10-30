package catalog

import (
	"context"
	"testing"

	"github.com/plutoframework/pluto-core/cache"
	"github.com/plutoframework/pluto-core/cache/memory"
)

type repository struct{ calls int }

func (repo *repository) Read(
	_ context.Context,
	tenant, id string,
) (Product, error) {
	repo.calls++

	return Product{ID: id, Name: tenant}, nil
}

func TestCachePartitionsAndInvalidates(t *testing.T) {
	store, err := memory.New(memory.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}

	repo := &repository{}

	catalog, err := New(store, repo)
	if err != nil {
		t.Fatal(err)
	}

	scope := cache.Scope{
		App: "orbit", Environment: "test", Namespace: "products",
		Version: "1", TenantID: "north", SubjectID: "reader",
	}
	ctx := context.Background()

	for range 2 {
		if _, err := catalog.Get(ctx, scope, "42"); err != nil {
			t.Fatal(err)
		}
	}

	if repo.calls != 1 {
		t.Fatal("cache hit read the repository")
	}

	key, err := cache.Key(scope, "42")
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}

	if _, err := catalog.Get(ctx, scope, "42"); err != nil {
		t.Fatal(err)
	}

	scope.TenantID = "harbor"

	product, err := catalog.Get(ctx, scope, "42")
	if err != nil {
		t.Fatal(err)
	}

	if repo.calls != 3 || product.Name != "harbor" {
		t.Fatal("lost invalidation or tenant partition")
	}
}
