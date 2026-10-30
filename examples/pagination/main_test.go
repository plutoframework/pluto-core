package main

import (
	"errors"
	"reflect"
	"testing"

	"github.com/plutoframework/pluto-core/pagination"
)

func TestPagesPreserveVisibilityAndBoundaries(t *testing.T) {
	rows := []purchase{
		{ID: "01", Tenant: "north"},
		{ID: "02", Tenant: "harbor"},
		{ID: "03", Tenant: "north"},
		{ID: "04", Tenant: "north"},
		{ID: "05", Tenant: "north"},
	}

	first, err := list(rows, "north", "")
	if err != nil || len(first.Data) != 2 || !first.Page.HasMore {
		t.Fatalf("first page: %#v, %v", first, err)
	}

	second, err := list(rows, "north", first.Page.NextCursor)
	if err != nil || second.Page.HasMore || second.Page.NextCursor != "" {
		t.Fatalf("last page: %#v, %v", second, err)
	}

	ids := []string{
		first.Data[0].ID,
		first.Data[1].ID,
		second.Data[0].ID,
		second.Data[1].ID,
	}
	if !reflect.DeepEqual(ids, []string{"01", "03", "04", "05"}) {
		t.Fatalf("visible IDs: %v", ids)
	}

	if _, err := list(
		rows,
		"harbor",
		first.Page.NextCursor,
	); !errors.Is(
		err,
		pagination.ErrInvalid,
	) {
		t.Fatalf("cross-scope cursor: %v", err)
	}
}

func TestEmptyPage(t *testing.T) {
	page, err := list(nil, "north", "")
	if err != nil || page.Data == nil || len(page.Data) != 0 ||
		page.Page.HasMore {
		t.Fatalf("empty page: %#v, %v", page, err)
	}
}
