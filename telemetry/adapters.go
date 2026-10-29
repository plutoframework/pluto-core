package telemetry

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/trace"
)

type (
	queryKey    struct{}
	queryTracer struct{ runtime *Runtime }
)

func (r *Runtime) PGXTracer() pgx.QueryTracer   { return queryTracer{r} }
func (r *Runtime) QueryTracer() pgx.QueryTracer { return r.PGXTracer() }

func (q queryTracer) TraceQueryStart(
	ctx context.Context,
	_ *pgx.Conn,
	_ pgx.TraceQueryStartData,
) context.Context {
	ctx, done := q.runtime.Operation(ctx, "postgres", "query")
	return context.WithValue(ctx, queryKey{}, done)
}

func (q queryTracer) TraceQueryEnd(
	ctx context.Context,
	_ *pgx.Conn,
	data pgx.TraceQueryEndData,
) {
	if done, ok := ctx.Value(queryKey{}).(func(error)); ok {
		done(data.Err)
	}
}

type logHandler struct{ base slog.Handler }

func (r *Runtime) Handler(
	base slog.Handler,
) slog.Handler {
	return logHandler{base}
}

func (h logHandler) Enabled(
	ctx context.Context,
	l slog.Level,
) bool {
	return h.base.Enabled(ctx, l)
}

func (h logHandler) Handle(ctx context.Context, record slog.Record) error {
	sc := trace.SpanContextFromContext(ctx)
	if sc.IsValid() {
		record = record.Clone()
		record.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}

	return h.base.Handle(ctx, record)
}

func (h logHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return logHandler{h.base.WithAttrs(attrs)}
}

func (h logHandler) WithGroup(
	name string,
) slog.Handler {
	return logHandler{h.base.WithGroup(name)}
}
