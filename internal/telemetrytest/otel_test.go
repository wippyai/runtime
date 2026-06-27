// SPDX-License-Identifier: MPL-2.0

package telemetrytest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

func TestNewTracerProvider_RecordsSpans(t *testing.T) {
	tp, sr := NewTracerProvider()
	defer func() { _ = tp.Shutdown(context.Background()) }()
	tracer := tp.Tracer("test")

	ctx, parent := tracer.Start(context.Background(), "root", trace.WithSpanKind(trace.SpanKindServer))
	parent.SetAttributes(attribute.String("http.method", "GET"))
	parent.SetStatus(codes.Ok, "")
	parent.End()

	_, child := tracer.Start(ctx, "child", trace.WithSpanKind(trace.SpanKindInternal))
	child.End()

	SpanCount(t, sr, 2)
	root := MustSpanNamed(t, sr, "root")
	SpanKind(t, root, trace.SpanKindServer)
	SpanStatus(t, root, codes.Ok)
	SpanHasStringAttr(t, root, "http.method", "GET")

	v, ok := SpanAttr(root, "absent")
	assert.False(t, ok, "absent attr should not be found")
	_ = v

	childSpan := MustSpanNamed(t, sr, "child")
	assert.Equal(t, TraceID(root), TraceID(childSpan), "parent and child share a trace ID")
}
