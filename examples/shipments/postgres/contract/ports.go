//go:build plutocandidate

// Package contract contains this application's operation-owned data ports and
// result types. It has no SQL implementation or use-case execution.
package contract

import (
	"context"

	"github.com/plutoframework/pluto-core/examples/shipments"
	"github.com/plutoframework/pluto-core/pagination"
	"github.com/plutoframework/pluto-core/state"

	identitygate "github.com/plutoframework/pluto-rbac/identity"
)

type Result struct {
	ShipmentID string           `json:"shipment_id"`
	Status     shipments.Status `json:"status"`
	Version    int64            `json:"version"`
	JobID      string           `json:"job_id"`
}

type Summary struct {
	ID      string           `json:"id"`
	Status  shipments.Status `json:"status"`
	Version int64            `json:"version"`
}

// Attempt is inspected for replay only. Implementing this interface cannot mint
// a valid commit capability: the concrete data adapter rejects foreign values.
type Attempt interface{ Replayed() (Result, bool) }

type DispatchData interface {
	StartDispatch(
		context.Context,
		*identitygate.Execution,
		shipments.DispatchInput,
	) (Attempt, error)
	Load(
		context.Context,
		*identitygate.Execution,
		string,
	) (shipments.Snapshot, error)
	CommitDispatch(
		context.Context,
		*identitygate.Execution,
		Attempt,
		state.Evaluated[shipments.Status, shipments.Event],
	) (Result, error)
}

type ListData interface {
	List(
		context.Context,
		*identitygate.Execution,
		shipments.ListInput,
	) (pagination.Page[Summary], error)
}

type ExportData interface {
	Export(
		context.Context,
		*identitygate.Execution,
		shipments.ExportInput,
	) ([]Summary, error)
}
