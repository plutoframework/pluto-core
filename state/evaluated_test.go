package state_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/plutoframework/pluto-core/state"
)

// This consumer-owned port accepts evaluated decisions, never a mutable entity.
type lifecycleWriter interface {
	Commit(state.Evaluated[string, string]) error
}

type boundedWriter struct {
	mu      sync.Mutex
	machine *state.Machine[string, string, bool]
	current state.Binding[string]
}

func (w *boundedWriter) Commit(change state.Evaluated[string, string]) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// A real adapter must implement this comparison and update atomically in
	// SQL.
	if err := w.machine.Validate(change, w.current); err != nil {
		return err
	}

	w.current.State = change.To()
	w.current.Version++

	return nil
}

func evaluatedMachine(t *testing.T) *state.Machine[string, string, bool] {
	t.Helper()

	machine, err := state.New(state.Definition[string, string, bool]{
		States: []string{"pending", "done"},
		Transitions: []state.Transition[string, string, bool]{
			{
				From: "pending",
				On:   "finish",
				To:   "done",
				Guard: func(allowed bool) error {
					if !allowed {
						return errDenied
					}

					return nil
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	return machine
}

var errDenied = errors.New("business guard denied")

func TestEvaluatedRestrictedWrite(t *testing.T) {
	machine := evaluatedMachine(t)
	binding := state.Binding[string]{
		Scope:   "tenant-a",
		Entity:  "purchase-1",
		State:   "pending",
		Version: 7,
	}

	change, err := machine.Evaluate(binding, "finish", true)
	if err != nil {
		t.Fatal(err)
	}

	if change.Binding() != binding || change.Event() != "finish" ||
		change.To() != "done" {
		t.Fatal("decision lost")
	}

	copied := change.Binding()
	copied.Scope = "tenant-b"

	if err := machine.Validate(change, binding); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		current state.Binding[string]
	}{
		{"tenant", copied},
		{
			"entity",
			state.Binding[string]{
				Scope:   binding.Scope,
				Entity:  "other",
				State:   binding.State,
				Version: binding.Version,
			},
		},
		{
			"state",
			state.Binding[string]{
				Scope:   binding.Scope,
				Entity:  binding.Entity,
				State:   "done",
				Version: binding.Version,
			},
		},
		{
			"version",
			state.Binding[string]{
				Scope:   binding.Scope,
				Entity:  binding.Entity,
				State:   binding.State,
				Version: 8,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var writer lifecycleWriter = &boundedWriter{
				machine: machine,
				current: tc.current,
			}
			if err := writer.Commit(
				change,
			); !errors.Is(
				err,
				state.ErrMismatch,
			) {
				t.Fatalf("got %v", err)
			}
		})
	}

	writer := &boundedWriter{machine: machine, current: binding}
	if err := writer.Commit(
		state.Evaluated[string, string]{},
	); !errors.Is(
		err,
		state.ErrEvaluated,
	) {
		t.Fatalf("zero: %v", err)
	}

	other := evaluatedMachine(t)

	foreign, err := other.Evaluate(binding, "finish", true)
	if err != nil {
		t.Fatal(err)
	}

	if err := writer.Commit(foreign); !errors.Is(err, state.ErrMachine) {
		t.Fatalf("foreign: %v", err)
	}

	// Two callers can copy a decision. The persistence comparison rejects
	// replay.
	var wg sync.WaitGroup

	results := make(chan error, 2)

	for range 2 {
		wg.Go(func() { results <- writer.Commit(change) })
	}

	wg.Wait()
	close(results)

	success, stale := 0, 0

	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, state.ErrMismatch) {
			stale++
		} else {
			t.Fatal(err)
		}
	}

	if success != 1 || stale != 1 {
		t.Fatalf("success=%d stale=%d", success, stale)
	}
}

func TestEvaluateFailures(t *testing.T) {
	machine := evaluatedMachine(t)
	binding := state.Binding[string]{
		Scope:  "single-tenant",
		Entity: "shipment-1",
		State:  "pending",
	}

	failed, err := machine.Evaluate(binding, "finish", false)
	if !errors.Is(err, state.ErrGuard) || !errors.Is(err, errDenied) {
		t.Fatalf("guard: %v", err)
	}

	if err := machine.Validate(
		failed,
		binding,
	); !errors.Is(
		err,
		state.ErrEvaluated,
	) {
		t.Fatalf("failed evidence: %v", err)
	}

	for _, invalid := range []state.Binding[string]{
		{
			Entity: "id",
			State:  "pending",
		},
		{
			Scope: "tenant",
			State: "pending",
		},
		{
			Scope:   "tenant",
			Entity:  "id",
			State:   "pending",
			Version: -1,
		},
	} {
		if _, err := machine.Evaluate(
			invalid,
			"finish",
			true,
		); !errors.Is(
			err,
			state.ErrBinding,
		) {
			t.Fatalf("binding: %v", err)
		}
	}

	binding.State = "unknown"
	if _, err := machine.Evaluate(
		binding,
		"finish",
		true,
	); !errors.Is(
		err,
		state.ErrState,
	) {
		t.Fatalf("state: %v", err)
	}

	binding.State = "pending"
	if _, err := machine.Evaluate(
		binding,
		"unknown",
		true,
	); !errors.Is(
		err,
		state.ErrTransition,
	) {
		t.Fatalf("event: %v", err)
	}
}
