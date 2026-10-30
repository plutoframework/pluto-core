// Package state describes explicit business lifecycles without owning storage,
// authorization, scheduling, or side effects.
package state

import (
	"errors"
	"fmt"
)

var (
	ErrDefinition = errors.New("invalid state machine definition")
	ErrState      = errors.New("unknown state")
	ErrTransition = errors.New("transition not allowed")
	ErrGuard      = errors.New("transition condition failed")
)

// Guard checks application facts without changing them or performing I/O.
// Facts should be a value snapshot prepared by the application operation.
type Guard[F any] func(F) error

type Transition[S ~string, E ~string, F any] struct {
	From  S
	On    E
	To    S
	Guard Guard[F]
}

type Definition[S ~string, E ~string, F any] struct {
	States      []S
	Transitions []Transition[S, E, F]
}

type key[S ~string, E ~string] struct {
	from  S
	event E
}

// Machine holds a compiled definition, not the current state of an entity.
// Its maps are immutable. Guards must be safe for concurrent evaluation.
type Machine[S ~string, E ~string, F any] struct {
	states      map[S]struct{}
	transitions map[key[S, E]]Transition[S, E, F]
}

// New copies and validates the definition. Each state/event pair has at most
// one transition; declaration order never selects business behavior.
func New[S ~string, E ~string, F any](
	d Definition[S, E, F],
) (*Machine[S, E, F], error) {
	if len(d.States) == 0 {
		return nil, fmt.Errorf(
			"%w: at least one state is required",
			ErrDefinition,
		)
	}

	m := &Machine[S, E, F]{
		states:      make(map[S]struct{}, len(d.States)),
		transitions: make(map[key[S, E]]Transition[S, E, F]),
	}
	for _, value := range d.States {
		if _, duplicate := m.states[value]; value == "" || duplicate {
			return nil, fmt.Errorf(
				"%w: empty or duplicate state %q",
				ErrDefinition,
				value,
			)
		}

		m.states[value] = struct{}{}
	}

	for _, transition := range d.Transitions {
		_, fromKnown := m.states[transition.From]
		_, toKnown := m.states[transition.To]
		k := key[S, E]{from: transition.From, event: transition.On}

		_, duplicate := m.transitions[k]
		if !fromKnown || !toKnown || transition.On == "" || duplicate {
			return nil, fmt.Errorf(
				"%w: transition %q + %q -> %q has undeclared states, an empty event, or a duplicate rule",
				ErrDefinition,
				transition.From,
				transition.On,
				transition.To,
			)
		}

		m.transitions[k] = transition
	}

	return m, nil
}

// Next decides a transition without mutating the entity. Failure returns the
// original state. A successful decision still needs an atomic persistence check.
// It does not prove authorization and must not be cached as permission to write.
func (m *Machine[S, E, F]) Next(current S, event E, facts F) (S, error) {
	if m == nil || m.states == nil {
		return current, ErrDefinition
	}

	if _, known := m.states[current]; !known {
		return current, ErrState
	}

	transition, allowed := m.transitions[key[S, E]{from: current, event: event}]
	if !allowed {
		return current, ErrTransition
	}

	if transition.Guard != nil {
		if err := transition.Guard(facts); err != nil {
			return current, fmt.Errorf("%w: %w", ErrGuard, err)
		}
	}

	return transition.To, nil
}
