package shipments_test

import (
	"context"
	"errors"
	"testing"

	"github.com/plutoframework/pluto-core/examples/shipments"
	"github.com/plutoframework/pluto-core/state"
)

type writer struct {
	machine *state.Machine[shipments.Status, shipments.Event, bool]
	current shipments.Snapshot
	writes  int
}

func (w *writer) CommitDispatch(
	_ context.Context,
	change state.Evaluated[shipments.Status, shipments.Event],
) error {
	if err := w.machine.Validate(change, w.current.Binding()); err != nil {
		return err
	}

	w.current.Status = change.To()
	w.current.Version++
	w.writes++

	return nil
}

func TestDispatchLoaded(t *testing.T) {
	machine, err := shipments.NewLifecycle()
	if err != nil {
		t.Fatal(err)
	}

	loaded := shipments.Snapshot{
		TenantID: "tenant",
		ID:       "shipment",
		Status:   shipments.Packed,
		Version:  3,
	}
	adapter := &writer{machine: machine, current: loaded}

	err = shipments.DispatchLoaded(
		context.Background(),
		machine,
		adapter,
		loaded,
	)
	if !errors.Is(err, shipments.ErrAddress) ||
		!errors.Is(err, state.ErrGuard) ||
		adapter.writes != 0 {
		t.Fatalf("guard: %v, writes: %d", err, adapter.writes)
	}

	loaded.AddressVerified = true

	adapter.current = loaded
	if err = shipments.DispatchLoaded(
		context.Background(),
		machine,
		adapter,
		loaded,
	); err != nil {
		t.Fatal(err)
	}

	if adapter.current.Status != shipments.Dispatched ||
		adapter.current.Version != 4 {
		t.Fatal("missing commit")
	}

	if err = shipments.DispatchLoaded(
		context.Background(),
		machine,
		adapter,
		loaded,
	); !errors.Is(
		err,
		state.ErrMismatch,
	) {
		t.Fatalf("stale: %v", err)
	}
}
