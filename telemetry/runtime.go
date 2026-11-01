package telemetry

import (
	"context"
	"errors"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

type Config struct {
	Enabled                                 bool
	Service, Version, Environment, Endpoint string
	SampleRatio                             float64
	TraceExporter                           sdktrace.SpanExporter
	MetricReader                            sdkmetric.Reader
}

type Runtime struct {
	Tracer trace.Tracer
	Meter  metric.Meter
	tp     *sdktrace.TracerProvider
	mp     *sdkmetric.MeterProvider
}

func FromEnv(prefix, service, version, environment string) (Config, error) {
	c := Config{
		Service:     service,
		Version:     version,
		Environment: environment,
		SampleRatio: 1,
	}

	key := strings.TrimSuffix(prefix, "_")
	if key != "" {
		key += "_"
	}

	var err error

	disabled := false

	if v, ok := os.LookupEnv("OTEL_SDK_DISABLED"); ok {
		parsed, e := strconv.ParseBool(v)
		if e != nil {
			return Config{}, errors.New("invalid SDK disabled setting")
		}

		disabled = parsed
	}

	if v, ok := os.LookupEnv(key + "OTEL_ENABLED"); ok {
		c.Enabled, err = strconv.ParseBool(v)
		if err != nil {
			return Config{}, errors.New("invalid telemetry enabled setting")
		}
	}

	c.Endpoint = os.Getenv(key + "OTEL_ENDPOINT")

	ratio, ok := os.LookupEnv(key + "OTEL_SAMPLE_RATIO")
	if !ok {
		ratio, ok = os.LookupEnv("OTEL_TRACES_SAMPLER_ARG")
	}

	if v := ratio; ok {
		c.SampleRatio, err = strconv.ParseFloat(v, 64)
		if err != nil || math.IsNaN(c.SampleRatio) ||
			math.IsInf(c.SampleRatio, 0) ||
			c.SampleRatio < 0 ||
			c.SampleRatio > 1 {
			return Config{}, errors.New("invalid telemetry sampling ratio")
		}
	}

	if c.Enabled && !disabled {
		switch os.Getenv("OTEL_TRACES_SAMPLER") {
		case "", "parentbased_traceidratio":
		default:
			return Config{}, errors.New(
				"unsupported telemetry sampler: use parentbased_traceidratio",
			)
		}
	}

	if disabled {
		c.Enabled = false
	}

	return c, nil
}

func New(ctx context.Context, c Config) (*Runtime, error) {
	r := &Runtime{
		Tracer: tracenoop.NewTracerProvider().Tracer("pluto"),
		Meter:  metricnoop.NewMeterProvider().Meter("pluto"),
	}
	if !c.Enabled {
		return r, nil
	}

	if math.IsNaN(c.SampleRatio) || math.IsInf(c.SampleRatio, 0) ||
		c.SampleRatio < 0 ||
		c.SampleRatio > 1 {
		return nil, errors.New("invalid telemetry sampling ratio")
	}

	res := resource.NewSchemaless(
		attribute.String("service.name", c.Service),
		attribute.String("service.version", c.Version),
		attribute.String("deployment.environment.name", c.Environment),
	)

	exp := c.TraceExporter
	if exp == nil {
		opts := []otlptracehttp.Option{}
		if c.Endpoint != "" {
			opts = append(
				opts,
				otlptracehttp.WithEndpointURL(
					signalEndpoint(c.Endpoint, "/v1/traces"),
				),
			)
		}

		var err error

		exp, err = otlptracehttp.New(ctx, opts...)
		if err != nil {
			return nil, errors.New("create trace exporter")
		}
	}

	reader := c.MetricReader
	if reader == nil {
		opts := []otlpmetrichttp.Option{}
		if c.Endpoint != "" {
			opts = append(
				opts,
				otlpmetrichttp.WithEndpointURL(
					signalEndpoint(c.Endpoint, "/v1/metrics"),
				),
			)
		}

		ex, err := otlpmetrichttp.New(ctx, opts...)
		if err != nil {
			_ = exp.Shutdown(ctx)
			return nil, errors.New("create metric exporter")
		}

		reader = sdkmetric.NewPeriodicReader(ex)
	}

	r.tp = sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSampler(
			sdktrace.ParentBased(sdktrace.TraceIDRatioBased(c.SampleRatio)),
		),
		sdktrace.WithBatcher(exp),
	)
	r.mp = sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithCardinalityLimit(400),
		sdkmetric.WithReader(reader),
	)
	r.Tracer = r.tp.Tracer("github.com/plutoframework/pluto-core/telemetry")
	r.Meter = r.mp.Meter("github.com/plutoframework/pluto-core/telemetry")

	return r, nil
}

func signalEndpoint(endpoint, signalPath string) string {
	u, err := url.Parse(endpoint)
	if err != nil || strings.TrimSpace(u.Path) != "" {
		return endpoint
	}

	u.Path = signalPath

	return u.String()
}

func (r *Runtime) Shutdown(ctx context.Context) error {
	if r == nil {
		return nil
	}

	var a, b error
	if r.tp != nil {
		a = r.tp.Shutdown(ctx)
	}

	if r.mp != nil {
		b = r.mp.Shutdown(ctx)
	}

	return errors.Join(a, b)
}

func (r *Runtime) Operation(
	ctx context.Context,
	component, operation string,
) (context.Context, func(error)) {
	if r == nil || r.Tracer == nil || r.Meter == nil {
		return ctx, func(error) {}
	}

	switch component {
	case "cache", "jobs", "postgres":
	default:
		component = "other"
	}

	switch operation {
	case "get", "set", "delete", "enqueue", "process", "query":
	default:
		operation = "other"
	}

	ctx, span := r.Tracer.Start(ctx, component+"."+operation)
	start := time.Now()
	histogram, _ := r.Meter.Float64Histogram(
		"pluto.operation.duration",
		metric.WithUnit("s"),
	)

	var once sync.Once

	return ctx, func(err error) {
		once.Do(func() {
			outcome := "success"
			if err != nil {
				outcome = "error"

				span.SetStatus(codes.Error, "operation failed")
			}

			attrs := []attribute.KeyValue{
				attribute.String("component", component),
				attribute.String("operation", operation),
				attribute.String("outcome", outcome),
			}
			span.SetAttributes(attrs...)
			histogram.Record(
				ctx,
				time.Since(start).Seconds(),
				metric.WithAttributes(attrs...),
			)
			span.End()
		})
	}
}
