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
