// SPDX-License-Identifier: MPL-2.0

package otel

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	attrsapi "github.com/wippyai/runtime/api/attrs"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/telemetrytest"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

func TestPublishInterceptor_ProducerSpan(t *testing.T) {
	useTraceContextPropagator(t)
	tp, sr := telemetrytest.NewTracerProvider()
	defer func() { _ = tp.Shutdown(context.Background()) }()

	pi := NewPublishInterceptor(tp.Tracer("test"))
	queueID := registry.NewID("ns", "orders")
	msg := &queueapi.Message{ID: "m1", Headers: attrsapi.NewBag()}

	called := false
	next := func(_ context.Context, _ registry.ID, _ []*queueapi.Message) error {
		called = true
		return nil
	}

	require.NoError(t, pi.Handle(context.Background(), queueID, []*queueapi.Message{msg}, next))
	require.True(t, called)

	span := telemetrytest.MustSpanNamed(t, sr, "ns:orders.publish")
	telemetrytest.SpanKind(t, span, trace.SpanKindProducer)
	telemetrytest.SpanHasStringAttr(t, span, "messaging.operation", "publish")
	telemetrytest.SpanHasStringAttr(t, span, "messaging.destination.name", "ns:orders")
	telemetrytest.SpanHasStringAttr(t, span, "messaging.message.id", "m1")

	tpVal, ok := msg.Headers.Get("traceparent")
	require.True(t, ok, "traceparent must be injected into the message headers")
	tpStr, ok := tpVal.(string)
	require.True(t, ok)
	assert.NotEmpty(t, tpStr)
}

func TestExtractFromDelivery_LinksChild(t *testing.T) {
	useTraceContextPropagator(t)
	tp, sr := telemetrytest.NewTracerProvider()
	defer func() { _ = tp.Shutdown(context.Background()) }()

	tracer := tp.Tracer("test")
	parentCtx, parent := tracer.Start(context.Background(), "publish.parent")
	defer parent.End()

	msg := &queueapi.Message{ID: "m1", Headers: attrsapi.NewBag()}
	otel.GetTextMapPropagator().Inject(parentCtx, &MessageHeaderCarrier{headers: msg.Headers})

	extractedCtx, hasSpan := extractFromDelivery(context.Background(), &queueapi.Delivery{Message: msg})
	require.True(t, hasSpan, "extract must recover a valid span context")

	_, child := tracer.Start(extractedCtx, "consume")
	child.End()

	consumed := telemetrytest.MustSpanNamed(t, sr, "consume")
	assert.Equal(t, parent.SpanContext().TraceID(), telemetrytest.TraceID(consumed),
		"consumer span must share the producer trace ID")
}
