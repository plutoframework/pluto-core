// Package memory provides a bounded, expiring in-process LRU data cache.
package memory

import (
	"container/list"
	"context"
	"sync"
	"time"

	"github.com/plutoframework/pluto-core/cache"
)

type Config struct {
	Limits     cache.Limits
	MaxEntries int
	MaxBytes   int
	Now        func() time.Time
}

func DefaultConfig() Config {
	return Config{
		Limits:     cache.DefaultLimits(),
		MaxEntries: 1024,
		MaxBytes:   16 * 1024 * 1024,
		Now:        time.Now,
	}
}

type entry struct {
	key     string
	value   []byte
	expires time.Time
}

type Store struct {
	mu      sync.Mutex
	config  Config
	entries map[string]*list.Element
	lru     *list.List
	bytes   int
}

func New(config Config) (*Store, error) {
	if config.Limits.Validate() != nil || config.MaxEntries < 1 ||
		config.MaxEntries > 1000000 ||
		config.MaxBytes < 1 ||
		config.MaxBytes > 1024*1024*1024 ||
		config.Now == nil {
		return nil, cache.ErrInvalid
	}

	return &Store{
		config:  config,
		entries: map[string]*list.Element{},
		lru:     list.New(),
	}, nil
}

func (s *Store) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	if err := s.config.Limits.Key(key); err != nil {
		return nil, false, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	element, ok := s.entries[key]
	if !ok {
		return nil, false, nil
	}

	value := element.Value.(entry)
	if !s.config.Now().Before(value.expires) {
		s.remove(element)

		return nil, false, nil
	}

	s.lru.MoveToFront(element)

	return append([]byte{}, value.value...), true, nil
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

	if err := s.config.Limits.Write(key, value, ttl); err != nil {
		return err
	}

	size := len(key) + len(value)
	if size > s.config.MaxBytes {
		return cache.ErrInvalid
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	if old := s.entries[key]; old != nil {
		s.remove(old)
	}

	for len(s.entries) >= s.config.MaxEntries || s.bytes+size > s.config.MaxBytes {
		s.remove(s.lru.Back())
	}

	element := s.lru.PushFront(
		entry{
			key:     key,
			value:   append([]byte{}, value...),
			expires: s.config.Now().Add(ttl),
		},
	)
	s.entries[key], s.bytes = element, s.bytes+size

	return nil
}

func (s *Store) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := s.config.Limits.Key(key); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	if element := s.entries[key]; element != nil {
		s.remove(element)
	}

	return nil
}

func (s *Store) remove(element *list.Element) {
	value := element.Value.(entry)
	delete(s.entries, value.key)

	s.bytes -= len(value.key) + len(value.value)
	s.lru.Remove(element)
}
