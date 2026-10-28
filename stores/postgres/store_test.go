package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/plutoframework/pluto-core/idempotency"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func isolated(t *testing.T) (*pgxpool.Pool, *Store) {
	t.Helper()

	url := os.Getenv("PLUTO_CORE_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SQL integration requires PLUTO_CORE_TEST_DATABASE_URL")
	}

	ctx := context.Background()

	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(admin.Close)

	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}

	name := "core_test_" + hex.EncodeToString(nonce[:])

	identifier := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if _, err := admin.Exec(
			ctx,
			"DROP SCHEMA "+identifier+" CASCADE",
		); err != nil {
			t.Error(err)
		}
	})

	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}

	config.ConnConfig.RuntimeParams["search_path"] = name

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(pool.Close)

	if _, err := pool.Exec(ctx, Schema); err != nil {
		t.Fatal(err)
	}

	store, err := New(pool)
	if err != nil {
		t.Fatal(err)
	}

	return pool, store
}

func newClaim(t *testing.T, key string) idempotency.Claim {
	t.Helper()

	claim, err := idempotency.NewClaim(
		idempotency.Scope{Subject: "alice", Tenant: "one", Operation: "create"},
		key,
		strings.Repeat("a", 64),
		time.Now(),
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}

	return claim
}

func TestPostgresConcurrentClaimsAndRetention(t *testing.T) {
	pool, store := isolated(t)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	claim := newClaim(t, "race")

	var (
		winners atomic.Int32
		wg      sync.WaitGroup
	)
	for range 20 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			_, err := store.Begin(ctx, claim)
			if err == nil {
				winners.Add(1)
			} else if !errors.Is(err, idempotency.ErrInProgress) {
				t.Error(err)
			}
		}()
	}

	wg.Wait()

	if winners.Load() != 1 {
		t.Fatal("concurrent claim winners", winners.Load())
	}

	changed := claim

	changed.Fingerprint = strings.Repeat("b", 64)
	if _, err := store.Begin(
		ctx,
		changed,
	); !errors.Is(
		err,
		idempotency.ErrConflict,
	) {
		t.Fatal(err)
	}

	impostor := claim
	impostor.Owner = strings.Repeat("b", 64)

	response := idempotency.Response{
		Status:      201,
		ContentType: "application/json",
		Body:        []byte(`{"id":"1"}`),
	}
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

	if _, err := pool.Exec(
		ctx,
		"UPDATE pluto_idempotency SET expires_at=now()-interval '1 hour' WHERE id=$1",
		claim.ID,
	); err != nil {
		t.Fatal(err)
	}

	if count, err := store.PruneCompleted(ctx); err != nil || count != 0 {
		t.Fatal(count, err)
	}

	if _, err := store.Begin(
		ctx,
		claim,
	); !errors.Is(
		err,
		idempotency.ErrInProgress,
	) {
		t.Fatal("stole expired pending", err)
	}

	if err := store.Complete(ctx, claim, response); err != nil {
		t.Fatal(err)
	}

	// Expired completed claims can renew, but old owners cannot complete them.
	next := newClaim(t, "race")
	if replay, err := store.Begin(ctx, next); err != nil || replay != nil {
		t.Fatal(replay, err)
	}

	if err := store.Complete(
		ctx,
		claim,
		response,
	); !errors.Is(
		err,
		idempotency.ErrOwnership,
	) {
		t.Fatal(err)
	}

	if err := store.Complete(ctx, next, response); err != nil {
		t.Fatal(err)
	}

	replay, err := store.Begin(ctx, newClaim(t, "race"))
	if err != nil || replay == nil ||
		string(replay.Body) != string(response.Body) {
		t.Fatal(replay, err)
	}
}

func TestPostgresBusinessCompletionAtomicity(t *testing.T) {
	pool, store := isolated(t)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if _, err := pool.Exec(
		ctx,
		"CREATE TABLE business_writes (id text PRIMARY KEY)",
	); err != nil {
		t.Fatal(err)
	}

	response := idempotency.Response{
		Status:      201,
		ContentType: "application/json",
		Body:        []byte(`{"created":true}`),
	}

	for _, commit := range []bool{false, true} {
		key := "rollback"
		if commit {
			key = "commit"
		}

		claim := newClaim(t, key)
		if _, err := store.Begin(ctx, claim); err != nil {
			t.Fatal(err)
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}

		completion, _ := New(tx)
		if _, err := tx.Exec(
			ctx,
			"INSERT INTO business_writes (id) VALUES ($1)",
			key,
		); err != nil {
			t.Fatal(err)
		}

		if err := completion.Complete(ctx, claim, response); err != nil {
			t.Fatal(err)
		}

		if commit {
			err = tx.Commit(ctx)
		} else {
			err = tx.Rollback(ctx)
		}

		if err != nil {
			t.Fatal(err)
		}

		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM business_writes WHERE id=$1", key).
			Scan(&count); err != nil {
			t.Fatal(err)
		}

		replay, err := store.Begin(ctx, newClaim(t, key))
		if commit {
			if count != 1 || err != nil || replay == nil {
				t.Fatal(count, replay, err)
			}
		} else if count != 0 || !errors.Is(err, idempotency.ErrInProgress) {
			t.Fatal(
				"rollback published business or completion",
				count,
				replay,
				err,
			)
		}
	}
}
