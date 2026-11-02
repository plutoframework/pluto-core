package state_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// A separate module proves package privacy through the actual consumer
// compiler.
// A positive control ensures missing imports/toolchain problems cannot count as
// rejection of a forged token.
func TestEvaluatedCannotBeMintedByConsumer(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}

	consumer := t.TempDir()

	module := "module consumer\n\ngo " +
		"1.25.5\n\nrequire " +
		"github.com/plutoframework/pluto-cor" +
		"e v0.0.0\n\nreplace " +
		"github.com/plutoframework/pluto-cor" +
		"e => " + strconv.Quote(
		root,
	) + "\n"
	if err := os.WriteFile(
		filepath.Join(consumer, "go.mod"),
		[]byte(module),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(consumer, "consumer_test.go")
	run := func(source string) ([]byte, error) {
		t.Helper()

		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}

		command := exec.CommandContext(t.Context(), "go", "test", ".")
		command.Dir = consumer

		command.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off")

		return command.CombinedOutput()
	}

	prefix := "package consumer\nimport " +
		"\"github.com/plutoframework/pluto-co" +
		"re/state\"\n"
	if output, err := run(
		prefix + "var _ = state.Evaluated[string,string]{}\n",
	); err != nil {
		t.Fatalf("positive control: %v\n%s", err, output)
	}

	output, err := run(
		prefix + "var _ = state.Evaluated[string,string]{valid:true}\n",
	)
	if err == nil {
		t.Fatal("consumer minted evaluated transition")
	}

	if !strings.Contains(
		string(output),
		"cannot refer to unexported field valid",
	) {
		t.Fatalf("unexpected compiler failure: %v\n%s", err, output)
	}
}
