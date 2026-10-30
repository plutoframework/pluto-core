<p align="center">
  <img src="https://raw.githubusercontent.com/plutoframework/pluto/main/assets/readme/banner.svg" alt="Pluto — a small framework, wide open possibilities" width="100%" />
</p>

# Pluto Core

Small Go primitives. Explicit application decisions.

[Website](https://plutoframework.github.io/pluto-site/) · [Documentation](https://plutoframework.github.io/pluto-site/docs/backend/core/) · [CLI demo](https://github.com/plutoframework/pluto/blob/main/docs/DEMO.md) · [First issues](https://github.com/plutoframework/pluto/blob/main/CONTRIBUTING.md#starter-tasks)

## What you get

- Bound pagination, own idempotency claims and keep caches non-authoritative.
- Evaluate typed business transitions and declared feature flags.
- Add PostgreSQL, Valkey or OpenTelemetry through separate modules.

## Get started

### 1. Install

From a Go **1.25.5+** module:

```sh
go get github.com/plutoframework/pluto-core@v0.4.0
```

### 2. Try a primitive

Save this as `cmd/page/main.go`, then run `go run ./cmd/page`:

```go
package main

import (
    "fmt"
    "net/url"

    "github.com/plutoframework/pluto-core/pagination"
)

func main() {
    query := url.Values{"limit": {"10"}}
    binding := pagination.Binding(query, "purchases:list", "verified-scope")
    request, err := pagination.Parse(query, binding)
    if err != nil {
        panic(err)
    }

    fmt.Println(request.Limit)
}
```

### 3. Connect the application boundary

Apply visibility predicates in your repository and fetch at most `limit + 1`
rows. Cursors are boundaries, never authorization. Compile state machines once;
operations still own authorization, guard facts and atomic persistence.

The root module is standard-library-only. Choose adapters explicitly; applications
own migrations and pool lifecycles. See the [transaction guide](docs/transactions.md).

## Read further

- [API and integration reference](docs/REFERENCE.md)
- [Field guide](https://plutoframework.github.io/pluto-site/docs/backend/core/)
- [Pluto ecosystem](https://github.com/plutoframework/pluto#the-ecosystem)

## Boundaries

Memory stores are development-only. Generic HTTP replay capture has a commit gap; use same-transaction completion when atomic business/replay storage is required.

## Contribute

Start with [CONTRIBUTING.md](CONTRIBUTING.md), then choose a [good first issue](https://github.com/plutoframework/pluto/blob/main/CONTRIBUTING.md#starter-tasks).

MIT licensed. See [LICENSE](LICENSE).
