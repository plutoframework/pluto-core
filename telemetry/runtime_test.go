package telemetry

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	collectormetric "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

func testRuntime(
	t *testing.T,
) (*Runtime, *tracetest.InMemoryExporter, *sdkmetric.ManualReader) {
	t.Helper()

	ex := tracetest.NewInMemoryExporter()
	reader := sdkmetric.NewManualReader()

	r, err := New(
		context.Background(),
		Config{
			Enabled:       true,
			SampleRatio:   1,
			Service:       "test",
			TraceExporter: ex,
			MetricReader:  reader,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = r.Shutdown(context.Background()) })

	return r, ex, reader
}

func TestOperationPrivacy(t *testing.T) {
	r, ex, reader := testRuntime(t)
	_, done := r.Operation(
		context.Background(),
		"secret-key",
		"private-payload",
	)

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() { done(errors.New("secret-error")) })
	}

	wg.Wait()

	if err := r.tp.ForceFlush(context.Background()); err != nil {
		t.Fatal(err)
	}

	spans := ex.GetSpans()
	if len(spans) != 1 || spans[0].Name != "other.other" ||
		spans[0].Status.Code != codes.Error {
		t.Fatalf("unexpected spans: %+v", spans)
	}

	var data metricdata.ResourceMetrics
	if err := reader.Collect(
		context.Background(),
		&data,
	); err != nil ||
		len(data.ScopeMetrics) == 0 {
		t.Fatalf("missing metrics: %v", err)
	}
}

func TestHTTPPrivacyAndPanic(t *testing.T) {
	r, ex, _ := testRuntime(t)
	router := chi.NewRouter()
	router.Use(r.Middleware)
	router.Get(
		"/items/{id}",
		func(w http.ResponseWriter, req *http.Request) { w.WriteHeader(204) },
	)
	router.Get(
		"/panic",
		func(http.ResponseWriter, *http.Request) { panic("private-panic") },
	)
	router.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest("GET", "/items/secret-id?token=secret", nil),
	)
	func() {
		defer func() {
			if recover() != "private-panic" {
				t.Error("panic not preserved")
			}
		}()

		router.ServeHTTP(
			httptest.NewRecorder(),
			httptest.NewRequest("GET", "/panic", nil),
		)
	}()

	_ = r.tp.ForceFlush(context.Background())

	spans := ex.GetSpans()
	if len(spans) != 2 || spans[0].Name != "GET /items/{id}" ||
		spans[1].Status.Code != codes.Error {
		t.Fatalf("unexpected spans %+v", spans)
	}

	for _, s := range spans {
		for _, a := range s.Attributes {
			if strings.Contains(a.Value.AsString(), "secret") {
				t.Fatal("sensitive attribute")
			}
		}
	}
}

func TestEnvValidation(t *testing.T) {
	for _, v := range []string{"NaN", "+Inf", "-1", "2", "bad"} {
		t.Setenv("APP_OTEL_SAMPLE_RATIO", v)

		if _, err := FromEnv("APP", "svc", "v", "test"); err == nil {
			t.Fatalf("accepted %s", v)
		}
	}
}

func TestOTLPHTTPExports(t *testing.T) {
	var mu sync.Mutex

	traces, metrics := 0, 0

	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			b, err := io.ReadAll(req.Body)
			if err != nil {
				t.Error(err)
			}

			mu.Lock()
			defer mu.Unlock()

			switch req.URL.Path {
			case "/v1/traces":
				var data collectortrace.ExportTraceServiceRequest
				if err := proto.Unmarshal(b, &data); err != nil {
					t.Error(err)
				}

				for _, res := range data.ResourceSpans {
					for _, scope := range res.ScopeSpans {
						traces += len(scope.Spans)
					}
				}
			case "/v1/metrics":
				var data collectormetric.ExportMetricsServiceRequest
				if err := proto.Unmarshal(b, &data); err != nil {
					t.Error(err)
				}

				for _, res := range data.ResourceMetrics {
					for _, scope := range res.ScopeMetrics {
						metrics += len(scope.Metrics)
					}
				}
			default:
				t.Errorf("unexpected path %s", req.URL.Path)
			}

			w.Header().Set("Content-Type", "application/x-protobuf")
			w.WriteHeader(200)
		}),
	)
	defer server.Close()

	r, err := New(
		context.Background(),
		Config{
			Enabled:     true,
			Service:     "smoke",
			SampleRatio: 1,
			Endpoint:    server.URL,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	_, done := r.Operation(context.Background(), "cache", "get")
	done(nil)

	if err := r.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()

	if traces != 1 || metrics < 1 {
		t.Fatalf("actual exports traces=%d metrics=%d", traces, metrics)
	}
}

func TestPropagationAndOutboundError(t *testing.T) {
	r, ex, _ := testRuntime(t)

	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.Header.Get("traceparent") == "" {
				t.Error("missing trace context")
			}

			if req.Header.Get("baggage") != "" {
				t.Error("unexpected baggage")
			}

			w.WriteHeader(503)
		}),
	)
	defer server.Close()

	req, _ := http.NewRequest("GET", server.URL+"/secret?token=private", nil)

	res, err := r.RoundTripper(nil).RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	if req.Header.Get("traceparent") != "" {
		t.Error("mutated original request")
	}

	_ = r.tp.ForceFlush(context.Background())

	if spans := ex.GetSpans(); len(spans) != 1 ||
		spans[0].Status.Code != codes.Error {
		t.Fatalf("unexpected spans %+v", spans)
	}
}

func TestCanceledHTTP(t *testing.T) {
	r, ex, _ := testRuntime(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/secret", nil).WithContext(ctx))

	_ = r.tp.ForceFlush(context.Background())

	spans := ex.GetSpans()
	if len(spans) != 1 || spans[0].Status.Code != codes.Error {
		t.Fatalf("unexpected spans %+v", spans)
	}
}

func TestDisabledAndEnvironment(t *testing.T) {
	t.Setenv("APP_OTEL_ENABLED", "true")
	t.Setenv("OTEL_SDK_DISABLED", "TRUE")

	c, err := FromEnv("APP", "test", "v1", "test")
	if err != nil || c.Enabled {
		t.Fatalf("disabled precedence %v %+v", err, c)
	}

	r, err := New(context.Background(), Config{})
	if err != nil {
		t.Fatal(err)
	}

	_, done := r.Operation(context.Background(), "cache", "get")
	done(nil)

	if err := r.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	var nilRuntime *Runtime

	_, done = nilRuntime.Operation(context.Background(), "cache", "get")
	done(nil)
	nilRuntime.PGXTracer().
		TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{})
}

func TestPanicPreservesWrittenStatus(t *testing.T) {
	runtime, exporter, _ := testRuntime(t)
	router := chi.NewRouter()
	router.Use(runtime.Middleware)
	router.Get(
		"/panic-after-write",
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			panic("private panic")
		},
	)
	func() {
		defer func() {
			if recover() != "private panic" {
				t.Error("panic changed")
			}
		}()

		router.ServeHTTP(
			httptest.NewRecorder(),
			httptest.NewRequest(http.MethodGet, "/panic-after-write", nil),
		)
	}()

	if err := runtime.tp.ForceFlush(context.Background()); err != nil {
		t.Fatal(err)
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 || spans[0].Status.Code != codes.Error {
		t.Fatal("missing panic error span")
	}

	for _, attr := range spans[0].Attributes {
		if string(attr.Key) == "http.response.status_code" &&
			attr.Value.AsInt64() != http.StatusAccepted {
			t.Fatal("changed observed response status")
		}
	}
}
