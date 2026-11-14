//go:build plutocandidate

package operations_test

import (
	"context"
	"errors"
	"testing"

	"github.com/plutoframework/pluto-core/examples/shipments"
	"github.com/plutoframework/pluto-core/examples/shipments/postgres/contract"
	"github.com/plutoframework/pluto-core/examples/shipments/postgres/operations"
	"github.com/plutoframework/pluto-core/state"

	identitygate "github.com/plutoframework/pluto-rbac/identity"
)

type attempt struct{ replay bool }

func (a attempt) Replayed() (contract.Result, bool) {
	return contract.Result{ShipmentID: "shipment-1"}, a.replay
}

type port struct {
	calls        []string
	replay       bool
	validAddress bool
}

func (p *port) StartDispatch(
	context.Context,
	*identitygate.Execution,
	shipments.DispatchInput,
) (contract.Attempt, error) {
	p.calls = append(p.calls, "start")
	return attempt{replay: p.replay}, nil
}

func (p *port) Load(
	context.Context,
	*identitygate.Execution,
	string,
) (shipments.Snapshot, error) {
	p.calls = append(p.calls, "load")

	return shipments.Snapshot{
		TenantID:        "tenant",
		ID:              "shipment-1",
		Status:          shipments.Packed,
		AddressVerified: p.validAddress,
	}, nil
}

func (p *port) CommitDispatch(
	context.Context,
	*identitygate.Execution,
	contract.Attempt,
	state.Evaluated[shipments.Status, shipments.Event],
) (contract.Result, error) {
	p.calls = append(p.calls, "commit")
	return contract.Result{}, nil
}

func TestDispatchSupportedOrdering(t *testing.T) {
	machine, err := shipments.NewLifecycle()
	if err != nil {
		t.Fatal(err)
	}

	input, err := shipments.NewDispatchInput(" shipment-1 ", "logical-key")
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name   string
		replay bool
		valid  bool
		want   string
	}{
		{"committed", false, true, "start,load,commit"},
		{"replayed", true, false, "start"},
		{"guard-denied", false, false, "start,load"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &port{replay: tc.replay, validAddress: tc.valid}

			_, err := operations.Dispatch(t.Context(), nil, p, machine, input)
			if tc.name == "guard-denied" &&
				!errors.Is(err, shipments.ErrAddress) {
				t.Fatal(err)
			}

			if tc.name != "guard-denied" && err != nil {
				t.Fatal(err)
			}

			got := ""
			for _, call := range p.calls {
				if got != "" {
					got += ","
				}

				got += call
			}

			if got != tc.want {
				t.Fatalf("ordering %s want %s", got, tc.want)
			}
		})
	}

	p := &port{}
	if _, err := operations.Dispatch(
		t.Context(),
		nil,
		p,
		machine,
		shipments.DispatchInput{},
	); !errors.Is(err, shipments.ErrInput) ||
		len(p.calls) != 0 {
		t.Fatal("invalid input reached data port")
	}
}
