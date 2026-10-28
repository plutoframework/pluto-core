package cache

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

type FailurePolicy uint8

const (
	Strict FailurePolicy = iota
	Bypass
)

type LoaderOptions struct {
	Limits        Limits
	FailurePolicy FailurePolicy
	MaxInFlight   int
	MaxWaiters    int
	LoadTimeout   time.Duration
	// OnError receives only fixed sentinels, never keys, payloads or raw errors.
	OnError func(error)
}

func DefaultLoaderOptions() LoaderOptions {
	return LoaderOptions{
		Limits: DefaultLimits(), FailurePolicy: Bypass,
		MaxInFlight: 128, MaxWaiters: 128, LoadTimeout: 5 * time.Second,
	}
}

type flight struct {
	done    chan struct{}
	data    []byte
	err     error
	waiters int
}

// Loader coalesces concurrent loads within this instance/process. The leader's
// context owns the synchronous load. Waiters can cancel independently; no
// detached goroutine hides an uncooperative loader. JSON values are copied.
type Loader[T any] struct {
	store   Store
	options LoaderOptions
	mu      sync.Mutex
	flights map[string]*flight
}

func NewLoader[T any](store Store, options LoaderOptions) (*Loader[T], error) {
	if store == nil || options.Limits.Validate() != nil ||
		options.FailurePolicy > Bypass || options.MaxInFlight < 1 || options.MaxInFlight > 4096 ||
		options.MaxWaiters < 1 || options.MaxWaiters > 4096 || options.LoadTimeout <= 0 || options.LoadTimeout > time.Minute {
		return nil, ErrInvalid
	}

	return &Loader[T]{
		store:   store,
		options: options,
		flights: map[string]*flight{},
	}, nil
}

func (l *Loader[T]) Remember(
	ctx context.Context,
	key string,
	ttl time.Duration,
	load func(context.Context) (T, error),
) (T, error) {
	var zero T
	if l == nil || load == nil {
		return zero, ErrInvalid
	}

	if err := ctx.Err(); err != nil {
		return zero, err
	}

	if err := l.options.Limits.Write(key, nil, ttl); err != nil {
		return zero, err
	}

	l.mu.Lock()

	if pending, ok := l.flights[key]; ok {
		if pending.waiters >= l.options.MaxWaiters {
			l.mu.Unlock()

			return zero, ErrBusy
		}

		pending.waiters++

		l.mu.Unlock()

		defer func() {
			l.mu.Lock()

			pending.waiters--
			l.mu.Unlock()
		}()

		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-pending.done:
			return decode[T](pending.data, pending.err)
		}
	}

	if len(l.flights) >= l.options.MaxInFlight {
		l.mu.Unlock()

		return zero, ErrBusy
	}

	pending := &flight{done: make(chan struct{})}
	l.flights[key] = pending
	l.mu.Unlock()

	// Always release followers, including if application loader/hook code panics.
	defer func() {
		l.mu.Lock()
		delete(l.flights, key)
		close(pending.done)
		l.mu.Unlock()
	}()

	pending.data, pending.err = l.resolve(ctx, key, ttl, load)

	return decode[T](pending.data, pending.err)
}

func decode[T any](data []byte, err error) (T, error) {
	var value T
	if err != nil {
		return value, err
	}

	if err := json.Unmarshal(data, &value); err != nil {
		return value, ErrEncoding
	}

	return value, nil
}

func (l *Loader[T]) resolve(
	ctx context.Context,
	key string,
	ttl time.Duration,
	load func(context.Context) (T, error),
) (data []byte, err error) {
	defer func() {
		if recover() != nil {
			data, err = nil, ErrLoader
		}
	}()

	data, found, err := l.store.Get(ctx, key)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err == nil && found {
		if len(data) <= l.options.Limits.MaxValueBytes {
			if _, err := decode[T](data, nil); err == nil {
				return data, nil
			}
		}

		err = ErrEncoding
	}

	if err != nil {
		if l.options.OnError != nil {
			l.options.OnError(ErrUnavailable)
		}

		if l.options.FailurePolicy == Strict {
			return nil, err
		}
	}

	loadContext, cancel := context.WithTimeout(ctx, l.options.LoadTimeout)
	defer cancel()

	value, err := load(loadContext)
	if err != nil {
		return nil, err
	}

	if err := loadContext.Err(); err != nil {
		return nil, err
	}

	data, err = json.Marshal(value)
	if err != nil {
		return nil, ErrEncoding
	}

	if len(data) > l.options.Limits.MaxValueBytes {
		return nil, ErrInvalid
	}

	if err := l.store.Set(ctx, key, data, ttl); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		if l.options.OnError != nil {
			l.options.OnError(ErrUnavailable)
		}

		if l.options.FailurePolicy == Strict {
			return nil, err
		}
	}

	return data, nil
}
