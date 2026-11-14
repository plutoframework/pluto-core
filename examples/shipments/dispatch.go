// Package shipments demonstrates a restricted lifecycle write contract.
package shipments

import (
	"context"
	"errors"

	"github.com/plutoframework/pluto-core/state"
)

type (
	Status string
	Event  string
)

const (
	Packed     Status = "packed"
	Dispatched Status = "dispatched"
	Dispatch   Event  = "dispatch"
)

var ErrAddress = errors.New("verified delivery address required")

// Snapshot is loaded through a scoped read port after operation authorization.
type Snapshot struct {
	TenantID        string
	ID              string
	Status          Status
	Version         int64
	AddressVerified bool
}

func (s Snapshot) Binding() state.Binding[Status] {
	return state.Binding[Status]{
		Scope:   s.TenantID,
		Entity:  s.ID,
		State:   s.Status,
		Version: s.Version,
	}
}

// Writer must use the same injected machine instance to Validate the decision,
// then atomically compare its binding and persist status/version and delivery
// intent. A successful domain decision alone is not permission or a commit.
type Writer interface {
	CommitDispatch(context.Context, state.Evaluated[Status, Event]) error
}

func NewLifecycle() (*state.Machine[Status, Event, bool], error) {
	return state.New(state.Definition[Status, Event, bool]{
		States: []Status{Packed, Dispatched},
		Transitions: []state.Transition[Status, Event, bool]{
			{From: Packed, On: Dispatch, To: Dispatched, Guard: hasAddress},
		},
	})
}

func hasAddress(verified bool) error {
	if !verified {
		return ErrAddress
	}

	return nil
}

// DispatchLoaded is called by an authorized operation with loaded facts. The
// operation still owns access checks and transaction/effect coordination.
func DispatchLoaded(
	ctx context.Context,
	machine *state.Machine[Status, Event, bool],
	writer Writer,
	loaded Snapshot,
) error {
	change, err := machine.Evaluate(
		loaded.Binding(),
		Dispatch,
		loaded.AddressVerified,
	)
	if err != nil {
		return err
	}

	return writer.CommitDispatch(ctx, change)
}
