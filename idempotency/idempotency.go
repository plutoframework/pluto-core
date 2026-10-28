// Package idempotency defines owned, durable request claims. An unfinished claim
// is never automatically stolen: its side effects may already have committed.
package idempotency

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"
)

const (
	MaxKey     = 128
	MaxPayload = 1 << 20
	DefaultTTL = 24 * time.Hour
	MaxTTL     = 7 * 24 * time.Hour
)

var (
	ErrInvalid  = errors.New("invalid idempotency input")
	ErrConflict = errors.New(
		"idempotency key reused for a different request",
	)
	ErrInProgress  = errors.New("idempotency outcome is pending reconciliation")
	ErrOwnership   = errors.New("idempotency claim ownership lost")
	ErrCapacity    = errors.New("idempotency capacity exhausted")
	ErrOversize    = errors.New("idempotency payload too large")
	ErrUnsupported = errors.New("response cannot be replayed")
)

// Scope MUST come from verified identity and current trusted tenant resolution.
// Public routes need a separate explicit application policy; no anonymous scope.
type Scope struct {
	Subject   string
	Tenant    string
	Operation string
}

type Claim struct {
	ID          string
	Fingerprint string
	Owner       string
	ExpiresAt   time.Time
}

type Response struct {
	Status      int
	ContentType string
	Body        []byte
}

type Store interface {
	// Begin atomically inserts a claim, replays a completed matching claim, or
	// returns ErrConflict/ErrInProgress. Only expired COMPLETED claims may renew.
	Begin(context.Context, Claim) (*Response, error)
	// Complete must compare ID, owner and fingerprint. Errors never release claims.
	Complete(context.Context, Claim, Response) error
}

func ValidKey(key string) bool {
	if len(key) < 1 || len(key) > MaxKey {
		return false
	}

	for _, c := range key {
		allowed := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') ||
			c == '-' ||
			c == '_' ||
			c == '.' ||
			c == ':'
		if !allowed {
			return false
		}
	}

	return true
}

func NewClaim(
	scope Scope,
	key, fingerprint string,
	now time.Time,
	ttl time.Duration,
) (Claim, error) {
	if !utf8.ValidString(scope.Subject) || !utf8.ValidString(scope.Tenant) ||
		!utf8.ValidString(scope.Operation) ||
		scope.Subject == "" ||
		scope.Operation == "" ||
		len(scope.Subject) > 256 ||
		len(scope.Tenant) > 256 ||
		len(scope.Operation) > 256 ||
		!ValidKey(key) ||
		!digest(fingerprint) ||
		ttl <= 0 ||
		ttl > MaxTTL {
		return Claim{}, ErrInvalid
	}

	encoded, _ := json.Marshal(
		[]string{scope.Subject, scope.Tenant, scope.Operation, key},
	)
	id := sha256.Sum256(encoded)

	var owner [32]byte
	if _, err := rand.Read(owner[:]); err != nil {
		return Claim{}, err
	}

	return Claim{
		ID:          hex.EncodeToString(id[:]),
		Fingerprint: fingerprint,
		Owner:       hex.EncodeToString(owner[:]),
		ExpiresAt:   now.Add(ttl),
	}, nil
}

func digest(value string) bool {
	data, err := hex.DecodeString(value)
	return err == nil && len(data) == 32 && value == hex.EncodeToString(data)
}

func (c Claim) Validate() error {
	if !digest(c.ID) || !digest(c.Fingerprint) || !digest(c.Owner) ||
		c.ExpiresAt.IsZero() {
		return ErrInvalid
	}

	return nil
}

func (r Response) Validate() error {
	if len(r.Body) > MaxPayload {
		return ErrOversize
	}

	if r.Status < 200 || r.Status >= 500 || r.Status == 401 ||
		r.Status == 403 ||
		r.ContentType != "application/json" {
		return ErrUnsupported
	}

	return nil
}
