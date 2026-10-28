package memory

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/plutoframework/pluto-core/idempotency"
)

func claim(
	t *testing.T,
	key, actor, tenant, fingerprint string,
) idempotency.Claim {
	t.Helper()

	c, err := idempotency.NewClaim(
		idempotency.Scope{
			Subject:   actor,
			Tenant:    tenant,
			Operation: "purchase.create",
		},
		key,
		strings.Repeat(fingerprint, 64),
		time.Now(),
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}

	return c
}

func TestClaims(t *testing.T) {
	ctx := context.Background()
	store, _ := New(100)

	c := claim(t, "retry", "alice", "org", "a")
	if response, err := store.Begin(ctx, c); err != nil || response != nil {
		t.Fatal(response, err)
	}

	if _, err := store.Begin(
		ctx,
		c,
	); !errors.Is(
		err,
		idempotency.ErrInProgress,
	) {
		t.Fatal(err)
	}

	changed := claim(t, "retry", "alice", "org", "b")
	if _, err := store.Begin(
		ctx,
		changed,
	); !errors.Is(
		err,
		idempotency.ErrConflict,
	) {
		t.Fatal(err)
	}

	for _, other := range []idempotency.Claim{claim(t, "retry", "bob", "org", "a"), claim(t, "retry", "alice", "other", "a"), claim(t, "other-key", "alice", "org", "a")} {
		if _, err := store.Begin(ctx, other); err != nil {
			t.Fatal("scope collision", err)
		}
	}

	response := idempotency.Response{
		Status:      201,
		ContentType: "application/json",
		Body:        []byte(`{"id":"one"}`),
	}
	impostor := c

	impostor.Owner = strings.Repeat("b", 64)
	if err := store.Complete(
		ctx,
		impostor,
		response,
	); !errors.Is(
		err,
		idempotency.ErrOwnership,
	) {
		t.Fatal(err)
	}

	if err := store.Complete(ctx, c, response); err != nil {
		t.Fatal(err)
	}

	replay, err := store.Begin(ctx, c)
	if err != nil || replay.Status != 201 ||
		string(replay.Body) != string(response.Body) {
		t.Fatal(replay, err)
	}

	replay.Body[0] = '!'

	replay, _ = store.Begin(ctx, c)
	if replay.Body[0] != '{' {
		t.Fatal("aliased recorded response")
	}

	if err := store.Complete(
		ctx,
		c,
		response,
	); !errors.Is(
		err,
		idempotency.ErrOwnership,
	) {
		t.Fatal(err)
	}
}

func TestConcurrencyTTLAndCancellation(t *testing.T) {
	ctx := context.Background()
	store, _ := New(2)
	c := claim(t, "race", "alice", "org", "a")

	var (
		winners atomic.Int32
		wg      sync.WaitGroup
	)
	for range 24 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			_, err := store.Begin(ctx, c)
			if err == nil {
				winners.Add(1)
			} else if !errors.Is(err, idempotency.ErrInProgress) {
				t.Error(err)
			}
		}()
	}

	wg.Wait()

	if winners.Load() != 1 {
		t.Fatal("multiple claim owners", winners.Load())
	}

	store.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	fresh := c

	fresh.ExpiresAt = time.Now().Add(3 * time.Hour)
	if _, err := store.Begin(
		ctx,
		fresh,
	); !errors.Is(
		err,
		idempotency.ErrInProgress,
	) {
		t.Fatal("stole expired pending", err)
	}

	if err := store.Complete(
		ctx,
		c,
		idempotency.Response{
			Status:      200,
			ContentType: "application/json",
			Body:        []byte(`{}`),
		},
	); err != nil {
		t.Fatal(err)
	}

	if replay, err := store.Begin(ctx, fresh); err != nil || replay != nil {
		t.Fatal("expired completed not renewed", replay, err)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()

	if err := store.Complete(
		cancelled,
		fresh,
		idempotency.Response{Status: 200, ContentType: "application/json"},
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatal(err)
	}

	if _, err := store.Begin(
		cancelled,
		claim(t, "cancel", "a", "t", "a"),
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatal(err)
	}

	if _, err := store.Begin(
		ctx,
		fresh,
	); !errors.Is(
		err,
		idempotency.ErrInProgress,
	) {
		t.Fatal(err)
	}
}

func TestCapacityAndResponseBounds(t *testing.T) {
	store, _ := New(1)
	c := claim(t, "one", "alice", "org", "a")

	_, _ = store.Begin(context.Background(), c)
	if _, err := store.Begin(
		context.Background(),
		claim(t, "two", "alice", "org", "a"),
	); !errors.Is(
		err,
		idempotency.ErrCapacity,
	) {
		t.Fatal(err)
	}

	for _, response := range []idempotency.Response{{Status: 200, ContentType: "application/json", Body: make([]byte, idempotency.MaxPayload+1)}, {Status: 200, ContentType: "text/html"}, {Status: 500, ContentType: "application/json"}} {
		if err := store.Complete(
			context.Background(),
			c,
			response,
		); err == nil {
			t.Fatal("unsafe response recorded")
		}
	}
}
