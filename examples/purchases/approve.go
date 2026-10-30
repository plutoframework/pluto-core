package purchases

import (
	"context"
	"errors"

	"github.com/plutoframework/pluto-core/state"
)

// Principal must be constructed from verified server identity.
type Principal struct {
	ActorID  string
	TenantID string
}

type Purchase struct {
	ID          string
	TenantID    string
	CreatorID   string
	Status      Status
	Version     int64
	AmountCents int64
}

// Change records the decision and the snapshot it depends on.
type Change struct {
	PurchaseID      string
	TenantID        string
	ActorID         string
	ExpectedVersion int64
	From            Status
	Event           Event
	To              Status
}

type Authorizer interface {
	Require(context.Context, Principal, string) error
}

// Repository is an application-owned boundary. Commit must atomically check
// tenant, ID, prior status and version, update status/version, and record the
// audit event and outbox work. A stale snapshot returns a conflict, with no
// writes. All changes to guard-relevant facts must advance the same version.
// Delivery happens after commit through the existing jobs/outbox worker.
type Repository interface {
	Purchase(ctx context.Context, tenantID, id string) (Purchase, error)
	Commit(context.Context, Change) error
}

type Service struct {
	authorizer Authorizer
	repository Repository
	lifecycle  *state.Machine[Status, Event, Facts]
}

func NewService(
	authorizer Authorizer,
	repository Repository,
) (*Service, error) {
	if authorizer == nil || repository == nil {
		return nil, errors.New("purchase dependencies required")
	}

	lifecycle, err := NewLifecycle()
	if err != nil {
		return nil, err
	}

	return &Service{
		authorizer: authorizer,
		repository: repository,
		lifecycle:  lifecycle,
	}, nil
}

func (s *Service) Approve(
	ctx context.Context,
	actor Principal,
	id string,
) error {
	if actor.ActorID == "" || actor.TenantID == "" {
		return errors.New("verified identity required")
	}

	if err := s.authorizer.Require(ctx, actor, "purchases:decide"); err != nil {
		return err
	}

	purchase, err := s.repository.Purchase(ctx, actor.TenantID, id)
	if err != nil {
		return err
	}

	if purchase.TenantID != actor.TenantID || purchase.ID != id {
		return errors.New("purchase scope mismatch")
	}

	next, err := s.lifecycle.Next(purchase.Status, Approve, Facts{
		ActorID:     actor.ActorID,
		CreatorID:   purchase.CreatorID,
		AmountCents: purchase.AmountCents,
	})
	if err != nil {
		return err
	}

	return s.repository.Commit(ctx, Change{
		PurchaseID:      purchase.ID,
		TenantID:        actor.TenantID,
		ActorID:         actor.ActorID,
		ExpectedVersion: purchase.Version,
		From:            purchase.Status,
		Event:           Approve,
		To:              next,
	})
}
