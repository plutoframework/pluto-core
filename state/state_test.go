package state_test

import (
	"errors"
	"testing"

	"github.com/plutoframework/pluto-core/state"
)

func TestDefinitionAndDecisionBoundaries(t *testing.T) {
	denied := errors.New("business condition")
	d := state.Definition[string, string, bool]{
		States: []string{"draft", "submitted"},
		Transitions: []state.Transition[string, string, bool]{
			{
				From: "draft",
				On:   "submit",
				To:   "submitted",
				Guard: func(ok bool) error {
					if !ok {
						return denied
					}

					return nil
				},
			},
		},
	}

	machine, err := state.New(d)
	if err != nil {
		t.Fatal(err)
	}

	// Editing the source definition cannot change a compiled machine.
	d.States[0] = "changed"
	d.Transitions[0].To = "changed"

	for _, test := range []struct {
		current, event string
		facts          bool
		want           string
		err            error
	}{
		{"draft", "submit", true, "submitted", nil},
		{"draft", "submit", false, "draft", state.ErrGuard},
		{"submitted", "submit", true, "submitted", state.ErrTransition},
		{"draft", "approve", true, "draft", state.ErrTransition},
		{"unknown", "submit", true, "unknown", state.ErrState},
	} {
		next, err := machine.Next(test.current, test.event, test.facts)
		if next != test.want || !errors.Is(err, test.err) {
			t.Fatalf(
				"%s/%s: next=%s err=%v",
				test.current,
				test.event,
				next,
				err,
			)
		}
	}

	_, err = machine.Next("draft", "submit", false)
	if !errors.Is(err, denied) {
		t.Fatal("business error must remain discoverable")
	}
}

func TestRejectInvalidDefinitions(t *testing.T) {
	for _, d := range []state.Definition[string, string, int]{
		{},
		{States: []string{""}},
		{States: []string{"a", "a"}},
		{States: []string{"a"}, Transitions: []state.Transition[string, string, int]{
			{From: "a", On: "go", To: "undeclared"},
		}},
		{States: []string{"a"}, Transitions: []state.Transition[string, string, int]{
			{From: "undeclared", On: "go", To: "a"},
		}},
		{States: []string{"a"}, Transitions: []state.Transition[string, string, int]{
			{From: "a", On: "", To: "a"},
		}},
		{States: []string{"a", "b"}, Transitions: []state.Transition[string, string, int]{
			{From: "a", On: "go", To: "a"},
			{From: "a", On: "go", To: "b"},
		}},
	} {
		if _, err := state.New(d); !errors.Is(err, state.ErrDefinition) {
			t.Fatal("invalid definition accepted")
		}
	}
}

func TestUninitializedAndTerminalMachines(t *testing.T) {
	for _, machine := range []*state.Machine[string, string, struct{}]{
		nil,
		{},
	} {
		next, err := machine.Next("current", "go", struct{}{})
		if next != "current" || !errors.Is(err, state.ErrDefinition) {
			t.Fatal("uninitialized machine accepted a transition")
		}
	}

	terminal, err := state.New(state.Definition[string, string, struct{}]{
		States: []string{"done"},
	})
	if err != nil {
		t.Fatal(err)
	}

	next, err := terminal.Next("done", "go", struct{}{})
	if next != "done" || !errors.Is(err, state.ErrTransition) {
		t.Fatal("terminal machine accepted an event")
	}
}

func TestConcurrentDecisionsAndExplicitSelfTransition(t *testing.T) {
	machine, err := state.New(state.Definition[string, string, struct{}]{
		States: []string{"ready"},
		Transitions: []state.Transition[string, string, struct{}]{
			{From: "ready", On: "refresh", To: "ready"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	for range 20 {
		t.Run("concurrent", func(t *testing.T) {
			t.Parallel()

			next, err := machine.Next("ready", "refresh", struct{}{})
			if next != "ready" || err != nil {
				t.Fatal("explicit self transition rejected")
			}
		})
	}
}
