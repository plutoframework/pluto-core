package main

import "testing"

func TestExplicitFalseAndClientProjection(t *testing.T) {
	snapshot, err := load(func(name string) (string, bool) {
		return "false", name == "ORBIT_FEATURE_REPORTS_EXPORT"
	})
	if err != nil {
		t.Fatal(err)
	}

	visible := snapshot.Visible()
	if snapshot.Enabled("reports-export") ||
		!snapshot.Enabled("export-diagnostics") ||
		len(visible) != 1 ||
		visible["reports-export"] {
		t.Fatal("lost explicit false or exposed an internal flag")
	}
}

func TestInvalidEnvironmentFailsStartup(t *testing.T) {
	_, err := load(func(string) (string, bool) {
		return "not-a-boolean", true
	})
	if err == nil {
		t.Fatal("invalid environment accepted")
	}
}
