package telemetry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/plutoframework/pluto-core/cache"

	jobs "github.com/plutoframework/pluto-jobs"
)

type failingCache struct{ err error }

func (s failingCache) Get(
	context.Context,
	string,
) ([]byte, bool, error) {
	return nil, false, s.err
}

func (s failingCache) Set(
	context.Context,
	string,
	[]byte,
	time.Duration,
) error {
	return s.err
}

func (s failingCache) Delete(
	context.Context,
	string,
) error {
	return s.err
}

type enqueueFunc func(context.Context, jobs.Request) error

func (f enqueueFunc) Enqueue(
	ctx context.Context,
	r jobs.Request,
) error {
	return f(ctx, r)
}

func TestAdaptersPreserveContracts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runtime, err := New(context.Background(), Config{})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = runtime.Shutdown(context.Background()) })

	sentinel := errors.New("secret failure")

	store := runtime.Cache(failingCache{err: sentinel})
	if _, found, err := store.Get(
		ctx,
		"private-key",
	); found ||
		err != sentinel {
		t.Fatal("get contract changed")
	}

	if err := store.Set(
		ctx,
		"private-key",
		[]byte("secret"),
		time.Second,
	); err != sentinel {
		t.Fatal("set contract changed")
	}

	if err := store.Delete(ctx, "private-key"); err != sentinel {
		t.Fatal("delete contract changed")
	}

	disabled := runtime.Cache(cache.Disabled{})
	if _, _, err := disabled.Get(
		ctx,
		"key",
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatal("cancellation lost")
	}

	job := jobs.Job{Request: jobs.Request{ID: "private-job-id"}}

	handler := runtime.JobHandler(
		func(callCtx context.Context, got jobs.Job) error {
			if callCtx.Err() != context.Canceled || got.ID != job.ID {
				t.Fatal("handler input changed")
			}

			return sentinel
		},
	)
	if handler(ctx, job) != sentinel {
		t.Fatal("handler error changed")
	}

	enqueuer := runtime.Enqueuer(
		enqueueFunc(func(callCtx context.Context, r jobs.Request) error {
			if callCtx.Err() != context.Canceled || r.ID != job.ID {
				t.Fatal("enqueue input changed")
			}

			return sentinel
		}),
	)
	if enqueuer.Enqueue(ctx, job.Request) != sentinel {
		t.Fatal("enqueue error changed")
	}
}

func TestJobHandlerPreservesPanic(t *testing.T) {
	runtime, err := New(context.Background(), Config{})
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if recover() != "private panic" {
			t.Fatal("panic changed")
		}
	}()

	handler := runtime.JobHandler(
		func(context.Context, jobs.Job) error { panic("private panic") },
	)
	_ = handler(context.Background(), jobs.Job{})
}
