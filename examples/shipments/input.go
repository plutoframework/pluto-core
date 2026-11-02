package shipments

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/plutoframework/pluto-core/idempotency"
)

var ErrInput = errors.New("invalid shipment operation input")

// DispatchInput is constructed once at the operation boundary. The normalized
// shipment ID and exact idempotency key cannot be changed by public assignment.
// The zero value is invalid; database and authorization checks remain separate.
type DispatchInput struct {
	id    string
	key   string
	valid bool
}

func NewDispatchInput(rawID, key string) (DispatchInput, error) {
	id := strings.TrimSpace(rawID)
	if id == "" || len(id) > 128 || !utf8.ValidString(id) ||
		!idempotency.ValidKey(key) {
		return DispatchInput{}, ErrInput
	}

	for _, r := range id {
		if unicode.IsControl(r) {
			return DispatchInput{}, ErrInput
		}
	}

	return DispatchInput{id: id, key: key, valid: true}, nil
}

func (i DispatchInput) Validate() error {
	if !i.valid {
		return ErrInput
	}

	return nil
}
func (i DispatchInput) ID() string  { return i.id }
func (i DispatchInput) Key() string { return i.key }

// ListInput and ExportInput carry bounded operation intent. Cursor validation
// and binding happen later with current trusted actor/tenant visibility facts.
type ListInput struct {
	limit  int
	cursor string
	valid  bool
}

func NewListInput(limit int, cursor string) (ListInput, error) {
	if limit < 1 || limit > 100 || len(cursor) > 2048 {
		return ListInput{}, ErrInput
	}

	return ListInput{limit: limit, cursor: cursor, valid: true}, nil
}

func (i ListInput) Validate() error {
	if !i.valid {
		return ErrInput
	}

	return nil
}
func (i ListInput) Limit() int     { return i.limit }
func (i ListInput) Cursor() string { return i.cursor }

const MaxExportRows = 1000

type ExportInput struct {
	rows  int
	valid bool
}

func NewExportInput(rows int) (ExportInput, error) {
	if rows < 1 || rows > MaxExportRows {
		return ExportInput{}, ErrInput
	}

	return ExportInput{rows: rows, valid: true}, nil
}

func (i ExportInput) Validate() error {
	if !i.valid {
		return ErrInput
	}

	return nil
}
func (i ExportInput) Rows() int { return i.rows }
