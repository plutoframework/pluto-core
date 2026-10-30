// Package purchases is an application lifecycle example.
package purchases

import (
	"errors"

	"github.com/plutoframework/pluto-core/state"
)

type (
	Status string
	Event  string
)

const (
	Draft     Status = "draft"
	Submitted Status = "submitted"
	Approved  Status = "approved"
	Rejected  Status = "rejected"
)

const (
	Submit  Event = "submit"
	Approve Event = "approve"
	Reject  Event = "reject"
)

var (
	ErrCreatorRequired = errors.New("only the creator can submit")
	ErrSelfDecision    = errors.New(
		"the creator cannot decide their own purchase",
	)
	ErrInvalidFacts = errors.New("purchase facts are incomplete")
)

// Facts come from verified server identity and the loaded purchase, never
// directly from an HTTP body or AI tool arguments.
type Facts struct {
	ActorID     string
	CreatorID   string
	AmountCents int64
}

func NewLifecycle() (*state.Machine[Status, Event, Facts], error) {
	return state.New(state.Definition[Status, Event, Facts]{
		States: []Status{Draft, Submitted, Approved, Rejected},
		Transitions: []state.Transition[Status, Event, Facts]{
			{
				From:  Draft,
				On:    Submit,
				To:    Submitted,
				Guard: canSubmit,
			},
			{
				From:  Submitted,
				On:    Approve,
				To:    Approved,
				Guard: canDecide,
			},
			{
				From:  Submitted,
				On:    Reject,
				To:    Rejected,
				Guard: canDecide,
			},
		},
	})
}

func canSubmit(f Facts) error {
	if f.ActorID == "" || f.CreatorID == "" || f.AmountCents <= 0 {
		return ErrInvalidFacts
	}

	if f.ActorID != f.CreatorID {
		return ErrCreatorRequired
	}

	return nil
}

func canDecide(f Facts) error {
	if f.ActorID == "" || f.CreatorID == "" || f.AmountCents <= 0 {
		return ErrInvalidFacts
	}

	if f.ActorID == f.CreatorID {
		return ErrSelfDecision
	}

	return nil
}
