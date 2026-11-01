package shipments_test

import (
	"errors"
	"testing"

	"github.com/plutoframework/pluto-core/examples/shipments"
)

func TestNormalizedPrivateInputs(t *testing.T) {
	command, err := shipments.NewDispatchInput(" shipment-1 ", "request-1")
	if err != nil {
		t.Fatal(err)
	}

	if command.ID() != "shipment-1" || command.Key() != "request-1" ||
		command.Validate() != nil {
		t.Fatal("normalization lost")
	}

	for _, id := range []string{"", " \t ", "shipment\x00id"} {
		if _, err := shipments.NewDispatchInput(
			id,
			"key",
		); !errors.Is(
			err,
			shipments.ErrInput,
		) {
			t.Fatalf("ID %q: %v", id, err)
		}
	}

	if _, err := shipments.NewDispatchInput(
		"shipment-1",
		" key ",
	); !errors.Is(
		err,
		shipments.ErrInput,
	) {
		t.Fatal("idempotency key was silently normalized")
	}

	if err := (shipments.DispatchInput{}).Validate(); !errors.Is(
		err,
		shipments.ErrInput,
	) {
		t.Fatal("zero dispatch valid")
	}

	if err := (shipments.ListInput{}).Validate(); !errors.Is(
		err,
		shipments.ErrInput,
	) {
		t.Fatal("zero list valid")
	}

	if err := (shipments.ExportInput{}).Validate(); !errors.Is(
		err,
		shipments.ErrInput,
	) {
		t.Fatal("zero export valid")
	}

	for _, limit := range []int{-1, 0, 101} {
		if _, err := shipments.NewListInput(
			limit,
			"",
		); !errors.Is(
			err,
			shipments.ErrInput,
		) {
			t.Fatalf("page bound %d", limit)
		}
	}

	for _, rows := range []int{-1, 0, shipments.MaxExportRows + 1} {
		if _, err := shipments.NewExportInput(
			rows,
		); !errors.Is(
			err,
			shipments.ErrInput,
		) {
			t.Fatalf("export bound %d", rows)
		}
	}
}
