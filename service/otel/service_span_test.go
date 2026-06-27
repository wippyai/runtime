// SPDX-License-Identifier: MPL-2.0

package otel

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	attrsapi "github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	httpapi "github.com/wippyai/runtime/api/service/http"
	otelapi "github.com/wippyai/runtime/api/service/otel"
	"github.com/wippyai/runtime/internal/telemetrytest"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// useTraceContextPropagator installs the W3C TraceContext+Baggage propagator
// as the global for the duration of a test, restoring the previous value after.
func useTraceContextPropagator(t *testing.T) {
	t.Helper()
	prev := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	t.Cleanup(func() { otel.SetTextMapPropagator(prev) })
}

func TestHTTPMiddleware_ServerSpan(t *testing.T) {
	useTraceContextPropagator(t)
	tp, sr := telemetrytest.NewTracerProvider()
	defer func() { _ = tp.Shutdown(context.Background()) }()

	svc := NewService(otelapi.Config{HTTP: otelapi.HTTPConfig{Enabled: true, ExtractHeaders: true, InjectHeaders: true}}, zap.NewNop(), tp)
	wrapped := svc.HTTPMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/users/123", nil)
	fcCtx, fc := ctxapi.OpenFrameContext(req.Context())
	defer ctxapi.ReleaseFrameContext(fc)
	require.NoError(t, httpapi.SetRouteLabel(fcCtx, "/users/:id"))
	req = req.WithContext(fcCtx)

	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	telemetrytest.SpanCount(t, sr, 1)
	span := telemetrytest.MustSpanNamed(t, sr, "GET /users/:id")
	telemetrytest.SpanKind(t, span, trace.SpanKindServer)
	telemetrytest.SpanHasStringAttr(t, span, "http.method", "GET")
	telemetrytest.SpanHasStringAttr(t, span, "http.route", "/users/:id")
	assert.NotEmpty(t, rec.Header().Get("traceparent"), "traceparent must be injected into response headers")
}

func TestHTTPMiddleware_ExtractsParent(t *testing.T) {
	useTraceContextPropagator(t)
	tp, sr := telemetrytest.NewTracerProvider()
	defer func() { _ = tp.Shutdown(context.Background()) }()

	tracer := tp.Tracer("test")
	parentCtx, parent := tracer.Start(context.Background(), "remote.parent")
	defer parent.End()

	svc := NewService(otelapi.Config{HTTP: otelapi.HTTPConfig{Enabled: true, ExtractHeaders: true}}, zap.NewNop(), tp)
	wrapped := svc.HTTPMiddleware()(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	otel.GetTextMapPropagator().Inject(parentCtx, propagation.HeaderCarrier(req.Header))

	wrapped.ServeHTTP(httptest.NewRecorder(), req)

	server := telemetrytest.MustSpanNamed(t, sr, "GET unmatched")
	assert.Equal(t, parent.SpanContext().TraceID(), telemetrytest.TraceID(server),
		"server span must continue the parent trace")
}

func TestInterceptor_SpanKind_RootIsServer(t *testing.T) {
	tp, sr := telemetrytest.NewTracerProvider()
	defer func() { _ = tp.Shutdown(context.Background()) }()

	inter := &interceptor{tracer: tp.Tracer("test"), logger: zap.NewNop()}
	task := runtime.Task{ID: registry.NewID("ns", "func")}
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		return &runtime.Result{}, nil
	}

	_, err := inter.Handle(context.Background(), task, next)
	require.NoError(t, err)

	span := telemetrytest.MustSpanNamed(t, sr, "ns:func")
	telemetrytest.SpanKind(t, span, trace.SpanKindServer)
}

func TestInterceptor_SpanKind_ParentIsInternal(t *testing.T) {
	tp, sr := telemetrytest.NewTracerProvider()
	defer func() { _ = tp.Shutdown(context.Background()) }()

	tracer := tp.Tracer("test")
	baseCtx, parent := tracer.Start(context.Background(), "parent")
	defer parent.End()

	fcCtx, fc := ctxapi.OpenFrameContext(baseCtx)
	defer ctxapi.ReleaseFrameContext(fc)
	require.NoError(t, otelapi.SetSpan(fcCtx, parent))

	inter := &interceptor{tracer: tracer, logger: zap.NewNop()}
	task := runtime.Task{ID: registry.NewID("ns", "func")}
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) { return &runtime.Result{}, nil }

	_, err := inter.Handle(fcCtx, task, next)
	require.NoError(t, err)

	span := telemetrytest.MustSpanNamed(t, sr, "ns:func")
	telemetrytest.SpanKind(t, span, trace.SpanKindInternal)
}

func TestInterceptor_SpanKind_QueueDeliveryIsConsumer(t *testing.T) {
	useTraceContextPropagator(t)
	tp, sr := telemetrytest.NewTracerProvider()
	defer func() { _ = tp.Shutdown(context.Background()) }()

	tracer := tp.Tracer("test")
	parentCtx, parent := tracer.Start(context.Background(), "publish.parent")
	defer parent.End()

	msg := &queueapi.Message{ID: "m1", Headers: attrsapi.NewBag()}
	otel.GetTextMapPropagator().Inject(parentCtx, &MessageHeaderCarrier{headers: msg.Headers})
	delivery := &queueapi.Delivery{Message: msg}

	inter := &interceptor{tracer: tracer, logger: zap.NewNop()}
	task := runtime.Task{
		ID:      registry.NewID("ns", "func"),
		Context: []ctxapi.Pair{{Value: delivery}},
	}
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) { return &runtime.Result{}, nil }

	_, err := inter.Handle(context.Background(), task, next)
	require.NoError(t, err)

	span := telemetrytest.MustSpanNamed(t, sr, "ns:func")
	telemetrytest.SpanKind(t, span, trace.SpanKindConsumer)
	telemetrytest.SpanHasStringAttr(t, span, "messaging.operation", "process")
	telemetrytest.SpanHasStringAttr(t, span, "messaging.message.id", "m1")
	assert.Equal(t, parent.SpanContext().TraceID(), telemetrytest.TraceID(span),
		"consumer span must continue the producer trace")
}

func TestInterceptor_ErrorStatus(t *testing.T) {
	tp, sr := telemetrytest.NewTracerProvider()
	defer func() { _ = tp.Shutdown(context.Background()) }()

	inter := &interceptor{tracer: tp.Tracer("test"), logger: zap.NewNop()}
	task := runtime.Task{ID: registry.NewID("ns", "func")}
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		return nil, errors.New("boom")
	}

	_, _ = inter.Handle(context.Background(), task, next)

	span := telemetrytest.MustSpanNamed(t, sr, "ns:func")
	telemetrytest.SpanStatus(t, span, codes.Error)
}
