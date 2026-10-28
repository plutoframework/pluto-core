// Package valkey adapts the shared data-cache contract to standalone Valkey and
// compatible Redis servers. Applications own the client lifetime and deployment.
package valkey

import (
	"context"
	"crypto/tls"
	"net"
	"net/url"
	"time"

	"github.com/plutoframework/pluto-core/cache"

	client "github.com/valkey-io/valkey-go"
)

type Config struct {
	URL           string
	AllowInsecure bool
	Timeout       time.Duration
	Limits        cache.Limits
}

func DefaultConfig() Config {
	return Config{
		Timeout: 200 * time.Millisecond,
		Limits:  cache.DefaultLimits(),
	}
}

type Store struct {
	client     client.Client
	options    client.ClientOption
	gate       chan struct{}
	closed     bool
	retryAfter time.Time
	limits     cache.Limits
	timeout    time.Duration
}

// Open rejects URL query parameters (including TLS bypass options), keeps TLS
// verification enabled, and permits plaintext only on loopback or by explicit
// opt-in. Errors never contain credentials, URLs, keys, payloads or server text.
// No client-side caching or topology discovery is enabled in this MVP.
func New(config Config) (*Store, error) {
	if config.Limits.Validate() != nil || config.Timeout < time.Millisecond ||
		config.Timeout > 30*time.Second {
		return nil, cache.ErrInvalid
	}

	parsed, err := url.Parse(config.URL)
	if err != nil || parsed.Hostname() == "" || parsed.RawQuery != "" ||
		parsed.Fragment != "" ||
		(parsed.Scheme != "redis" && parsed.Scheme != "rediss") {
		return nil, cache.ErrInvalid
	}

	address := net.ParseIP(parsed.Hostname())

	loopback := parsed.Hostname() == "localhost" ||
		(address != nil && address.IsLoopback())
	if parsed.Scheme == "redis" && !loopback && !config.AllowInsecure {
		return nil, cache.ErrInvalid
	}

	options, err := client.ParseURL(config.URL)
	if err != nil {
		return nil, cache.ErrInvalid
	}

	if options.TLSConfig != nil {
		options.TLSConfig.MinVersion = tls.VersionTLS12
	}

	options.DisableCache = true
	options.ForceSingleClient = true
	options.DisableAutoPipelining = true
	options.DisableRetry = true
	options.Dialer.Timeout = config.Timeout
	options.ConnWriteTimeout = config.Timeout

	store := &Store{
		options: options,
		limits:  config.Limits,
		timeout: config.Timeout,
		gate:    make(chan struct{}, 1),
	}
	store.gate <- struct{}{}

	return store, nil
}

// Open also verifies initial connectivity. New validates without dialing so an
// optional data cache can recover after startup outages. Reconnect attempts are
// bounded and coalesced, with a one-second delay after failed initial dials.
func Open(config Config) (*Store, error) {
	store, err := New(config)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()

	if _, err := store.connection(ctx); err != nil {
		store.Close()

		return nil, err
	}

	return store, nil
}

func (s *Store) connection(ctx context.Context) (client.Client, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.gate:
	}

	defer func() { s.gate <- struct{}{} }()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if s.closed {
		return nil, cache.ErrUnavailable
	}

	if s.client != nil {
		return s.client, nil
	}

	if time.Now().Before(s.retryAfter) {
		return nil, cache.ErrUnavailable
	}

	options := s.options
	if deadline, ok := ctx.Deadline(); ok {
		options.Dialer.Timeout = min(
			options.Dialer.Timeout,
			time.Until(deadline),
		)
		options.ConnWriteTimeout = min(
			options.ConnWriteTimeout,
			options.Dialer.Timeout,
		)
	}

	connection, err := client.NewClient(options)
	if err != nil {
		s.retryAfter = time.Now().Add(time.Second)
		return nil, failure(ctx)
	}

	if err := ctx.Err(); err != nil {
		connection.Close()

		return nil, err
	}

	s.client = connection

	return connection, nil
}

func (s *Store) Close() {
	<-s.gate

	defer func() { s.gate <- struct{}{} }()

	s.closed = true
	if s.client != nil {
		s.client.Close()
	}
}

func (s *Store) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	if err := s.limits.Key(key); err != nil {
		return nil, false, err
	}

	operation, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	connection, err := s.connection(operation)
	if err != nil {
		return nil, false, failure(ctx)
	}

	value, err := connection.Do(operation, connection.B().Get().Key(key).Build()).
		AsBytes()
	if client.IsValkeyNil(err) {
		return nil, false, nil
	}

	if err != nil {
		return nil, false, failure(ctx)
	}

	if len(value) > s.limits.MaxValueBytes {
		return nil, false, cache.ErrInvalid
	}

	return append([]byte{}, value...), true, nil
}

func (s *Store) Set(
	ctx context.Context,
	key string,
	value []byte,
	ttl time.Duration,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := s.limits.Write(key, value, ttl); err != nil {
		return err
	}

	operation, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	connection, err := s.connection(operation)
	if err != nil {
		return failure(ctx)
	}

	milliseconds := int64((ttl + time.Millisecond - 1) / time.Millisecond)

	err = connection.Do(operation, connection.B().Set().Key(key).Value(string(value)).PxMilliseconds(milliseconds).Build()).
		Error()
	if err != nil {
		return failure(ctx)
	}

	return nil
}

func (s *Store) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := s.limits.Key(key); err != nil {
		return err
	}

	operation, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	connection, err := s.connection(operation)
	if err != nil {
		return failure(ctx)
	}

	if err := connection.Do(operation, connection.B().Del().Key(key).Build()).
		Error(); err != nil {
		return failure(ctx)
	}

	return nil
}

func failure(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	return cache.ErrUnavailable
}

var _ cache.Store = (*Store)(nil)
