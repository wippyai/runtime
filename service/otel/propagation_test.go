// SPDX-License-Identifier: MPL-2.0

package otel

import (
	"context"
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
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// TestPropagation_HTTP_Function_Queue_Consume proves a single trace survives
// the full surface chain: HTTP server span -> function (Internal) -> queue
// publish (Producer, traceparent injected into the message) -> queue consume
// (Consumer, trace recovered from the message headers).
func TestPropagation_HTTP_Function_Queue_Consume(t *testing.T) {
	useTraceContextPropagator(t)
	tp, sr := telemetrytest.NewTracerProvider()
	defer func() { _ = tp.Shutdown(context.Background()) }()

	svc := NewService(otelapi.Config{
		HTTP:        otelapi.HTTPConfig{Enabled: true, ExtractHeaders: true, InjectHeaders: true},
		Interceptor: otelapi.InterceptorConfig{Enabled: true},
		Queue:       otelapi.QueueConfig{Enabled: true},
	}, zap.NewNop(), tp)
	httpMW := svc.HTTPMiddleware()
	funcInter := svc.Interceptor()
	require.NotNil(t, funcInter)
	pi := NewPublishInterceptor(tp.Tracer("wippy-runtime"))

	var publishedMsg *queueapi.Message
	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		task := runtime.Task{ID: registry.NewID("ns", "func")}
		next := func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
			msg := &queueapi.Message{ID: "m1", Headers: attrsapi.NewBag()}
			publishedMsg = msg
			pubNext := func(_ context.Context, _ registry.ID, _ []*queueapi.Message) error { return nil }
			if err := pi.Handle(ctx, registry.NewID("ns", "orders"), []*queueapi.Message{msg}, pubNext); err != nil {
				return nil, err
			}
			return &runtime.Result{}, nil
		}
		_, _ = funcInter.Handle(r.Context(), task, next)
	})
	wrapped := httpMW(handler)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/orders", nil)
	fcCtx, fc := ctxapi.OpenFrameContext(req.Context())
	defer ctxapi.ReleaseFrameContext(fc)
	require.NoError(t, httpapi.SetRouteLabel(fcCtx, "/orders"))
	req = req.WithContext(fcCtx)

	wrapped.ServeHTTP(httptest.NewRecorder(), req)

	// Simulate the worker consuming the published message.
	require.NotNil(t, publishedMsg)
	consumeTask := runtime.Task{
		ID:      registry.NewID("ns", "consumer"),
		Context: []ctxapi.Pair{{Value: &queueapi.Delivery{Message: publishedMsg}}},
	}
	consumeNext := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) { return &runtime.Result{}, nil }
	_, _ = funcInter.Handle(context.Background(), consumeTask, consumeNext)

	// Every span in the chain must share a single trace ID.
	spans := sr.Ended()
	require.GreaterOrEqual(t, len(spans), 4, "expected at least the 4 chain spans, got %d", len(spans))

	wantNames := map[string]bool{
		"GET /orders":       false,
		"ns:func":           false,
		"ns:orders.publish": false,
		"ns:consumer":       false,
	}
	for _, s := range spans {
		if _, ok := wantNames[s.Name()]; ok {
			wantNames[s.Name()] = true
		}
	}
	for name, seen := range wantNames {
		assert.Truef(t, seen, "expected span %q in the chain", name)
	}

	var traceID trace.TraceID
	for i, s := range spans {
		if i == 0 {
			traceID = telemetrytest.TraceID(s)
			continue
		}
		assert.Equal(t, traceID, telemetrytest.TraceID(s),
			"span %q must share the single chain trace ID", s.Name())
	}
}
