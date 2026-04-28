// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"context"
	"strconv"
	"time"

	"github.com/wippyai/runtime/api/metrics"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// telemetry owns metric and span emission for the pg subsystem. All recorders
// are nil-safe so callers can ignore the absence of a configured collector or
// tracer (e.g., in unit tests that don't wire OTel).
type telemetry struct {
	coll   metrics.Collector
	tracer trace.Tracer
}

func newTelemetry(coll metrics.Collector, mp otelmetric.MeterProvider, tp trace.TracerProvider) *telemetry {
	if tp == nil {
		tp = otel.GetTracerProvider()
	}
	_ = mp // metrics export is plumbed via metrics.Collector
	t := &telemetry{
		coll:   coll,
		tracer: tp.Tracer("wippy-runtime"),
	}
	// Bootstrap rare event-driven counters with zero so dashboards have
	// visible series even before the corresponding event ever fires.
	if coll != nil {
		coll.CounterAdd("pg_circuit_breaker_trips_total", 0, metrics.Labels{"pg": "_init"})
		coll.GaugeSet("pg_circuit_breaker_state", 0, metrics.Labels{"pg": "_init"})
		coll.CounterAdd("pg_retry_total", 0, metrics.Labels{"pg": "_init", "op": "noop", "attempt": "0"})
		coll.CounterAdd("pg_retry_giveup_total", 0, metrics.Labels{"pg": "_init", "op": "noop"})
		coll.CounterAdd("pg_retry_dropped_total", 0, metrics.Labels{"pg": "_init", "op": "noop"})
		coll.GaugeSet("pg_retry_queue_size", 0, metrics.Labels{"pg": "_init"})
		coll.GaugeSet("pg_dispatcher_inflight", 0, metrics.Labels{"pg": "_init"})
		coll.CounterAdd("pg_queue_dropped_total", 0, metrics.Labels{"pg": "_init", "reason": "noop"})
		coll.CounterAdd("pg_fence_rejection_total", 0, metrics.Labels{"pg": "_init", "reason": "noop"})
	}
	return t
}

func resultLabel(err error) string {
	if err != nil {
		return "err"
	}

	return "ok"
}

func (t *telemetry) recordJoin(pg string, err error, dur time.Duration) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.CounterInc("pg_join_total", metrics.Labels{"pg": pg, "result": resultLabel(err)})
	t.coll.HistogramObserve("pg_op_duration_seconds", dur.Seconds(), metrics.Labels{"pg": pg, "op": "join"})
}

func (t *telemetry) recordLeave(pg string, err error, dur time.Duration) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.CounterInc("pg_leave_total", metrics.Labels{"pg": pg, "result": resultLabel(err)})
	t.coll.HistogramObserve("pg_op_duration_seconds", dur.Seconds(), metrics.Labels{"pg": pg, "op": "leave"})
}

func (t *telemetry) recordBroadcast(pg string, recipients int, err error, dur time.Duration) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.CounterInc("pg_broadcast_total", metrics.Labels{"pg": pg, "result": resultLabel(err)})
	t.coll.HistogramObserve("pg_broadcast_recipients", float64(recipients), metrics.Labels{"pg": pg})
	t.coll.HistogramObserve("pg_op_duration_seconds", dur.Seconds(), metrics.Labels{"pg": pg, "op": "broadcast"})
}

func cbStateValue(state string) float64 {
	switch state {
	case "open":
		return 2
	case "half-open":
		return 1
	default: // "closed"
		return 0
	}
}

func (t *telemetry) recordQueueDepth(pg string, depth int) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.GaugeSet("pg_queue_depth", float64(depth), metrics.Labels{"pg": pg})
}

func (t *telemetry) recordQueueDropped(pg, reason string) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.CounterInc("pg_queue_dropped_total", metrics.Labels{"pg": pg, "reason": reason})
}

func (t *telemetry) recordCircuitBreakerState(pg, state string) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.GaugeSet("pg_circuit_breaker_state", cbStateValue(state), metrics.Labels{"pg": pg})
}

func (t *telemetry) recordCircuitBreakerTrip(pg string) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.CounterInc("pg_circuit_breaker_trips_total", metrics.Labels{"pg": pg})
}

func (t *telemetry) recordRetry(pg, op string, attempt int) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.CounterInc("pg_retry_total", metrics.Labels{"pg": pg, "op": op, "attempt": strconv.Itoa(attempt)})
}

func (t *telemetry) recordRetryGiveup(pg, op string) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.CounterInc("pg_retry_giveup_total", metrics.Labels{"pg": pg, "op": op})
}

func (t *telemetry) recordRetryDropped(pg, op string) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.CounterInc("pg_retry_dropped_total", metrics.Labels{"pg": pg, "op": op})
}

func (t *telemetry) recordRetryQueueSize(pg string, size int) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.GaugeSet("pg_retry_queue_size", float64(size), metrics.Labels{"pg": pg})
}

func (t *telemetry) startSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	if t == nil || t.tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}

	return t.tracer.Start(ctx, name, opts...)
}

func (t *telemetry) setSpanError(span trace.Span, err error) {
	if err == nil || span == nil {
		return
	}

	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}
