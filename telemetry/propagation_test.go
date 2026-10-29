package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/trace"
)

func TestHTTPPropagation(t *testing.T) {
	runtime, exporter, _ := testRuntime(t)

	const incoming = "00-11111111111111111111111111111111-2222222222222222-01"

	router := chi.NewRouter()
	router.Use(runtime.Middleware)
	router.Get("/owned/{id}", func(w http.ResponseWriter, req *http.Request) {
		if !trace.SpanContextFromContext(req.Context()).IsValid() {
			t.Error("missing server span")
		}

		w.WriteHeader(http.StatusNoContent)
	})

	server := httptest.NewServer(router)
	defer server.Close()

	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		server.URL+"/owned/private",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	request.Header.Set("traceparent", incoming)

	response, err := (&http.Client{Transport: runtime.RoundTripper(nil)}).Do(
		request,
	)
	if err != nil {
		t.Fatal(err)
	}

	_ = response.Body.Close()

	if request.Header.Get("traceparent") != incoming {
		t.Fatal("transport mutated caller headers")
	}

	if err := runtime.tp.ForceFlush(context.Background()); err != nil {
		t.Fatal(err)
	}

	spans := exporter.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("expected client/server spans, got %d", len(spans))
	}

	var (
		client, serverSpan trace.SpanContext
		parent             trace.SpanContext
	)

	for _, span := range spans {
		if span.SpanKind == trace.SpanKindClient {
			client = span.SpanContext
		}

		if span.SpanKind == trace.SpanKindServer {
			serverSpan = span.SpanContext
			parent = span.Parent
		}
	}

	if !client.IsValid() || client.TraceID() != serverSpan.TraceID() ||
		parent.SpanID() != client.SpanID() {
		t.Fatal("outbound propagation did not connect server to client")
	}
}
