// Evaluate declared flags with application and environment overrides.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/plutoframework/pluto-core/features"
)

var declarations = []features.Definition{
	{
		Key:           "reports-export",
		Name:          "Export reports",
		Description:   "Download reports visible to the current caller.",
		Default:       false,
		ClientVisible: true,
	},
	{
		Key:     "export-diagnostics",
		Name:    "Export diagnostics",
		Default: false,
	},
}

func load(lookup func(string) (string, bool)) (*features.Snapshot, error) {
	environment, err := features.ReadEnvironment(
		declarations,
		"ORBIT_FEATURE_",
		lookup,
	)
	if err != nil {
		return nil, err
	}

	// This layer comes from application configuration, not a Core file loader.
	application := map[features.Key]bool{
		"reports-export":     true,
		"export-diagnostics": true,
	}

	return features.NewSnapshot(declarations, application, environment)
}

func run() error {
	snapshot, err := load(os.LookupEnv)
	if err != nil {
		return err
	}

	fmt.Println("export enabled:", snapshot.Enabled("reports-export"))

	// An authenticated capability endpoint may project these declared values.
	// Its caller must still apply any identity/tenant-aware evaluation.
	visible, err := json.Marshal(snapshot.Visible())
	if err != nil {
		return err
	}

	fmt.Println("client flags:", string(visible))

	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
