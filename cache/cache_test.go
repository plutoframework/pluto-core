package cache_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/plutoframework/pluto-core/cache"
	"github.com/plutoframework/pluto-core/cache/memory"
)

func TestScopeIsolationAndFraming(t *testing.T) {
	scope := cache.Scope{
		App:         "app",
		Environment: "test",
		Namespace:   "profile",
		Version:     "1",
		TenantID:    "northstar",
	}

	first, err := cache.Key(scope, "a:b", "c")
	if err != nil {
		t.Fatal(err)
	}

	second, err := cache.Key(scope, "a", "b:c")
	if err != nil || first == second {
		t.Fatal("ambiguous components", err)
	}

	scope.TenantID = "harbor"

	other, err := cache.Key(scope, "a:b", "c")
	if err != nil || other == first {
		t.Fatal("tenant collision", err)
	}

	scope.SubjectID = "actor"

	subject, err := cache.Key(scope, "a:b", "c")
	if err != nil || subject == other {
		t.Fatal("subject collision", err)
	}

	scope.Environment = "prod"

	production, _ := cache.Key(scope, "a:b", "c")
	if production == subject {
		t.Fatal("environment collision")
	}
}

func FuzzKey(f *testing.F) {
	f.Add("northstar", "a:b", "c")
	f.Fuzz(func(t *testing.T, tenant, first, second string) {
		scope := cache.Scope{
			App:         "app",
			Environment: "test",
			Namespace:   "data",
			Version:     "1",
			TenantID:    tenant,
		}

		key, err := cache.Key(scope, first, second)
		if err == nil && cache.DefaultLimits().Key(key) != nil {
			t.Fatal("invalid generated key")
		}
	})
}

type unavailable struct{}

func (unavailable) Get(context.Context, string) ([]byte, bool, error) {
	return nil, false, cache.ErrUnavailable
}

func (unavailable) Set(context.Context, string, []byte, time.Duration) error {
	return cache.ErrUnavailable
}

func (unavailable) Delete(
	context.Context,
	string,
) error {
	return cache.ErrUnavailable
}

func TestLoaderPoliciesCancellationAndPanic(t *testing.T) {
	for _, policy := range []cache.FailurePolicy{cache.Strict, cache.Bypass} {
		options := cache.DefaultLoaderOptions()
		options.FailurePolicy = policy

		loader, err := cache.NewLoader[[]string](unavailable{}, options)
		if err != nil {
			t.Fatal(err)
		}

		calls := 0
		load := func(context.Context) ([]string, error) { calls++; return []string{"database"}, nil }

		value, err := loader.Remember(
			context.Background(),
			"key",
			time.Minute,
			load,
		)
		if policy == cache.Strict {
			if !errors.Is(err, cache.ErrUnavailable) || calls != 0 {
				t.Fatal("strict policy bypassed", err, calls)
			}
		} else if err != nil || calls != 1 || len(value) != 1 {
			t.Fatal("fallback failed", value, err, calls)
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		before := calls
		if _, err := loader.Remember(
			ctx,
			"key",
			time.Minute,
			load,
		); !errors.Is(err, context.Canceled) ||
			calls != before {
			t.Fatal("canceled request loaded", err)
		}
	}

	loader, _ := cache.NewLoader[string](
		cache.Disabled{},
		cache.DefaultLoaderOptions(),
	)
	if _, err := loader.Remember(
		context.Background(),
		"key",
		time.Minute,
		func(context.Context) (string, error) { panic("private") },
	); !errors.Is(
		err,
		cache.ErrLoader,
	) {
		t.Fatal(err)
	}

	if value, err := loader.Remember(
		context.Background(),
		"key",
		time.Minute,
		func(context.Context) (string, error) { return "recovered", nil },
	); err != nil ||
		value != "recovered" {
		t.Fatal("flight leaked", value, err)
	}
}

func TestLoaderCoalescesCopiesAndBounds(t *testing.T) {
	store, _ := memory.New(memory.DefaultConfig())
	options := cache.DefaultLoaderOptions()
	options.MaxInFlight = 1
	loader, _ := cache.NewLoader[[]string](store, options)
	started, release := make(chan struct{}), make(chan struct{})

	var calls atomic.Int32

	load := func(context.Context) ([]string, error) {
		calls.Add(1)
		close(started)
		<-release

		return []string{"safe"}, nil
	}

	var wg sync.WaitGroup

	results := make(chan []string, 20)

	wg.Go(func() {
		value, err := loader.Remember(
			context.Background(),
			"one",
			time.Minute,
			load,
		)
		if err != nil {
			t.Error(err)
		}

		results <- value
	})
	<-started

	if _, err := loader.Remember(
		context.Background(),
		"two",
		time.Minute,
		load,
	); !errors.Is(
		err,
		cache.ErrBusy,
	) {
		t.Fatal("unbounded distinct flights", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := loader.Remember(
		ctx,
		"one",
		time.Minute,
		load,
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatal(err)
	}

	for range 19 {
		wg.Go(func() {
			value, err := loader.Remember(
				context.Background(),
				"one",
				time.Minute,
				load,
			)
			if err != nil {
				t.Error(err)
			}

			results <- value
		})
	}

	close(release)
	wg.Wait()
	close(results)

	if calls.Load() != 1 {
		t.Fatal("stampede", calls.Load())
	}

	for value := range results {
		if len(value) != 1 || value[0] != "safe" {
			t.Fatal(value)
		}

		value[0] = "mutated"
	}

	value, found, err := cache.GetJSON[[]string](
		context.Background(),
		store,
		"one",
	)
	if err != nil || !found || value[0] != "safe" {
		t.Fatal("shared mutable value", value, err)
	}
}
