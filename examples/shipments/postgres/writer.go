// Package shipmentpostgres is an unpublished application reference consumer,
// not a generic Core storage adapter. It owns the shipment SQL write boundary.
package shipmentpostgres

import (
	"context"
	_ "embed"
	"errors"
	"math"
	"time"

	"github.com/plutoframework/pluto-core/examples/shipments"
	"github.com/plutoframework/pluto-core/examples/shipments/postgres/internal/db"
	"github.com/plutoframework/pluto-core/state"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Schema is an explicit reference migration. New never executes DDL.
//
//go:embed schema.sql
var Schema string

var (
	ErrDependencies = errors.New("shipment writer dependencies required")
	ErrConflict     = errors.New("shipment snapshot conflict")
	ErrDispatch     = errors.New("invalid shipment dispatch decision")
)

// Writer has no API accepting a replacement entity or freely constructed change.
// Composition injects the same owning domain machine used by the operation.
// The pool is private and the writer owns its transaction, SQL CAS and outbox.
type Writer struct {
	pool      *pgxpool.Pool
	lifecycle *state.Machine[shipments.Status, shipments.Event, bool]
}

var _ shipments.Writer = (*Writer)(nil)

func New(
	pool *pgxpool.Pool,
	lifecycle *state.Machine[shipments.Status, shipments.Event, bool],
) (*Writer, error) {
	if pool == nil || lifecycle == nil {
		return nil, ErrDependencies
	}

	return &Writer{pool: pool, lifecycle: lifecycle}, nil
}

// CommitDispatch validates the evaluated decision before any database access.
// SQL atomically matches scope, identity, source state and expected version; the
// status/version update and durable delivery/audit intent commit together.
// It does not establish current authorization, run delivery, or re-run guards.
func (w *Writer) CommitDispatch(
	ctx context.Context,
	change state.Evaluated[shipments.Status, shipments.Event],
) error {
	if w == nil || w.pool == nil || w.lifecycle == nil {
		return ErrDependencies
	}

	snapshot := change.Binding()
	if err := w.lifecycle.Validate(change, snapshot); err != nil {
		return err
	}

	if change.Event() != shipments.Dispatch ||
		change.To() != shipments.Dispatched ||
		snapshot.State != shipments.Packed ||
		snapshot.Version == math.MaxInt64 {
		return ErrDispatch
	}

	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return err
	}

	defer func() {

		// Cleanup must still roll back when the caller's context has been canceled.
		cleanup, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cancel()

		_ = tx.Rollback(cleanup)
	}()

	queries := db.New(tx)

	count, err := queries.CommitDispatch(ctx, db.CommitDispatchParams{
		TenantID:        snapshot.Scope,
		ID:              snapshot.Entity,
		PreviousStatus:  string(snapshot.State),
		ExpectedVersion: snapshot.Version,
		NextStatus:      string(change.To()),
	})
	if err != nil {
		return err
	}

	if count != 1 {
		return ErrConflict
	}

	if err := queries.InsertDispatchIntent(ctx, db.InsertDispatchIntentParams{
		TenantID:   snapshot.Scope,
		ShipmentID: snapshot.Entity,
		Version:    snapshot.Version + 1,
		Event:      string(change.Event()),
	}); err != nil {
		return err
	}

	return tx.Commit(ctx)
}
