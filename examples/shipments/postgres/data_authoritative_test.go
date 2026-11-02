//go:build plutocandidate

package shipmentpostgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/plutoframework/pluto-core/cache"
	"github.com/plutoframework/pluto-core/examples/shipments"
	"github.com/plutoframework/pluto-core/examples/shipments/postgres/operations"

	identitygate "github.com/plutoframework/pluto-rbac/identity"
)

func TestPostgresAuthoritativeGuardFacts(t *testing.T) {
	pool := isolated(t)
	f := newDataFixture(t, pool)
	snapshot := loaded()
	snapshot.AddressVerified = false
	seed(t, pool, snapshot)
	grant(t, pool, snapshot.ID, "alice")

	poisoned := snapshot
	poisoned.AddressVerified = true

	body, err := json.Marshal(poisoned)
	if err != nil {
		t.Fatal(err)
	}

	key, err := cache.Key(
		cache.Scope{
			App:         "reference",
			Environment: "test",
			Namespace:   "shipment-snapshot",
			Version:     "v1",
			TenantID:    "tenant-a",
			SubjectID:   "alice",
		},
		snapshot.ID,
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.cache.Set(
		t.Context(),
		key,
		body,
		time.Minute,
	); err != nil {
		t.Fatal(err)
	}

	input, err := shipments.NewDispatchInput(snapshot.ID, "guard-facts")
	if err != nil {
		t.Fatal(err)
	}

	err = f.within(
		t,
		"alice",
		snapshot.ID,
		func(ctx context.Context, execution *identitygate.Execution) error {
			display, err := f.data.Display(ctx, execution, snapshot.ID)
			if err != nil {
				return err
			}

			if !display.AddressVerified {
				return errors.New("cache poison control did not reach display")
			}

			_, err = operations.Dispatch(
				ctx,
				execution,
				f.data,
				f.machine,
				input,
			)
			if !errors.Is(err, shipments.ErrAddress) {
				return errors.New("CACHED_GUARD_FACTS_AUTHORIZED_TRANSITION")
			}

			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	assertRows(t, pool, snapshot, shipments.Packed, 7, 0)
}
