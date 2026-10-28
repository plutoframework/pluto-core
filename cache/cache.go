// Package cache defines bounded, expiring data caches. It does not establish
// identity, grant permissions, provide locks or replace durable idempotency.
package cache

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type Store interface {
	Get(context.Context, string) ([]byte, bool, error)
	Set(context.Context, string, []byte, time.Duration) error
	Delete(context.Context, string) error
}

var (
	ErrInvalid     = errors.New("cache: invalid key, limits, value or TTL")
	ErrUnavailable = errors.New("cache: unavailable")
	ErrEncoding    = errors.New("cache: invalid JSON value")
	ErrBusy        = errors.New("cache: loader capacity reached")
	ErrLoader      = errors.New("cache: loader panicked")
)

type Limits struct {
	MaxKeyBytes   int
	MaxValueBytes int
	MaxTTL        time.Duration
}

func DefaultLimits() Limits {
	return Limits{
		MaxKeyBytes:   512,
		MaxValueBytes: 1024 * 1024,
		MaxTTL:        24 * time.Hour,
	}
}

func (l Limits) Validate() error {
	if l.MaxKeyBytes < 1 || l.MaxKeyBytes > 4096 || l.MaxValueBytes < 1 ||
		l.MaxValueBytes > 16*1024*1024 || l.MaxTTL < time.Millisecond || l.MaxTTL > 7*24*time.Hour {
		return ErrInvalid
	}

	return nil
}

func (l Limits) Key(key string) error {
	if err := l.Validate(); err != nil {
		return err
	}

	if key == "" || len(key) > l.MaxKeyBytes || strings.TrimSpace(key) != key ||
		!utf8.ValidString(key) {
		return ErrInvalid
	}

	for _, r := range key {
		if unicode.IsControl(r) {
			return ErrInvalid
		}
	}

	return nil
}

func (l Limits) Write(key string, value []byte, ttl time.Duration) error {
	if err := l.Key(key); err != nil {
		return err
	}

	if len(value) > l.MaxValueBytes || ttl < time.Millisecond ||
		ttl > l.MaxTTL {
		return ErrInvalid
	}

	return nil
}

// Disabled preserves input/cancellation checks while retaining nothing.
type Disabled struct{}

func (Disabled) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	return nil, false, DefaultLimits().Key(key)
}

func (Disabled) Set(
	ctx context.Context,
	key string,
	value []byte,
	ttl time.Duration,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	return DefaultLimits().Write(key, value, ttl)
}

func (Disabled) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	return DefaultLimits().Key(key)
}
