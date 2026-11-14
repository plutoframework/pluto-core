//go:build plutocandidate

package shipmentpostgres_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/plutoframework/pluto-core/examples/shipments"
	shipmentpostgres "github.com/plutoframework/pluto-core/examples/shipments/postgres"
	"github.com/plutoframework/pluto-core/examples/shipments/postgres/operations"

	auth "github.com/plutoframework/pluto-auth"
	jobs "github.com/plutoframework/pluto-jobs"
	jobspostgres "github.com/plutoframework/pluto-jobs/stores/postgres"
	identitygate "github.com/plutoframework/pluto-rbac/identity"
)

func TestPostgresTypedDeliveryAfterCommit(t *testing.T) {
	pool := isolated(t)
	f := newDataFixture(t, pool)
	snapshot := loaded()
	seed(t, pool, snapshot)
	grant(t, pool, snapshot.ID, "alice")

	input, err := shipments.NewDispatchInput(snapshot.ID, "delivery-1")
	if err != nil {
		t.Fatal(err)
	}

	var committed shipmentpostgres.DispatchResult

	err = f.within(
		t,
		"alice",
		input.ID(),
		func(ctx context.Context, execution *identitygate.Execution) error {
			var err error

			committed, err = operations.Dispatch(
				ctx,
				execution,
				f.data,
				f.machine,
				input,
			)

			return err
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	effect, err := shipmentpostgres.NewDispatchEffect()
	if err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32

	handler, err := effect.Bind(
		func(
			ctx context.Context,
			delivery jobs.Delivery,
			payload shipmentpostgres.DispatchNotice,
		) error {
			if delivery.ID != committed.JobID || delivery.TenantID == nil ||
				*delivery.TenantID != "tenant-a" ||
				payload.ShipmentID != snapshot.ID {
				return jobs.Permanent(jobs.ErrInvalid)
			}

			// A composition-owned worker identity is verified anew. Stored
			// tenant/job
			// metadata never substitutes for the current operation's
			// authorization.
			return f.manager.WithIdentity(
				ctx,
				f.tokens["alice"],
				func(ctx context.Context, identity *auth.Identity) error {
					return f.gate.Within(
						ctx,
						identity,
						"tenant-a",
						payload.ShipmentID,
						func(
							ctx context.Context,
							execution *identitygate.Execution,
						) error {
							return f.gate.Use(
								ctx,
								execution,
								"tenant-a",
								payload.ShipmentID,
								func(context.Context) error {
									calls.Add(1)

									return nil
								},
							)
						},
					)
				},
			)
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	store, err := jobspostgres.New(pool)
	if err != nil {
		t.Fatal(err)
	}

	config := jobs.DefaultConfig()
	config.Concurrency = 1
	config.PollInterval = time.Millisecond

	worker, err := jobs.NewWorker(
		store,
		map[jobs.Kind]jobs.Handler{effect.Kind(): handler},
		config,
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()

	defer func() {
		cancel()

		err := <-done
		if !errors.Is(err, context.Canceled) {
			t.Errorf("worker shutdown: %v", err)
		}
	}()

	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()

	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()

	for {
		var state string
		if err := pool.QueryRow(
			t.Context(),
			"SELECT state FROM pluto_jobs WHERE id=$1",
			committed.JobID,
		).
			Scan(&state); err != nil {
			t.Fatal(err)
		}

		if state == "succeeded" {
			break
		}

		select {
		case <-deadline.C:
			t.Fatal("typed dispatch delivery did not settle")
		case <-tick.C:
		}
	}

	if calls.Load() != 1 {
		t.Fatalf("typed operation calls=%d", calls.Load())
	}
}
