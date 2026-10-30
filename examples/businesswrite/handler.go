// Package businesswrite wraps an authorized business operation in JSON replay.
package businesswrite

import (
	"net/http"
	"time"

	"github.com/plutoframework/pluto-core/httpidem"
	"github.com/plutoframework/pluto-core/idempotency"
)

// Authorize checks current identity, permissions, flag and resource access.
// Its returned scope contains verified subject/tenant and a stable operation ID.
type Authorize func(*http.Request) (idempotency.Scope, error)

func New(
	store idempotency.Store,
	authorize Authorize,
	create http.Handler,
) (http.Handler, error) {
	if authorize == nil || create == nil {
		return nil, idempotency.ErrInvalid
	}

	replay, err := httpidem.New(
		store,
		24*time.Hour,
		64<<10,
		64<<10,
		func(w http.ResponseWriter, _ *http.Request, err error) {
			http.Error(w, "business request rejected", httpidem.Status(err))
		},
	)
	if err != nil {
		return nil, err
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope, err := authorize(r)
		if err != nil {
			http.Error(w, "access denied", http.StatusForbidden)

			return
		}

		// Authorization runs on every request, including completed replays.
		replay.Serve(w, r, scope, create)
	}), nil
}
