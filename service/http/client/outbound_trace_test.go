// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/internal/telemetrytest"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// TestOutbound_TracedWithClientSpan verifies the pooled HTTP client wraps its
// transport with otelhttp, so an outbound request issued within a span produces
// a client span and injects traceparent into the outgoing headers.
func TestOutbound_TracedWithClientSpan(t *testing.T) {
	// otelhttp uses the global provider and propagator.
	tp, sr := telemetrytest.NewTracerProvider()
	prevTP := otel.GetTracerProvider()
	prevProp := otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
		_ = tp.Shutdown(context.Background())
	})

	var gotTraceparent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTraceparent = r.Header.Get("traceparent")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	pool := NewClientPool()
	cli := pool.GetClient(7*time.Second, "")

	tracer := tp.Tracer("test")
	ctx, parent := tracer.Start(context.Background(), "caller")
	defer parent.End()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	resp, err := cli.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()

	var clientSpan sdktrace.ReadOnlySpan
	for _, s := range sr.Ended() {
		if s.SpanKind() == trace.SpanKindClient {
			clientSpan = s
			break
		}
	}
	require.NotNil(t, clientSpan, "expected an outbound client span")
	assert.Equal(t, parent.SpanContext().TraceID(), telemetrytest.TraceID(clientSpan),
		"client span must continue the caller trace")
	assert.NotEmpty(t, gotTraceparent, "outgoing request must carry a traceparent header")
}
