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

### 2. Page a scoped collection

This complete program follows two pages of synthetic purchases. Save it as
`cmd/page/main.go` and run `go run ./cmd/page`, or run the checked-in example with
`go run ./examples/pagination` from this repository.

<!-- example-source: examples/pagination/main.go -->

```go
// Pagination over synthetic, already ordered rows. No database is required.
package main

import (
	"fmt"
	"net/url"

	"github.com/plutoframework/pluto-core/pagination"
)

type purchase struct {
	ID     string
	Tenant string
}

func list(
	rows []purchase,
	verifiedTenant, cursor string,
) (pagination.Page[purchase], error) {
	query := url.Values{"limit": {"2"}}
	if cursor != "" {
		query.Set("cursor", cursor)
	}

	binding := pagination.Binding(query, "purchases:list", verifiedTenant)

	request, err := pagination.Parse(query, binding)
	if err != nil {
		return pagination.Page[purchase]{}, err
	}

	// A SQL repository uses the same tenant predicate, strict key boundary and
	// LIMIT limit+1. Cursor binding never supplies authorization.
	selected := []purchase{}

	for _, row := range rows {
		if row.Tenant != verifiedTenant || row.ID <= request.After.ID {
			continue
		}

		selected = append(selected, row)
		if len(selected) == request.FetchLimit() {
			break
		}
	}

	return pagination.Finish(
		request,
		selected,
		func(row purchase) pagination.Key {
			return pagination.Key{ID: row.ID}
		},
	)
}

func main() {
	rows := []purchase{
		{ID: "purchase-01", Tenant: "north"},
		{ID: "purchase-02", Tenant: "harbor"},
		{ID: "purchase-03", Tenant: "north"},
		{ID: "purchase-04", Tenant: "north"},
		{ID: "purchase-05", Tenant: "harbor"},
		{ID: "purchase-06", Tenant: "north"},
	}

	first, err := list(rows, "north", "")
	if err != nil {
		panic(err)
	}

	second, err := list(rows, "north", first.Page.NextCursor)
	if err != nil {
		panic(err)
	}

	fmt.Println("first:", first.Data[0].ID, first.Data[1].ID)
	fmt.Println("second:", second.Data[0].ID, second.Data[1].ID)
	fmt.Println("more:", second.Page.HasMore)
}
```

<!-- /example-source -->

Expected output:

```text
first: purchase-01 purchase-03
second: purchase-04 purchase-06
more: false
```

The dataset is ordered before paging. Each page reapplies the tenant predicate
and strict ID boundary, then fetches only `limit + 1` rows. The [tests](examples/pagination/main_test.go)
also reject a cursor replayed in another scope. A cursor never grants access.

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
