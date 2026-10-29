package features

import (
	"strings"
	"testing"
)

func TestSnapshotLayersAndExposure(t *testing.T) {
	definitions := []Definition{
		{Key: "export", Default: true, ClientVisible: true},
		{Key: "diagnostics", Default: true},
	}
	file := map[Key]bool{"export": true}
	env := map[Key]bool{"export": false, "unknown": true}

	snapshot, err := NewSnapshot(definitions, file, env)
	if err != nil {
		t.Fatal(err)
	}

	env["export"] = true
	definitions[0].ClientVisible = false

	if snapshot.Enabled("export") || snapshot.Enabled("unknown") ||
		!snapshot.Enabled("diagnostics") {
		t.Fatal("lost override presence or declaration boundary")
	}

	visible := snapshot.Visible()
	if len(visible) != 1 || visible["export"] {
		t.Fatal("leaked internal flag or wrong value")
	}

	visible["export"] = true

	if snapshot.Visible()["export"] {
		t.Fatal("aliased output map")
	}

	var empty *Snapshot
	if empty.Enabled("export") || len(empty.Visible()) != 0 {
		t.Fatal("nil snapshot enabled flags")
	}
}

func TestEnvironmentPresenceAndPrivacy(t *testing.T) {
	declarations := []Definition{{Key: "purchase.export", Default: true}}

	values, err := ReadEnvironment(
		declarations,
		"APP_FEATURE_",
		func(key string) (string, bool) { return "false", key == "APP_FEATURE_PURCHASE_EXPORT" },
	)
	if err != nil || values["purchase.export"] || len(values) != 1 {
		t.Fatal("explicit false not preserved")
	}

	secret := "private-invalid-value"

	_, err = ReadEnvironment(
		declarations,
		"APP_FEATURE_",
		func(string) (string, bool) { return secret, true },
	)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatal("invalid value accepted or disclosed")
	}

	for _, definitions := range [][]Definition{
		{{Key: ""}}, {{Key: "same"}, {Key: "same"}}, {{Key: "a.b"}, {Key: "a-b"}},
	} {
		if _, err := NewSnapshot(definitions); err == nil {
			t.Fatal("invalid declarations accepted")
		}
	}

	disabled := false

	overrides := Overrides(map[Key]*bool{"export": &disabled, "absent": nil})
	if len(overrides) != 1 || overrides["export"] {
		t.Fatal("pointer presence lost")
	}
}
