//go:build plutocandidate

// Package operations owns application use cases over small contract ports.
package operations

import (
	"context"

	"github.com/plutoframework/pluto-core/examples/shipments"
	"github.com/plutoframework/pluto-core/examples/shipments/postgres/contract"
	"github.com/plutoframework/pluto-core/state"

	identitygate "github.com/plutoframework/pluto-rbac/identity"
)

// Dispatch is reached inside the composition-owned identity/resource gate.
// The data adapter rechecks authority at every port call, including replay and
// cache hits. This operation owns ordering and delegates rules to the domain.
func Dispatch(
	ctx context.Context,
	execution *identitygate.Execution,
	data contract.DispatchData,
	machine *state.Machine[shipments.Status, shipments.Event, bool],
	input shipments.DispatchInput,
) (contract.Result, error) {
	if err := input.Validate(); err != nil {
		return contract.Result{}, err
	}

	if data == nil || machine == nil {
		return contract.Result{}, shipments.ErrInput
	}

	attempt, err := data.StartDispatch(ctx, execution, input)
	if err != nil {
		return contract.Result{}, err
	}

	if attempt == nil {
		return contract.Result{}, shipments.ErrInput
	}

	if result, replayed := attempt.Replayed(); replayed {
		return result, nil
	}

	loaded, err := data.Load(ctx, execution, input.ID())
	if err != nil {
		return contract.Result{}, err
	}

	change, err := machine.Evaluate(
		loaded.Binding(),
		shipments.Dispatch,
		loaded.AddressVerified,
	)
	if err != nil {
		return contract.Result{}, err
	}

	return data.CommitDispatch(ctx, execution, attempt, change)
}
