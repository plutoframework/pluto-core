package cache

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"
)

func TestWaiterBoundAndCancellation(t *testing.T) {
	options := DefaultLoaderOptions()
	options.MaxWaiters = 1
	loader, _ := NewLoader[string](Disabled{}, options)
	started, release := make(chan struct{}), make(chan struct{})
	leader := make(chan error, 1)

	go func() {
		_, err := loader.Remember(
			context.Background(),
			"key",
			time.Minute,
			func(context.Context) (string, error) {
				close(started)
				<-release

				return "value", nil
			},
		)
		leader <- err
	}()

	<-started

	ctx, cancel := context.WithCancel(context.Background())
	waiter := make(chan error, 1)

	go func() {
		_, err := loader.Remember(
			ctx,
			"key",
			time.Minute,
			func(context.Context) (string, error) {
				t.Error("follower loaded")

				return "", nil
			},
		)
		waiter <- err
	}()

	deadline := time.Now().Add(time.Second)

	for {
		loader.mu.Lock()

		count := loader.flights["key"].waiters
		loader.mu.Unlock()

		if count == 1 {
			break
		}

		if time.Now().After(deadline) {
			cancel()
			close(release)
			t.Fatal("follower not registered")
		}

		runtime.Gosched()
	}

	bounded, stop := context.WithTimeout(
		context.Background(),
		100*time.Millisecond,
	)
	defer stop()

	_, err := loader.Remember(
		bounded,
		"key",
		time.Minute,
		func(context.Context) (string, error) { return "", nil },
	)
	if !errors.Is(err, ErrBusy) {
		t.Fatal("waiter limit ignored", err)
	}

	cancel()

	if err := <-waiter; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}

	close(release)

	if err := <-leader; err != nil {
		t.Fatal(err)
	}
}
