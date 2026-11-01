//go:build plutocandidate

package shipmentpostgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/plutoframework/pluto-core/examples/shipments"
	shipmentpostgres "github.com/plutoframework/pluto-core/examples/shipments/postgres"
	"github.com/plutoframework/pluto-core/examples/shipments/postgres/operations"
	"github.com/plutoframework/pluto-core/idempotency"

	identitygate "github.com/plutoframework/pluto-rbac/identity"
)

func TestPostgresConcurrentIdempotentDispatch(t *testing.T) {
	pool := isolated(t)
	f := newDataFixture(t, pool)
	snapshot := loaded()
	seed(t, pool, snapshot)
	grant(t, pool, snapshot.ID, "alice")

	input, err := shipments.NewDispatchInput(snapshot.ID, "concurrent-command")
	if err != nil {
		t.Fatal(err)
	}

	type outcome struct {
		result shipmentpostgres.DispatchResult
		err    error
	}

	results := make(chan outcome, 12)

	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			var result shipmentpostgres.DispatchResult

			err := f.within(
				t,
				"alice",
				snapshot.ID,
				func(ctx context.Context, execution *identitygate.Execution) error {
					var err error

					result, err = operations.Dispatch(
						ctx,
						execution,
						f.data,
						f.machine,
						input,
					)

					return err
				},
			)
			results <- outcome{result: result, err: err}
		})
	}

	wg.Wait()
	close(results)

	success := 0

	var jobID string

	for outcome := range results {
		if outcome.err != nil {
			if !errors.Is(outcome.err, idempotency.ErrInProgress) {
				t.Fatal(outcome.err)
			}

			continue
		}

		success++

		if jobID != "" && jobID != outcome.result.JobID {
			t.Fatal("logical retry minted another delivery ID")
		}

		jobID = outcome.result.JobID
	}

	if success == 0 || jobID == "" {
		t.Fatal("no logical command committed")
	}

	assertRows(t, pool, snapshot, shipments.Dispatched, 8, 1)

	var actor string
	if err := pool.QueryRow(t.Context(), "SELECT actor_id FROM shipment_dispatch_outbox").
		Scan(&actor); err != nil {
		t.Fatal(err)
	}

	if actor != "alice" {
		t.Fatal("audit lost verified actor")
	}

	var jobs, claims int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM pluto_jobs").
		Scan(&jobs); err != nil {
		t.Fatal(err)
	}

	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM pluto_idempotency WHERE completed").
		Scan(&claims); err != nil {
		t.Fatal(err)
	}

	if jobs != 1 || claims != 1 {
		t.Fatalf("jobs=%d completed claims=%d", jobs, claims)
	}

	other := snapshot
	other.ID = "shipment-2"
	seed(t, pool, other)
	grant(t, pool, other.ID, "alice")

	conflicting, err := shipments.NewDispatchInput(other.ID, input.Key())
	if err != nil {
		t.Fatal(err)
	}

	err = f.within(
		t,
		"alice",
		other.ID,
		func(ctx context.Context, execution *identitygate.Execution) error {
			_, err := f.data.StartDispatch(ctx, execution, conflicting)
			return err
		},
	)
	if !errors.Is(err, idempotency.ErrConflict) {
		t.Fatalf("key replayed another logical resource: %v", err)
	}
}
