package telemetry

import (
	"context"
	"errors"

	jobs "github.com/plutoframework/pluto-jobs"
)

// JobHandler starts one diagnostic operation for an execution attempt. Durable
// jobs intentionally start new traces: the outbox stores no incoming headers or
// baggage. Worker cancellation/deadlines and at-least-once semantics are retained.
// Instrument registrations, never payload-derived job kinds or identifiers.
func (r *Runtime) JobHandler(handler jobs.Handler) jobs.Handler {
	return func(ctx context.Context, job jobs.Job) (err error) {
		ctx, done := r.Operation(ctx, "jobs", "process")

		defer func() {
			if value := recover(); value != nil {
				done(errors.New("handler panic"))
				panic(value)
			}

			done(err)
		}()

		return handler(ctx, job)
	}
}

// Enqueuer instruments outbox writes in the caller's transaction/context. A
// successful enqueue span means persistence succeeded, not delivery or commit.
func (r *Runtime) Enqueuer(store jobs.Enqueuer) jobs.Enqueuer {
	return &enqueuer{runtime: r, store: store}
}

type enqueuer struct {
	runtime *Runtime
	store   jobs.Enqueuer
}

func (s *enqueuer) Enqueue(ctx context.Context, request jobs.Request) error {
	ctx, done := s.runtime.Operation(ctx, "jobs", "enqueue")
	err := s.store.Enqueue(ctx, request)
	done(err)

	return err
}
