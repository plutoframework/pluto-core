package valkey

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/plutoframework/pluto-core/cache"
)

func TestRejectsUnsafeConfigurationAndRedactsErrors(t *testing.T) {
	for _, address := range []string{
		"redis://remote.example:6379", "http://127.0.0.1:6379",
		"rediss://example.test:6379?skip_verify=true", "redis://localhost:6379#secret",
	} {
		config := DefaultConfig()

		config.URL = address
		if _, err := New(config); !errors.Is(err, cache.ErrInvalid) {
			t.Fatal("unsafe configuration accepted")
		}
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	address := listener.Addr().String()
	listener.Close()

	config := DefaultConfig()
	config.URL = "redis://:private-test-value@" + address
	config.Timeout = 20 * time.Millisecond

	store, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, _, err := store.Get(ctx, "key"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}

	if _, _, err := store.Get(
		context.Background(),
		"key",
	); !errors.Is(err, cache.ErrUnavailable) ||
		strings.Contains(err.Error(), "private-test-value") {
		t.Fatal("outage misclassified")
	}
}

func TestServers(t *testing.T) {
	urls := os.Getenv("PLUTO_CORE_CACHE_TEST_URLS")
	if urls == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("CI requires real cache servers")
		}

		t.Skip("set PLUTO_CORE_CACHE_TEST_URLS for real Valkey/Redis tests")
	}

	for index, address := range strings.Split(urls, ",") {
		t.Run(fmt.Sprintf("server-%d", index), func(t *testing.T) {
			config := DefaultConfig()
			config.URL = address
			config.Timeout = time.Second
			config.Limits.MaxValueBytes = 128

			store, err := Open(config)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()

			ctx := context.Background()

			key, err := cache.Key(
				cache.Scope{
					App:         "integration",
					Environment: "test",
					Namespace:   "contract",
					Version:     "1",
				},
				fmt.Sprint(time.Now().UnixNano()),
			)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Delete(ctx, key)

			if _, found, err := store.Get(ctx, key); err != nil || found {
				t.Fatal("miss contract", found, err)
			}

			if err := store.Set(ctx, key, nil, time.Minute); err != nil {
				t.Fatal(err)
			}

			if value, found, err := store.Get(
				ctx,
				key,
			); err != nil || !found ||
				len(value) != 0 {
				t.Fatal("empty/miss conflated", found, err)
			}

			if err := store.Set(
				ctx,
				key,
				[]byte("expires"),
				20*time.Millisecond,
			); err != nil {
				t.Fatal(err)
			}

			deadline := time.Now().Add(2 * time.Second)

			for {
				_, found, err := store.Get(ctx, key)
				if err != nil {
					t.Fatal(err)
				}

				if !found {
					break
				}

				if time.Now().After(deadline) {
					t.Fatal("TTL was not applied atomically")
				}

				time.Sleep(time.Millisecond)
			}

			for _, ttl := range []time.Duration{0, -1, 8 * 24 * time.Hour} {
				if err := store.Set(
					ctx,
					key,
					nil,
					ttl,
				); !errors.Is(
					err,
					cache.ErrInvalid,
				) {
					t.Fatal("invalid TTL accepted")
				}
			}

			var wg sync.WaitGroup
			for range 8 {
				wg.Go(func() {
					for range 5 {
						if err := store.Set(
							ctx,
							key,
							[]byte("safe"),
							time.Minute,
						); err != nil {
							t.Error(err)
						}

						if _, _, err := store.Get(ctx, key); err != nil {
							t.Error(err)
						}
					}
				})
			}

			wg.Wait()

			connection, err := store.connection(ctx)
			if err != nil {
				t.Fatal(err)
			}

			if err := connection.Do(ctx, connection.B().Set().Key(key).Value(strings.Repeat("x", 129)).Build()).
				Error(); err != nil {
				t.Fatal(err)
			}

			if _, _, err := store.Get(
				ctx,
				key,
			); !errors.Is(
				err,
				cache.ErrInvalid,
			) {
				t.Fatal("oversized server value accepted", err)
			}

			if err := store.Delete(ctx, key); err != nil {
				t.Fatal(err)
			}

			if _, found, err := store.Get(ctx, key); err != nil || found {
				t.Fatal("delete failed", err)
			}
		})
	}
}
