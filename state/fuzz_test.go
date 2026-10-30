package state_test

import (
	"testing"

	"github.com/plutoframework/pluto-core/state"
)

func FuzzTransitions(f *testing.F) {
	f.Add("abbaa")
	f.Add("")
	f.Fuzz(func(t *testing.T, events string) {
		machine, err := state.New(state.Definition[string, string, struct{}]{
			States: []string{"new", "ready", "done"},
			Transitions: []state.Transition[string, string, struct{}]{
				{From: "new", On: "a", To: "ready"},
				{From: "ready", On: "b", To: "done"},
			},
		})
		if err != nil {
			t.Fatal(err)
		}

		current := "new"
		for _, event := range events {
			before := current
			next, err := machine.Next(current, string(event), struct{}{})

			allowed := (before == "new" && event == 'a') ||
				(before == "ready" && event == 'b')
			if allowed != (err == nil) || (!allowed && next != before) {
				t.Fatal("transition invariant violated")
			}

			if before == "done" && next != "done" {
				t.Fatal("terminal state escaped")
			}

			current = next
		}
	})
}
