package httpidem

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/plutoframework/pluto-core/idempotency"
	"github.com/plutoframework/pluto-core/idempotency/memory"
)

func setup(t *testing.T) *Handler {
	t.Helper()

	store, _ := memory.New(100)

	handler, err := New(
		store,
		time.Hour,
		128,
		128,
		func(w http.ResponseWriter, _ *http.Request, err error) {
			http.Error(w, http.StatusText(Status(err)), Status(err))
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	return handler
}

func request(
	h *Handler,
	ctx context.Context,
	scope idempotency.Scope,
	key, body, path, content string,
	next http.Handler,
) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(body)).
		WithContext(ctx)
	r.Header.Set("Idempotency-Key", key)
	r.Header.Set("Content-Type", content)

	w := httptest.NewRecorder()
	h.Serve(w, r, scope, next)

	return w
}

func TestReplayFingerprintAndScope(t *testing.T) {
	h := setup(t)
	ctx := context.Background()
	scope := idempotency.Scope{
		Subject:   "alice",
		Tenant:    "one",
		Operation: "create",
	}

	var calls int

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Arbitrary", "secret")
		w.WriteHeader(201)

		_, _ = w.Write([]byte(`{"id":"one"}`))
	})
	for range 2 {
		w := request(
			h,
			ctx,
			scope,
			"same",
			`{}`,
			"/p?q=1",
			"application/json",
			next,
		)
		if w.Code != 201 || w.Body.String() != `{"id":"one"}` ||
			w.Header().Get("X-Arbitrary") != "" {
			t.Fatal(w)
		}
	}

	if calls != 1 {
		t.Fatal(calls)
	}

	for _, input := range [][3]string{{`{"v":1}`, "/p?q=1", "application/json"}, {`{}`, "/other?q=1", "application/json"}, {`{}`, "/p?q=2", "application/json"}, {`{}`, "/p?q=1", "text/plain"}} {
		if w := request(
			h,
			ctx,
			scope,
			"same",
			input[0],
			input[1],
			input[2],
			next,
		); w.Code != 409 {
			t.Fatal(w)
		}
	}

	scope.Subject = "bob"
	if w := request(
		h,
		ctx,
		scope,
		"same",
		`{}`,
		"/p?q=1",
		"application/json",
		next,
	); w.Code != 201 ||
		calls != 2 {
		t.Fatal(w, calls)
	}
}

func TestConcurrentHTTP(t *testing.T) {
	h := setup(t)
	scope := idempotency.Scope{Subject: "a", Operation: "write"}
	entered := make(chan struct{})
	release := make(chan struct{})

	var calls atomic.Int32

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(entered)
		<-release
		w.Header().Set("Content-Type", "application/json")

		_, _ = w.Write([]byte(`{}`))
	})

	var wg sync.WaitGroup

	wg.Add(1)

	go func() {
		defer wg.Done()

		if w := request(
			h,
			context.Background(),
			scope,
			"key",
			`{}`,
			"/p",
			"application/json",
			next,
		); w.Code != 200 {
			t.Error(w)
		}
	}()

	<-entered

	if w := request(
		h,
		context.Background(),
		scope,
		"key",
		`{}`,
		"/p",
		"application/json",
		next,
	); w.Code != 409 {
		t.Fatal(w)
	}

	close(release)
	wg.Wait()

	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
}

func TestUnsafeAndCancellation(t *testing.T) {
	scope := idempotency.Scope{Subject: "a", Operation: "write"}

	for _, mode := range []string{"cookie", "oversize", "cancel", "panic", "server-error"} {
		t.Run(mode, func(t *testing.T) {
			h := setup(t)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			calls := 0
			next := http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					calls++

					w.Header().Set("Content-Type", "application/json")

					switch mode {
					case "cookie":
						w.Header().Set("Set-Cookie", "secret")
					case "oversize":
						_, _ = w.Write([]byte(strings.Repeat("x", 129)))
					case "cancel":
						cancel()
					case "panic":
						panic("uncertain")
					case "server-error":
						w.WriteHeader(500)
					}

					_, _ = w.Write([]byte(`{}`))
				},
			)

			func() {
				defer func() { _ = recover() }()

				w := request(
					h,
					ctx,
					scope,
					"key",
					`{}`,
					"/p",
					"application/json",
					next,
				)
				if w.Code != 503 && w.Code != 413 &&
					(mode != "server-error" || w.Code != 500) {
					t.Error(w)
				}
			}()

			if w := request(
				h,
				context.Background(),
				scope,
				"key",
				`{}`,
				"/p",
				"application/json",
				next,
			); w.Code != 409 ||
				calls != 1 {
				t.Fatal(w, calls)
			}
		})
	}

	h := setup(t)

	next := http.HandlerFunc(
		func(http.ResponseWriter, *http.Request) { t.Fatal("executed invalid request") },
	)
	for _, key := range []string{"", "bad key", strings.Repeat("a", 129)} {
		if w := request(
			h,
			context.Background(),
			scope,
			key,
			`{}`,
			"/p",
			"application/json",
			next,
		); w.Code != 400 {
			t.Fatal(w)
		}
	}

	if w := request(
		h,
		context.Background(),
		scope,
		"key",
		strings.Repeat("a", 129),
		"/p",
		"application/json",
		next,
	); w.Code != 413 {
		t.Fatal(w)
	}

	if w := request(
		h,
		context.Background(),
		idempotency.Scope{Operation: "write"},
		"key",
		`{}`,
		"/p",
		"application/json",
		next,
	); w.Code != 400 {
		t.Fatal(w)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if w := request(
		h,
		ctx,
		scope,
		"key",
		`{}`,
		"/p",
		"application/json",
		next,
	); w.Code != 503 {
		t.Fatal(w)
	}
}

func FuzzKey(f *testing.F) {
	for _, key := range []string{"", "ok-key", "bad key", strings.Repeat("x", 129)} {
		f.Add(key)
	}

	f.Fuzz(func(t *testing.T, key string) {
		valid := idempotency.ValidKey(key)
		if len(key) > 128 && valid {
			t.Fatal("oversized key")
		}

		_, err := idempotency.NewClaim(
			idempotency.Scope{Subject: "a", Operation: "write"},
			key,
			strings.Repeat("a", 64),
			time.Now(),
			time.Hour,
		)
		if valid != (err == nil) && !errors.Is(err, context.Canceled) {
			t.Fatal(valid, err)
		}
	})
}

func TestMalformedFingerprintMetadataRejected(t *testing.T) {
	h := setup(t)
	scope := idempotency.Scope{Subject: "alice", Operation: "write"}
	next := http.HandlerFunc(
		func(http.ResponseWriter, *http.Request) { t.Fatal("executed ambiguous fingerprint") },
	)

	for _, field := range []string{"query", "content-type", "duplicate-content-type", "subject", "tenant", "operation"} {
		t.Run(field, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/p", strings.NewReader(`{}`))
			r.Header.Set("Idempotency-Key", "valid-key")
			r.Header.Set("Content-Type", "application/json")

			trusted := scope
			malformed := string([]byte{0xff})

			switch field {
			case "query":
				r.URL.RawQuery = malformed
			case "content-type":
				r.Header.Set("Content-Type", malformed)
			case "duplicate-content-type":
				r.Header.Add("Content-Type", "text/plain")
			case "subject":
				trusted.Subject = malformed
			case "tenant":
				trusted.Tenant = malformed
			case "operation":
				trusted.Operation = malformed
			}

			w := httptest.NewRecorder()
			h.Serve(w, r, trusted, next)

			if w.Code != 400 {
				t.Fatal(w)
			}
		})
	}
}
