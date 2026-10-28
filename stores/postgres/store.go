// Package postgres provides pgx/sqlc durable owned idempotency claims. Apply
// Schema as an explicit application migration. New never executes DDL.
package postgres

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/plutoframework/pluto-core/idempotency"
	"github.com/plutoframework/pluto-core/stores/postgres/internal/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

//go:embed schema.sql
var Schema string

type DBTX interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Store struct{ queries *db.Queries }

// New accepts a pool for independent HTTP claims, or a transaction for atomic
// business mutation + completion. Connections/transactions are not concurrency
// safe. Begin must commit BEFORE executing an independently committed operation.
func New(database DBTX) (*Store, error) {
	if database == nil {
		return nil, idempotency.ErrInvalid
	}

	return &Store{queries: db.New(database)}, nil
}

func (s *Store) Begin(
	ctx context.Context,
	claim idempotency.Claim,
) (*idempotency.Response, error) {
	if err := claim.Validate(); err != nil {
		return nil, err
	}

	now := time.Now()
	if !claim.ExpiresAt.After(now) ||
		claim.ExpiresAt.After(now.Add(idempotency.MaxTTL)) {
		return nil, idempotency.ErrInvalid
	}

	owner, err := s.queries.BeginClaim(
		ctx,
		db.BeginClaimParams{
			ID:          claim.ID,
			Fingerprint: claim.Fingerprint,
			Owner:       claim.Owner,
			ExpiresAt:   claim.ExpiresAt,
		},
	)
	if err == nil {
		if owner != claim.Owner {
			return nil, idempotency.ErrOwnership
		}

		return nil, nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	record, err := s.queries.ReadClaim(ctx, claim.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, idempotency.ErrInProgress
	}

	if err != nil {
		return nil, err
	}

	if record.Fingerprint != claim.Fingerprint {
		return nil, idempotency.ErrConflict
	}

	if !record.Completed {
		return nil, idempotency.ErrInProgress
	}

	response := idempotency.Response{
		Status:      int(record.Status),
		ContentType: record.ContentType,
		Body:        record.Body,
	}
	if err := response.Validate(); err != nil {
		return nil, err
	}

	return &response, nil
}

func (s *Store) Complete(
	ctx context.Context,
	claim idempotency.Claim,
	response idempotency.Response,
) error {
	if err := claim.Validate(); err != nil {
		return err
	}

	if err := response.Validate(); err != nil {
		return err
	}

	count, err := s.queries.CompleteClaim(
		ctx,
		db.CompleteClaimParams{
			ID:          claim.ID,
			Owner:       claim.Owner,
			Fingerprint: claim.Fingerprint,
			Status:      int32(response.Status),
			ContentType: response.ContentType,
			Body:        append([]byte{}, response.Body...),
		},
	)
	if err != nil {
		return err
	}

	if count != 1 {
		return idempotency.ErrOwnership
	}

	return nil
}

// PruneCompleted removes at most 1,000 expired completed records. Schedule this
// explicitly; pending claims are retained for operator reconciliation forever.
func (s *Store) PruneCompleted(ctx context.Context) (int64, error) {
	return s.queries.PruneCompleted(ctx)
}

var _ idempotency.Store = (*Store)(nil)
