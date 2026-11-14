package state

import "errors"

var (
	ErrBinding   = errors.New("invalid transition binding")
	ErrEvaluated = errors.New("invalid evaluated transition")
	ErrMachine   = errors.New("evaluated transition machine mismatch")
	ErrMismatch  = errors.New("evaluated transition binding mismatch")
)

// Binding identifies the loaded snapshot used for a decision. Scope is an
// application-defined tenant or other persistence namespace, not authorization.
// Versions must be nonnegative and advance on every guard-relevant change.
type Binding[S ~string] struct {
	Scope   string
	Entity  string
	State   S
	Version int64
}

// Evaluated carries a successful machine decision and its original snapshot.
// Its zero value is invalid. Private fields prevent public literals from
// minting
// valid values. Machine.Validate checks the originating machine and binding.
// This is not proof of authorization, persistence, or single use.
type Evaluated[S ~string, E ~string] struct {
	binding Binding[S]
	event   E
	to      S
	origin  *byte
	valid   bool
}

// Evaluate decides the event with Next and binds success to the supplied
// snapshot. It performs no I/O. Failure returns an invalid zero value.
func (m *Machine[S, E, F]) Evaluate(
	binding Binding[S], event E, facts F,
) (Evaluated[S, E], error) {
	if binding.Scope == "" || binding.Entity == "" || binding.Version < 0 {
		return Evaluated[S, E]{}, ErrBinding
	}

	next, err := m.Next(binding.State, event, facts)
	if err != nil {
		return Evaluated[S, E]{}, err
	}

	return Evaluated[S, E]{
		binding: binding,
		event:   event,
		to:      next,
		origin:  m.identity,
		valid:   true,
	}, nil
}

// validate rejects invalid values and decisions for another snapshot. The data
// adapter must also atomically compare this binding in its database write;
// checking a previously loaded record alone does not prevent a stale commit.
func (e Evaluated[S, E]) validate(current Binding[S]) error {
	if !e.valid {
		return ErrEvaluated
	}

	if e.binding != current {
		return ErrMismatch
	}

	return nil
}

// Binding returns the original snapshot; it is a copy, not mutable authority.
func (e Evaluated[S, E]) Binding() Binding[S] { return e.binding }

// Event returns the evaluated event. Machine.Validate must succeed before use.
func (e Evaluated[S, E]) Event() E { return e.event }

// To returns the resulting state. Machine.Validate must succeed before use.
func (e Evaluated[S, E]) To() S { return e.to }

// Validate checks both the originating machine and the bound snapshot. Writers
// should use the intended domain machine, injected at composition, rather than
// accepting a caller-supplied machine. It does not re-run guards or authorize.
func (m *Machine[S, E, F]) Validate(
	e Evaluated[S, E],
	current Binding[S],
) error {
	if err := e.validate(current); err != nil {
		return err
	}

	if m == nil || m.identity == nil || e.origin != m.identity {
		return ErrMachine
	}

	return nil
}
