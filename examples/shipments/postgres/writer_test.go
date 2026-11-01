package shipmentpostgres_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/plutoframework/pluto-core/examples/shipments"
	shipmentpostgres "github.com/plutoframework/pluto-core/examples/shipments/postgres"
	"github.com/plutoframework/pluto-core/state"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func isolated(t *testing.T) *pgxpool.Pool {
	t.Helper()

	url := os.Getenv("PLUTO_CORE_TEST_DATABASE_URL")
	if url == "" {
		if os.Getenv("PLUTO_CORE_REQUIRE_DATABASE") == "1" {
			t.Fatal(
				"required shipment SQL integration needs PLUTO_CORE_TEST_DATABASE_URL",
			)
		}

		t.Skip("shipment SQL integration needs PLUTO_CORE_TEST_DATABASE_URL")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(admin.Close)

	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}

	name := "shipment_test_" + hex.EncodeToString(nonce[:])

	identifier := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cancel()

		if _, err := admin.Exec(
			cleanup,
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

	if _, err := pool.Exec(ctx, shipmentpostgres.Schema); err != nil {
		t.Fatal(err)
	}

	return pool
}

func owningMachine(
	t *testing.T,
) *state.Machine[shipments.Status, shipments.Event, bool] {
	t.Helper()

	machine, err := shipments.NewLifecycle()
	if err != nil {
		t.Fatal(err)
	}

	return machine
}

func loaded() shipments.Snapshot {
	return shipments.Snapshot{
		TenantID:        "tenant-a",
		ID:              "shipment-1",
		Status:          shipments.Packed,
		Version:         7,
		AddressVerified: true,
	}
}

func seed(t *testing.T, pool *pgxpool.Pool, snapshot shipments.Snapshot) {
	t.Helper()

	_, err := pool.Exec(
		t.Context(),
		"INSERT INTO shipments (tenant_id,id,status,version,address_verified) VALUES ($1,$2,$3,$4,$5)",
		snapshot.TenantID,
		snapshot.ID,
		string(snapshot.Status),
		snapshot.Version,
		snapshot.AddressVerified,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func decision(
	t *testing.T,
	machine *state.Machine[shipments.Status, shipments.Event, bool],
	snapshot shipments.Snapshot,
) state.Evaluated[shipments.Status, shipments.Event] {
	t.Helper()

	token, err := machine.Evaluate(
		snapshot.Binding(),
		shipments.Dispatch,
		snapshot.AddressVerified,
	)
	if err != nil {
		t.Fatal(err)
	}

	return token
}

func writer(
	t *testing.T,
	pool *pgxpool.Pool,
	machine *state.Machine[shipments.Status, shipments.Event, bool],
) *shipmentpostgres.Writer {
	t.Helper()

	writer, err := shipmentpostgres.New(pool, machine)
	if err != nil {
		t.Fatal(err)
	}

	return writer
}

func assertRows(
	t *testing.T,
	pool *pgxpool.Pool,
	snapshot shipments.Snapshot,
	status shipments.Status,
	version int64,
	intents int,
) {
	t.Helper()

	var (
		gotStatus  string
		gotVersion int64
	)
	if err := pool.QueryRow(t.Context(), "SELECT status, version FROM shipments WHERE tenant_id=$1 AND id=$2", snapshot.TenantID, snapshot.ID).
		Scan(&gotStatus, &gotVersion); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM shipment_dispatch_outbox").
		Scan(&count); err != nil {
		t.Fatal(err)
	}

	if gotStatus != string(status) || gotVersion != version ||
		count != intents {
		t.Fatalf(
			"status=%s version=%d intents=%d",
			gotStatus,
			gotVersion,
			count,
		)
	}
}

func TestPostgresDispatchBinding(t *testing.T) {
	for _, name := range []string{"zero", "foreign-machine", "scope", "entity", "state", "version", "valid"} {
		t.Run(name, func(t *testing.T) {
			pool := isolated(t)
			machine := owningMachine(t)
			snapshot := loaded()
			seed(t, pool, snapshot)

			adapter := writer(t, pool, machine)
			token := decision(t, machine, snapshot)
			expected := shipmentpostgres.ErrConflict

			switch name {
			case "zero":
				token = state.Evaluated[shipments.Status, shipments.Event]{}
				expected = state.ErrEvaluated
			case "foreign-machine":
				token = decision(t, owningMachine(t), snapshot)
				expected = state.ErrMachine
			case "scope":
				other := snapshot
				other.TenantID = "tenant-b"
				token = decision(t, machine, other)
				other.Status = shipments.Dispatched
				other.Version = 8
				seed(t, pool, other)
			case "entity":
				other := snapshot
				other.ID = "shipment-2"
				token = decision(t, machine, other)
				other.Status = shipments.Dispatched
				other.Version = 8
				seed(t, pool, other)
			case "state":
				if _, err := pool.Exec(
					t.Context(),
					"UPDATE shipments SET status='dispatched'",
				); err != nil {
					t.Fatal(err)
				}
			case "version":
				if _, err := pool.Exec(
					t.Context(),
					"UPDATE shipments SET version=8",
				); err != nil {
					t.Fatal(err)
				}
			case "valid":
				expected = nil
			}

			err := adapter.CommitDispatch(t.Context(), token)
			if !errors.Is(err, expected) {
				t.Fatalf("got %v want %v", err, expected)
			}

			status, version, intents := shipments.Packed, int64(7), 0
			if name == "valid" {
				status, version, intents = shipments.Dispatched, 8, 1
			}

			if name == "state" {
				status = shipments.Dispatched
			}

			if name == "version" {
				version = 8
			}

			assertRows(t, pool, snapshot, status, version, intents)
		})
	}
}

func TestPostgresConcurrentReplay(t *testing.T) {
	pool := isolated(t)
	machine := owningMachine(t)
	snapshot := loaded()
	seed(t, pool, snapshot)

	// Same entity ID in a different tenant must remain unchanged.
	neighbor := snapshot
	neighbor.TenantID = "tenant-b"
	seed(t, pool, neighbor)

	adapter := writer(t, pool, machine)
	token := decision(t, machine, snapshot)
	results := make(chan error, 12)

	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() { results <- adapter.CommitDispatch(t.Context(), token) })
	}

	wg.Wait()
	close(results)

	winners, conflicts := 0, 0

	for err := range results {
		if err == nil {
			winners++
		} else if errors.Is(err, shipmentpostgres.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}

	if winners != 1 || conflicts != 11 {
		t.Fatalf("winners=%d conflicts=%d", winners, conflicts)
	}

	if err := adapter.CommitDispatch(
		t.Context(),
		token,
	); !errors.Is(
		err,
		shipmentpostgres.ErrConflict,
	) {
		t.Fatalf("replay: %v", err)
	}

	assertRows(t, pool, snapshot, shipments.Dispatched, 8, 1)
	assertRows(t, pool, neighbor, shipments.Packed, 7, 1)
}

func TestPostgresOutboxRollback(t *testing.T) {
	pool := isolated(t)
	machine := owningMachine(t)
	snapshot := loaded()
	seed(t, pool, snapshot)

	if _, err := pool.Exec(
		t.Context(),
		"ALTER TABLE shipment_dispatch_outbox ADD CONSTRAINT injected_failure CHECK (false) NOT VALID",
	); err != nil {
		t.Fatal(err)
	}

	adapter := writer(t, pool, machine)

	token := decision(t, machine, snapshot)
	if err := adapter.CommitDispatch(t.Context(), token); err == nil {
		t.Fatal("outbox failure accepted")
	}

	assertRows(t, pool, snapshot, shipments.Packed, 7, 0)

	if _, err := pool.Exec(
		t.Context(),
		"ALTER TABLE shipment_dispatch_outbox DROP CONSTRAINT injected_failure",
	); err != nil {
		t.Fatal(err)
	}

	if err := adapter.CommitDispatch(t.Context(), token); err != nil {
		t.Fatal(err)
	}

	assertRows(t, pool, snapshot, shipments.Dispatched, 8, 1)
}

func TestInvalidBeforeDatabaseAccess(t *testing.T) {
	config, err := pgxpool.ParseConfig(
		"postgres://unused:unused@127.0.0.1:1/unused?sslmode=disable",
	)
	if err != nil {
		t.Fatal(err)
	}

	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	machine := owningMachine(t)

	adapter := writer(t, pool, machine)
	if err := adapter.CommitDispatch(
		t.Context(),
		state.Evaluated[shipments.Status, shipments.Event]{},
	); !errors.Is(
		err,
		state.ErrEvaluated,
	) {
		t.Fatalf("zero: %v", err)
	}

	foreign := decision(t, owningMachine(t), loaded())
	if err := adapter.CommitDispatch(
		t.Context(),
		foreign,
	); !errors.Is(
		err,
		state.ErrMachine,
	) {
		t.Fatalf("foreign: %v", err)
	}
}

func TestRequiredDatabaseGate(t *testing.T) {
	command := exec.CommandContext(
		t.Context(),
		"bash",
		"scripts/database-check.sh",
	)

	command.Env = append(os.Environ(), "PLUTO_CORE_TEST_DATABASE_URL=")

	output, err := command.CombinedOutput()
	if err == nil ||
		!strings.Contains(
			string(output),
			"required isolated database URL is missing",
		) {
		t.Fatalf("missing DB was not rejected: %v\n%s", err, output)
	}
}
