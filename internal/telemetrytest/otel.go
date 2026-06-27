// SPDX-License-Identifier: MPL-2.0

package telemetrytest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// NewTracerProvider returns a real TracerProvider wired to an in-memory
// SpanRecorder, so tests can assert on the spans that were actually emitted
// instead of relying on noop tracers that only verify wiring.
func NewTracerProvider() (*sdktrace.TracerProvider, *tracetest.SpanRecorder) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	return tp, sr
}

// SpanCount asserts the total number of ended spans recorded by sr.
func SpanCount(t *testing.T, sr *tracetest.SpanRecorder, want int) {
	t.Helper()
	assert.Lenf(t, sr.Ended(), want, "expected %d ended span(s)", want)
}

// MustSpanNamed returns the single ended span with the given name, failing the
// test if there is not exactly one match.
func MustSpanNamed(t *testing.T, sr *tracetest.SpanRecorder, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	var matches []sdktrace.ReadOnlySpan
	for _, s := range sr.Ended() {
		if s.Name() == name {
			matches = append(matches, s)
		}
	}
	require.Lenf(t, matches, 1, "expected exactly 1 span named %q, got %d", name, len(matches))
	return matches[0]
}

// SpanKind asserts the span's kind.
func SpanKind(t *testing.T, span sdktrace.ReadOnlySpan, want trace.SpanKind) {
	t.Helper()
	assert.Equalf(t, want, span.SpanKind(), "span %q kind", span.Name())
}

// SpanStatus asserts the span's status code.
func SpanStatus(t *testing.T, span sdktrace.ReadOnlySpan, want codes.Code) {
	t.Helper()
	assert.Equalf(t, want, span.Status().Code, "span %q status code", span.Name())
}

// SpanAttr returns the value of the named attribute and whether it was present.
func SpanAttr(span sdktrace.ReadOnlySpan, key string) (attribute.Value, bool) {
	for _, kv := range span.Attributes() {
		if string(kv.Key) == key {
			return kv.Value, true
		}
	}
	return attribute.Value{}, false
}

// SpanHasStringAttr asserts the span carries a string attribute key==want.
func SpanHasStringAttr(t *testing.T, span sdktrace.ReadOnlySpan, key, want string) {
	t.Helper()
	v, ok := SpanAttr(span, key)
	if !ok {
		t.Fatalf("span %q has no attribute %q", span.Name(), key)
	}
	assert.Equalf(t, want, v.AsString(), "span %q attribute %q", span.Name(), key)
}

// SpanHasInt64Attr asserts the span carries an int64 attribute key==want.
func SpanHasInt64Attr(t *testing.T, span sdktrace.ReadOnlySpan, key string, want int64) {
	t.Helper()
	v, ok := SpanAttr(span, key)
	if !ok {
		t.Fatalf("span %q has no attribute %q", span.Name(), key)
	}
	assert.Equalf(t, want, v.AsInt64(), "span %q attribute %q", span.Name(), key)
}

// TraceID returns the trace ID carried by a recorded span.
func TraceID(span sdktrace.ReadOnlySpan) trace.TraceID {
	return span.SpanContext().TraceID()
}
