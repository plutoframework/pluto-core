# Optional telemetry

API: FromEnv(prefix, service, version, environment) (Config, error),
New(ctx, Config) (*Runtime, error), Runtime.Shutdown(ctx) error.
Runtime.Middleware is Chi middleware; RoundTripper(base) wraps an HTTP transport;
Handler(base) correlates slog; PGXTracer() (also QueryTracer()) returns pgx.QueryTracer.
Nil runtime instrumentation is safe. Disabled configuration uses noop providers.
Runtime.Tracer and Runtime.Meter expose the explicit OTEL APIs; no globals change.

Operation(ctx context.Context, component, operation string) (context.Context,
func(error)) is stable. Components cache/jobs/postgres; operations
get/set/delete/enqueue/process/query. Unknown values collapse to other. Finish is
idempotent and emits only success/error outcomes, never error text.

Config fields: Enabled, Service, Version, Environment, Endpoint, SampleRatio,
TraceExporter (sdktrace.SpanExporter), MetricReader (sdkmetric.Reader).
FromEnv reads PREFIX_OTEL_ENABLED (default false), PREFIX_OTEL_ENDPOINT,
PREFIX_OTEL_SAMPLE_RATIO (default 1). Explicit resource identity arguments win.
Standard exporter settings apply except explicit Endpoint overrides endpoints.
W3C tracecontext only; no baggage, raw paths, URLs, errors, SQL or parameters.

Official SDK/exporter documentation: https://opentelemetry.io/docs/languages/go/exporters/
This optional module pins patched OTEL v1.45 and requires Go 1.26 or newer.
Use a current patched Go toolchain. The core module remains Go 1.25-compatible.

Environment precedence: telemetry stays disabled until PREFIX_OTEL_ENABLED=true;
OTEL_SDK_DISABLED=true always disables it. Prefix sampling overrides
OTEL_TRACES_SAMPLER_ARG, then defaults to 1. Config.SampleRatio=0 intentionally
samples no root spans; parent-based sampling honors upstream decisions. Explicit
Config does not read application enable/sampling variables. The exporter SDK
reads standard OTLP endpoint/header/timeout/TLS settings; Config.Endpoint overrides
both signal endpoints. An endpoint without a path uses `/v1/traces` and
`/v1/metrics`; an explicit path is preserved for both signals. Resource identity is deliberately explicit: OTEL_SERVICE_NAME
and OTEL_RESOURCE_ATTRIBUTES are not read, avoiding accidental sensitive resource
attributes. Use the three constructor identity arguments instead.

Outbound spans end when RoundTrip returns headers, before the response body is
consumed. HTTP response bodies and transport errors are returned unchanged.
Register middleware before all routes. Dynamic routes must use Chi parameters;
unmatched paths become unknown. Route templates themselves are application-owned.
SDK metrics use a 400-series cardinality limit per instrument. Shutdown flushes
both providers; use a bounded context. No automatic log export is configured.
OTEL_TRACES_SAMPLER accepts empty or parentbased_traceidratio; other named samplers
are rejected when enabled. Set sampling ratio 0/1 for parent-based off/on behavior.
