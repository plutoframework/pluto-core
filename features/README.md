# Feature flags

Use flags to merge staged application behavior while keeping it unavailable by
default. Keep permission and resource checks in the operation: a flag is never
authorization.

## Scaffold a gated endpoint

From an initialized Pluto project:

```sh
pluto --no-input add endpoint reports Export \
  --method GET \
  --path /api/reports/export \
  --name 'Export reports' \
  --description 'Export reports visible to the verified caller.' \
  --permission reports:export \
  --feature reports-export \
  --feature-client-visible
```

This writes route metadata, authored handler/model/operation files, a default-off
flag declaration and OpenAPI. Implement the operation and tests; the CLI does
not invent report data or grant anyone permission.

If the project prefix is `ORBIT_`, enable the declared flag at startup with:

```sh
ORBIT_FEATURE_REPORTS_EXPORT=true go run ./cmd/api
```

The default-off route is unavailable until enabled. The authenticated
`/api/features` endpoint exposes only client-visible declarations so the UI can
hide unavailable controls; route and operation checks remain authoritative.

## Evaluate override precedence in Go

Install the published Core module in a Go 1.25.5+ project:

```sh
go get github.com/plutoframework/pluto-core@v0.4.0
```

Save this complete program as `main.go`. It layers declarations, application
configuration and finally environment overrides. The application layer explicitly
enables export here to show how an environment value of false disables it again.
New CLI-generated declarations remain default-off.

<!-- example-source: examples/featureflags/main.go -->

```go
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
```

<!-- /example-source -->

Run it with the later override explicitly disabled:

```sh
ORBIT_FEATURE_REPORTS_EXPORT=false go run .
```

Expected output:

```text
export enabled: false
client flags: {"reports-export":false}
```

`export-diagnostics` is enabled internally by the application layer but absent
from the client projection. Remove the environment setting and reports export
uses the application's true override. Set it to `not-a-boolean` and startup
fails, naming the variable without printing its value.

## Gate the operation and expose capabilities

The generated route applies the flag gate. Other callers of the operation must
use the same evaluator before side effects. Keep the operation's permission,
tenant and resource checks even when the flag is true.

Only an authenticated capability endpoint should expose the client-visible
projection. An application evaluator can narrow results by the verified caller;
use `features.Visible(declarations, evaluator)` to project that evaluator instead
of exposing the raw global snapshot. The UI may hide controls, but cannot grant
access. The [purchasing application adapter](https://github.com/plutoframework/pluto-example/tree/main/internal/features)
shows this server-owned boundary.

## Test enabled, disabled and malformed configuration

From a Core checkout:

```sh
go test -race ./features ./examples/featureflags
```

The example tests preserve explicit false, exclude internal flags and reject
malformed startup values. Feature-specific operation tests should additionally
assert that disabled flags cause zero side effects and enabled flags still deny
unauthorized callers.

## Evaluation contract

Defaults apply first. Later layers override earlier layers only for present keys;
explicit false is preserved. Unknown override keys are ignored, and undeclared
flags evaluate false. Input declarations/maps are copied; Visible returns a fresh
map containing only ClientVisible declarations. NewSnapshot rejects empty keys,
duplicate keys and collisions in normalized environment names. MustSnapshot is an
explicit startup convenience for application-owned declarations already checked
by a generator; invalid declarations panic. Runtime values never cause that panic.

ReadEnvironment takes a caller-owned lookup and complete prefix, e.g.
APP_FEATURE_. It parses declared values with strconv.ParseBool, normalizes dots
and hyphens to underscores, and never includes raw values in parse errors. It does
not load environment variables globally or introduce runtime config file parsing.
Overrides converts optional bool pointers without losing unset versus false.

Flags are not authorization. Establish verified identity and tenant scope before
exposing Visible, and keep permission/resource checks in business operations even
when a flag is enabled. A custom application evaluator can implement Enabled or
use EvaluateFunc; the client-visible projection remains centralized here.
