// Package httpidem captures bounded JSON business responses. Authentication,
// current permissions/features and trusted scope resolution MUST run first.
// Never wrap session, credential, token, streaming or sensitive response routes.
package httpidem

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/plutoframework/pluto-core/idempotency"
)

type WriteError func(http.ResponseWriter, *http.Request, error)

type Handler struct {
	store       idempotency.Store
	ttl         time.Duration
	maxBody     int64
	maxResponse int
	writeError  WriteError
}

func New(
	store idempotency.Store,
	ttl time.Duration,
	maxBody int64,
	maxResponse int,
	writeError WriteError,
) (*Handler, error) {
	if store == nil || ttl <= 0 || ttl > idempotency.MaxTTL || maxBody < 1 ||
		maxBody > idempotency.MaxPayload ||
		maxResponse < 1 ||
		maxResponse > idempotency.MaxPayload ||
		writeError == nil {
		return nil, idempotency.ErrInvalid
	}

	return &Handler{
		store:       store,
		ttl:         ttl,
		maxBody:     maxBody,
		maxResponse: maxResponse,
		writeError:  writeError,
	}, nil
}

// Serve executes once per owned claim. Cancellations, panics, unsupported/large
// responses and completion failures leave the claim pending, never runnable.
// Only Content-Type is persisted/replayed; outer security headers remain current.
func (h *Handler) Serve(
	w http.ResponseWriter,
	r *http.Request,
	scope idempotency.Scope,
	next http.Handler,
) {

	// JSON fingerprint framing requires valid UTF-8 metadata. The body itself
	// is hashed as exact bytes and need not be UTF-8.
	if !utf8.ValidString(r.Method) || !utf8.ValidString(r.URL.EscapedPath()) ||
		!utf8.ValidString(r.URL.RawQuery) ||
		!utf8.ValidString(r.Header.Get("Content-Type")) ||
		len(r.Header.Values("Content-Type")) > 1 {
		h.writeError(w, r, idempotency.ErrInvalid)

		return
	}

	keys := r.Header.Values("Idempotency-Key")
	if len(keys) != 1 || !idempotency.ValidKey(keys[0]) {
		h.writeError(w, r, idempotency.ErrInvalid)

		return
	}

	switch r.Method {
	case "POST", "PUT", "PATCH", "DELETE":
	default:
		h.writeError(w, r, idempotency.ErrInvalid)

		return
	}

	body := r.Body
	if body == nil {
		body = http.NoBody
	}

	raw, err := io.ReadAll(io.LimitReader(body, h.maxBody+1))
	_ = body.Close()

	if err != nil {
		h.writeError(w, r, err)

		return
	}

	if int64(len(raw)) > h.maxBody {
		h.writeError(w, r, idempotency.ErrOversize)

		return
	}

	if err := r.Context().Err(); err != nil {
		h.writeError(w, r, err)

		return
	}

	bodyHash := sha256.Sum256(raw)
	encoded, _ := json.Marshal(
		[]string{
			r.Method,
			r.URL.EscapedPath(),
			r.URL.RawQuery,
			r.Header.Get("Content-Type"),
			hex.EncodeToString(bodyHash[:]),
		},
	)
	sum := sha256.Sum256(encoded)

	claim, err := idempotency.NewClaim(
		scope,
		keys[0],
		hex.EncodeToString(sum[:]),
		time.Now(),
		h.ttl,
	)
	if err != nil {
		h.writeError(w, r, err)

		return
	}

	replay, err := h.store.Begin(r.Context(), claim)
	if err != nil {
		h.writeError(w, r, err)

		return
	}

	if replay != nil {
		send(w, *replay)

		return
	}

	if err := r.Context().Err(); err != nil {
		h.writeError(w, r, err)

		return
	}

	r.Body = io.NopCloser(bytes.NewReader(raw))
	capture := &capture{header: make(http.Header), limit: h.maxResponse}
	next.ServeHTTP(capture, r)

	if capture.err != nil {
		h.writeError(w, r, capture.err)

		return
	}

	// Reject sensitive/security headers, instead of persisting them. Ordinary app
	// headers are omitted; transport security headers belong outside this adapter.
	for _, name := range []string{"Set-Cookie", "Authorization", "Proxy-Authorization", "WWW-Authenticate", "Location", "Connection", "Transfer-Encoding", "Upgrade", "Trailer"} {
		if len(capture.header.Values(name)) > 0 {
			h.writeError(w, r, idempotency.ErrUnsupported)

			return
		}
	}

	response := idempotency.Response{
		Status:      capture.status,
		ContentType: capture.header.Get("Content-Type"),
		Body:        capture.body.Bytes(),
	}

	// Preserve application errors without recording them. The claim remains
	// pending: an error response does not prove that side effects rolled back.
	if response.ContentType == "application/json" &&
		(response.Status == 401 || response.Status == 403 || (response.Status >= 500 && response.Status <= 599)) {
		send(w, response)

		return
	}

	if err := response.Validate(); err != nil {
		h.writeError(w, r, err)

		return
	}

	if err := h.store.Complete(r.Context(), claim, response); err != nil {
		h.writeError(w, r, err)

		return
	}

	send(w, response)
}

func send(w http.ResponseWriter, response idempotency.Response) {
	w.Header().Set("Content-Type", response.ContentType)
	w.WriteHeader(response.Status)

	_, _ = w.Write(response.Body)
}

type capture struct {
	header http.Header
	status int
	body   bytes.Buffer
	limit  int
	err    error
}

func (c *capture) Header() http.Header { return c.header }
func (c *capture) WriteHeader(status int) {
	if c.status == 0 {
		c.status = status
	}
}

func (c *capture) Write(data []byte) (int, error) {
	if c.status == 0 {
		c.WriteHeader(http.StatusOK)
	}

	if c.err != nil {
		return 0, c.err
	}

	if len(data) > c.limit-c.body.Len() {
		c.err = idempotency.ErrOversize
		return 0, c.err
	}

	return c.body.Write(data)
}

// Status maps shared errors to a transport-neutral application error adapter.
func Status(err error) int {
	switch {
	case errors.Is(err, idempotency.ErrInvalid):
		return 400
	case errors.Is(err, idempotency.ErrConflict),
		errors.Is(err, idempotency.ErrInProgress):
		return 409
	case errors.Is(err, idempotency.ErrOversize):
		return 413
	default:
		return 503
	}
}
