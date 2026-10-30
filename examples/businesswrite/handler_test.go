package businesswrite

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/plutoframework/pluto-core/idempotency"
	"github.com/plutoframework/pluto-core/idempotency/memory"
)

func TestReplayRequiresCurrentAuthorization(t *testing.T) {
	store, err := memory.New(10)
	if err != nil {
		t.Fatal(err)
	}

	allowed := true
	writes := 0
	checks := 0

	handler, err := New(
		store,
		func(*http.Request) (idempotency.Scope, error) {
			checks++

			if !allowed {
				return idempotency.Scope{}, errors.New("revoked")
			}

			return idempotency.Scope{
				Subject: "reader", Tenant: "north", Operation: "orders:create",
			}, nil
		},
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writes++

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)

			_, _ = w.Write([]byte(`{"data":{"id":"42"}}`))
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	request := func(body string) int {
		r := httptest.NewRequest(
			http.MethodPost,
			"/api/orders",
			strings.NewReader(body),
		)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "order-42")

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		return w.Code
	}

	first := request(`{"amount":42}`)
	replay := request(`{"amount":42}`)
	conflict := request(`{"amount":43}`)

	if first != 201 || replay != 201 || conflict != 409 || writes != 1 {
		t.Fatal("replay or conflict executed another write")
	}

	allowed = false

	if request(`{"amount":42}`) != 403 || checks != 4 || writes != 1 {
		t.Fatal("replay bypassed current authorization")
	}
}
