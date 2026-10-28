package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/plutoframework/pluto-core/cache"
)

func TestExpirationCopiesLRUAndBounds(t *testing.T) {
	now := time.Unix(1000, 0)
	config := DefaultConfig()
	config.Now = func() time.Time { return now }
	config.MaxEntries = 2
	config.MaxBytes = 10

	store, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	value := []byte("one")
	if err := store.Set(ctx, "a", value, time.Second); err != nil {
		t.Fatal(err)
	}

	value[0] = 'X'

	read, found, err := store.Get(ctx, "a")
	if err != nil || !found || string(read) != "one" {
		t.Fatal(read, found, err)
	}

	read[0] = 'Y'

	if err := store.Set(ctx, "b", []byte("two"), time.Second); err != nil {
		t.Fatal(err)
	}

	_, _, _ = store.Get(ctx, "a")
	if err := store.Set(ctx, "c", []byte("new"), time.Second); err != nil {
		t.Fatal(err)
	}

	if _, found, _ := store.Get(ctx, "b"); found {
		t.Fatal("LRU did not evict")
	}

	read, _, _ = store.Get(ctx, "a")
	if string(read) != "one" {
		t.Fatal("caller mutated storage")
	}

	now = now.Add(time.Second)

	if _, found, _ := store.Get(ctx, "a"); found {
		t.Fatal("expired value returned")
	}

	if err := store.Set(ctx, "empty", nil, time.Second); err != nil {
		t.Fatal(err)
	}

	if _, found, err := store.Get(ctx, "empty"); err != nil || !found {
		t.Fatal("empty treated as miss")
	}

	if err := store.Set(
		ctx,
		"large",
		[]byte("123456"),
		time.Second,
	); !errors.Is(
		err,
		cache.ErrInvalid,
	) {
		t.Fatal("byte bound ignored", err)
	}

	for _, ttl := range []time.Duration{0, -time.Second, 8 * 24 * time.Hour} {
		if err := store.Set(
			ctx,
			"key",
			nil,
			ttl,
		); !errors.Is(
			err,
			cache.ErrInvalid,
		) {
			t.Fatal("TTL accepted", ttl)
		}
	}

	if err := store.Delete(ctx, "empty"); err != nil {
		t.Fatal(err)
	}

	if _, found, _ := store.Get(ctx, "empty"); found {
		t.Fatal("delete failed")
	}
}
