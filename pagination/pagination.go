// Package pagination provides bounded forward keyset pages. Cursors are opaque
// versioned encodings, NOT signatures or authorization. Reapply visibility in SQL.
package pagination

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strconv"
)

const (
	DefaultLimit = 25
	MaxLimit     = 100
	MaxCursor    = 2048
)

var ErrInvalid = errors.New("invalid pagination")

// Key is a deterministic ascending sort value with a unique ID tie-breaker.
// For ID-only ordering use an empty Sort. Sort representation belongs to the app.
type Key struct {
	Sort string `json:"sort"`
	ID   string `json:"id"`
}

type Request struct {
	Limit   int
	After   Key
	binding string
}

type Metadata struct {
	HasMore    bool   `json:"has_more"`
	NextCursor string `json:"next_cursor"`
}

type Page[T any] struct {
	Data []T      `json:"data"`
	Page Metadata `json:"page"`
}

type cursor struct {
	Version int    `json:"v"`
	Binding string `json:"b"`
	Key     Key    `json:"k"`
}

// Binding includes the stable operation, VERIFIED actor/tenant/visibility policy
// and canonical filters. Changing a limit does not change the result set.
func Binding(query url.Values, scope ...string) string {
	filters := url.Values{}

	for k, v := range query {
		if k != "limit" && k != "cursor" {
			filters[k] = v
		}
	}

	data, _ := json.Marshal(struct {
		Scope   []string
		Filters string
	}{scope, filters.Encode()})
	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:])
}

func Parse(query url.Values, binding string) (Request, error) {
	request := Request{Limit: DefaultLimit, binding: binding}
	if len(binding) != 64 {
		return Request{}, ErrInvalid
	}

	if values, ok := query["limit"]; ok {
		if len(values) != 1 || len(values[0]) == 0 || len(values[0]) > 3 {
			return Request{}, ErrInvalid
		}

		for _, c := range values[0] {
			if c < '0' || c > '9' {
				return Request{}, ErrInvalid
			}
		}

		limit, err := strconv.Atoi(values[0])
		if err != nil || limit < 1 || limit > MaxLimit {
			return Request{}, ErrInvalid
		}

		request.Limit = limit
	}

	if values, ok := query["cursor"]; ok {
		if len(values) != 1 || len(values[0]) == 0 ||
			len(values[0]) > MaxCursor {
			return Request{}, ErrInvalid
		}

		raw, err := base64.RawURLEncoding.Strict().DecodeString(values[0])
		if err != nil {
			return Request{}, ErrInvalid
		}

		var value cursor

		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()

		if err := decoder.Decode(&value); err != nil {
			return Request{}, ErrInvalid
		}

		if decoder.Decode(new(any)) != io.EOF || value.Version != 1 ||
			value.Binding != binding ||
			!validKey(value.Key) {
			return Request{}, ErrInvalid
		}

		request.After = value.Key
	}

	return request, nil
}

func validKey(
	key Key,
) bool {
	return key.ID != "" && len(key.ID) <= 256 && len(key.Sort) <= 256
}

// Validate also protects repositories called without the HTTP parser.
func (r Request) Validate() error {
	if r.Limit < 1 || r.Limit > MaxLimit ||
		(r.After != (Key{}) && !validKey(r.After)) {
		return ErrInvalid
	}

	return nil
}

func (r Request) FetchLimit() int { return r.Limit + 1 }

// Finish consumes at most limit+1 ALREADY ordered/filtered rows from a bounded
// repository query. It never sorts, filters, counts or fetches the collection.
func Finish[T any](
	request Request,
	rows []T,
	key func(T) Key,
) (Page[T], error) {
	if request.Validate() != nil || len(request.binding) != 64 ||
		len(rows) > request.FetchLimit() {
		return Page[T]{}, ErrInvalid
	}

	more := len(rows) > request.Limit
	if more {
		rows = rows[:request.Limit]
	}

	if rows == nil {
		rows = []T{}
	}

	page := Page[T]{Data: rows, Page: Metadata{HasMore: more}}
	if more {
		last := key(rows[len(rows)-1])
		if !validKey(last) {
			return Page[T]{}, ErrInvalid
		}

		data, err := json.Marshal(
			cursor{Version: 1, Binding: request.binding, Key: last},
		)
		if err != nil {
			return Page[T]{}, err
		}

		encoded := base64.RawURLEncoding.EncodeToString(data)
		if len(encoded) > MaxCursor {
			return Page[T]{}, ErrInvalid
		}

		page.Page.NextCursor = encoded
	}

	return page, nil
}
