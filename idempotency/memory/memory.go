// Package memory is bounded, process-local demonstration storage. Never use it
// as a production multi-instance fallback. Pending claims require reconciliation.
package memory

import (
	"context"
	"sync"
	"time"

	"github.com/plutoframework/pluto-core/idempotency"
)

type (
	record struct {
		claim    idempotency.Claim
		response *idempotency.Response
	}
	Store struct {
		mu       sync.Mutex
		records  map[string]record
		capacity int
		now      func() time.Time
	}
)

func New(capacity int) (*Store, error) {
	if capacity < 1 {
		return nil, idempotency.ErrInvalid
	}

	return &Store{
		records:  make(map[string]record),
		capacity: capacity,
		now:      time.Now,
	}, nil
}

func clone(r *idempotency.Response) *idempotency.Response {
	if r == nil {
		return nil
	}

	return &idempotency.Response{
		Status:      r.Status,
		ContentType: r.ContentType,
		Body:        append([]byte(nil), r.Body...),
	}
}

func (s *Store) Begin(
	ctx context.Context,
	claim idempotency.Claim,
) (*idempotency.Response, error) {
	if err := claim.Validate(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	now := s.now()
	if !claim.ExpiresAt.After(now) ||
		claim.ExpiresAt.After(now.Add(idempotency.MaxTTL)) {
		return nil, idempotency.ErrInvalid
	}

	for id, r := range s.records {
		if r.response != nil && !r.claim.ExpiresAt.After(now) {
			delete(s.records, id)
		}
	}

	if r, ok := s.records[claim.ID]; ok {
		if r.claim.Fingerprint != claim.Fingerprint {
			return nil, idempotency.ErrConflict
		}

		if r.response == nil {
			return nil, idempotency.ErrInProgress
		}

		return clone(r.response), nil
	}

	if len(s.records) >= s.capacity {
		return nil, idempotency.ErrCapacity
	}

	s.records[claim.ID] = record{claim: claim}

	return nil, nil
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

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	r, ok := s.records[claim.ID]
	if !ok || r.claim.Owner != claim.Owner ||
		r.claim.Fingerprint != claim.Fingerprint ||
		r.response != nil {
		return idempotency.ErrOwnership
	}

	r.response = clone(&response)
	s.records[claim.ID] = r

	return nil
}
