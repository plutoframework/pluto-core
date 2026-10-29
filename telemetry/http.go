package telemetry

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func (r *Runtime) Middleware(next http.Handler) http.Handler {
	if r == nil || r.Tracer == nil {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := propagation.TraceContext{}.Extract(
			req.Context(),
			propagation.HeaderCarrier(req.Header),
		)
		ctx, span := r.Tracer.Start(
			ctx,
			"HTTP",
			trace.WithSpanKind(trace.SpanKindServer),
		)
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, req.ProtoMajor)

		defer func() {
			panicked := recover()

			route := "unknown"
			if rc := chi.RouteContext(
				ctx,
			); rc != nil &&
				rc.RoutePattern() != "" {
				route = rc.RoutePattern()
			}

			status := ww.Status()
			if status == 0 {
				status = http.StatusOK
				if panicked != nil {
					status = http.StatusInternalServerError
				}
			}

			method := boundedMethod(req.Method)
			attrs := []attribute.KeyValue{
				attribute.String("http.request.method", method),
				attribute.String("http.route", route),
				attribute.Int("http.response.status_code", status),
			}
			span.SetName(method + " " + route)
			span.SetAttributes(attrs...)

			if status >= 500 {
				span.SetStatus(codes.Error, "server error")
			}

			if panicked != nil {
				span.SetStatus(codes.Error, "handler panic")
			}

			if ctx.Err() != nil {
				span.SetStatus(codes.Error, "request canceled")
				span.SetAttributes(
					attribute.String("error.type", cancellationType(ctx.Err())),
				)
			}

			span.End()

			if r.Meter != nil {
				h, _ := r.Meter.Float64Histogram(
					"http.server.request.duration",
					metric.WithUnit("s"),
					metric.WithExplicitBucketBoundaries(
						.005,
						.01,
						.025,
						.05,
						.075,
						.1,
						.25,
						.5,
						.75,
						1,
						2.5,
						5,
						7.5,
						10,
					),
				)
				h.Record(
					ctx,
					time.Since(start).Seconds(),
					metric.WithAttributes(attrs...),
				)
			}

			if panicked != nil {
				panic(panicked)
			}
		}()

		next.ServeHTTP(ww, req.WithContext(ctx))
	})
}

func boundedMethod(s string) string {
	switch s {
	case "GET",
		"POST",
		"PUT",
		"PATCH",
		"DELETE",
		"HEAD",
		"OPTIONS",
		"CONNECT",
		"TRACE":
		return s
	default:
		return "_OTHER"
	}
}

type transport struct {
	runtime *Runtime
	base    http.RoundTripper
}

func (r *Runtime) RoundTripper(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}

	if r == nil || r.Tracer == nil {
		return base
	}

	return &transport{r, base}
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx, span := t.runtime.Tracer.Start(
		req.Context(),
		"HTTP "+boundedMethod(req.Method),
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer span.End()

	clone := req.Clone(ctx)
	clone.Header.Del("baggage")
	clone.Header.Del("traceparent")
	clone.Header.Del("tracestate")
	propagation.TraceContext{}.Inject(
		ctx,
		propagation.HeaderCarrier(clone.Header),
	)

	res, err := t.base.RoundTrip(clone)
	if err != nil {
		span.SetStatus(codes.Error, "request failed")
	}

	if res != nil && res.StatusCode >= 500 {
		span.SetStatus(codes.Error, "server error")
	}

	if res != nil {
		span.SetAttributes(
			attribute.Int("http.response.status_code", res.StatusCode),
		)
	}

	return res, err
}

func cancellationType(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}

	return "canceled"
}
