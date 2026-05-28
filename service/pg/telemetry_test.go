// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"context"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/internal/telemetrytest"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func newTestTelemetry(t *testing.T) (*telemetry, *telemetrytest.Recorder) {
	t.Helper()
	rec := telemetrytest.NewRecorder()
	tt := newTelemetry(rec, nil, nil)
	if tt == nil {
		t.Fatalf("newTelemetry returned nil")
	}
	return tt, rec
}

func TestTelemetry_RecordJoin_OK(t *testing.T) {
	tt, rec := newTestTelemetry(t)
	tt.recordJoin("g1", nil, 12*time.Millisecond)

	if v := rec.CounterValue("pg_join_total", metrics.Labels{"pg": "g1", "result": "ok"}); v != 1 {
		t.Fatalf("pg_join_total{pg=g1,result=ok}: want 1, got %v", v)
	}
	if c := rec.HistogramCount("pg_op_duration_seconds", metrics.Labels{"pg": "g1", "op": "join"}); c != 1 {
		t.Fatalf("pg_op_duration_seconds count: want 1, got %v", c)
	}
}

func TestTelemetry_RecordJoin_Error(t *testing.T) {
	tt, rec := newTestTelemetry(t)
	tt.recordJoin("g1", errSomething, 0)
	if v := rec.CounterValue("pg_join_total", metrics.Labels{"pg": "g1", "result": "err"}); v != 1 {
		t.Fatalf("pg_join_total{result=err}: want 1, got %v", v)
	}
}

func TestTelemetry_RecordLeave(t *testing.T) {
	tt, rec := newTestTelemetry(t)
	tt.recordLeave("g1", nil, 5*time.Millisecond)
	if v := rec.CounterValue("pg_leave_total", metrics.Labels{"pg": "g1", "result": "ok"}); v != 1 {
		t.Fatalf("pg_leave_total: want 1, got %v", v)
	}
}

func TestTelemetry_RecordBroadcast(t *testing.T) {
	tt, rec := newTestTelemetry(t)
	tt.recordBroadcast("g1", 7, nil, 20*time.Millisecond)
	if v := rec.CounterValue("pg_broadcast_total", metrics.Labels{"pg": "g1", "result": "ok"}); v != 1 {
		t.Fatalf("pg_broadcast_total: want 1, got %v", v)
	}
	if c := rec.HistogramCount("pg_broadcast_recipients", metrics.Labels{"pg": "g1"}); c != 1 {
		t.Fatalf("pg_broadcast_recipients count: want 1, got %v", c)
	}
}

func TestTelemetry_NilCollector_NoPanic(t *testing.T) {
	tt := newTelemetry(nil, nil, nil)
	tt.recordJoin("g1", nil, time.Millisecond)
	tt.recordLeave("g1", nil, time.Millisecond)
	tt.recordBroadcast("g1", 1, nil, time.Millisecond)
	tt.recordQueueDepth("g1", 0)
	tt.recordQueueDropped("g1", "full")
	tt.recordCircuitBreakerState("g1", "open")
	tt.recordCircuitBreakerTrip("g1")
	tt.recordRetry("g1", "broadcast", 1)
	tt.recordRetryGiveup("g1", "broadcast")
}

func TestTelemetry_RecordQueue(t *testing.T) {
	tt, rec := newTestTelemetry(t)
	tt.recordQueueDepth("g1", 7)
	if v := rec.GaugeValue("pg_queue_depth", metrics.Labels{"pg": "g1"}); v != 7 {
		t.Fatalf("pg_queue_depth: want 7, got %v", v)
	}
	tt.recordQueueDropped("g1", "full")
	if v := rec.CounterValue("pg_queue_dropped_total", metrics.Labels{"pg": "g1", "reason": "full"}); v != 1 {
		t.Fatalf("pg_queue_dropped_total: want 1, got %v", v)
	}
}

func TestTelemetry_RecordCircuitBreaker(t *testing.T) {
	tt, rec := newTestTelemetry(t)
	tt.recordCircuitBreakerState("g1", "open")
	if v := rec.GaugeValue("pg_circuit_breaker_state", metrics.Labels{"pg": "g1"}); v != 2 {
		t.Fatalf("cb_state(open): want 2, got %v", v)
	}
	tt.recordCircuitBreakerState("g1", "half-open")
	if v := rec.GaugeValue("pg_circuit_breaker_state", metrics.Labels{"pg": "g1"}); v != 1 {
		t.Fatalf("cb_state(half-open): want 1, got %v", v)
	}
	tt.recordCircuitBreakerTrip("g1")
	if v := rec.CounterValue("pg_circuit_breaker_trips_total", metrics.Labels{"pg": "g1"}); v != 1 {
		t.Fatalf("cb_trips: want 1, got %v", v)
	}
}

func TestTelemetry_RecordRetry(t *testing.T) {
	tt, rec := newTestTelemetry(t)
	tt.recordRetry("g1", "broadcast", 2)
	if v := rec.CounterValue("pg_retry_total", metrics.Labels{"pg": "g1", "op": "broadcast", "attempt": "2-3"}); v != 1 {
		t.Fatalf("pg_retry_total: want 1, got %v", v)
	}
	tt.recordRetryGiveup("g1", "broadcast")
	if v := rec.CounterValue("pg_retry_giveup_total", metrics.Labels{"pg": "g1", "op": "broadcast"}); v != 1 {
		t.Fatalf("pg_retry_giveup_total: want 1, got %v", v)
	}
}

var errSomething = errStub("boom")

type errStub string

func (e errStub) Error() string { return string(e) }

func newTestTelemetryWithSpans(t *testing.T) (*telemetry, *telemetrytest.Recorder, *tracetest.SpanRecorder) {
	t.Helper()
	rec := telemetrytest.NewRecorder()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	return newTelemetry(rec, nil, tp), rec, sr
}

func TestTelemetry_JoinSpan_Success(t *testing.T) {
	tt, _, sr := newTestTelemetryWithSpans(t)
	_, span := tt.startSpan(context.Background(), "pg.join")
	span.End()
	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("want 1 span, got %d", len(spans))
	}
	if spans[0].Name() != "pg.join" {
		t.Fatalf("name: want pg.join, got %s", spans[0].Name())
	}
}

func TestTelemetry_SetSpanError(t *testing.T) {
	tt, _, sr := newTestTelemetryWithSpans(t)
	_, span := tt.startSpan(context.Background(), "pg.broadcast")
	tt.setSpanError(span, errSomething)
	span.End()
	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatal("want 1 span")
	}
	if spans[0].Status().Code != codes.Error {
		t.Fatalf("want Error code, got %v", spans[0].Status().Code)
	}
}

func TestAttemptBucket(t *testing.T) {
	cases := []struct {
		want string
		in   int
	}{
		{"1", 0}, {"1", 1},
		{"2-3", 2}, {"2-3", 3},
		{"4+", 4}, {"4+", 99},
	}
	for _, c := range cases {
		if got := attemptBucket(c.in); got != c.want {
			t.Errorf("attemptBucket(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}
