package cache

import (
	"context"
	"encoding/json"
	"time"
)

func GetJSON[T any](
	ctx context.Context,
	store Store,
	key string,
) (T, bool, error) {
	var value T
	if store == nil {
		return value, false, ErrInvalid
	}

	data, found, err := store.Get(ctx, key)
	if err != nil || !found {
		return value, false, err
	}

	if err := json.Unmarshal(data, &value); err != nil {
		return value, false, ErrEncoding
	}

	return value, true, nil
}

func SetJSON[T any](
	ctx context.Context,
	store Store,
	key string,
	value T,
	ttl time.Duration,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if store == nil {
		return ErrInvalid
	}

	data, err := json.Marshal(value)
	if err != nil {
		return ErrEncoding
	}

	return store.Set(ctx, key, data, ttl)
}
