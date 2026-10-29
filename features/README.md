# Local feature flags

This stdlib-only package owns boolean evaluation and startup overrides. Applications
own declarations, verified identity/tenant context, permissions and HTTP responses.
It is a local snapshot provider, not OpenFeature/OFREP or a remote flag service.

```go
snapshot, err := features.NewSnapshot(declarations, fileOverrides, envOverrides)
```

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
