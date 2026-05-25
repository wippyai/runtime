// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"context"
	"time"

	"github.com/wippyai/runtime/api/metrics"
	"go.opentelemetry.io/otel/codes"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// telemetry owns metric and span emission for the pg subsystem. All recorders
// are nil-safe so callers can ignore the absence of a configured collector or
// tracer (e.g., in unit tests that don't wire OTel). When tracing is disabled
// (no TracerProvider wired), startSpan returns a shared no-op span and
// allocates nothing on the hot path.
type telemetry struct {
	coll    metrics.Collector
	tracer  trace.Tracer
	tracing bool
}

// noopSpan is the shared instance returned when tracing is disabled, so the
// hot path never allocates a Span just to satisfy the (ctx, Span) signature.
var noopSpan = trace.SpanFromContext(context.Background())

func newTelemetry(coll metrics.Collector, mp otelmetric.MeterProvider, tp trace.TracerProvider) *telemetry {
	_ = mp // metrics export is plumbed via metrics.Collector
	t := &telemetry{coll: coll}
	// Tracing is opt-in: callers wire it by passing a non-nil TracerProvider.
	// When nil, the hot path returns the shared noopSpan and skips every
	// allocation associated with span creation. Tests and benchmarks rely on
	// this fast path; production deployments that want tracing pass their
	// configured TracerProvider explicitly.
	if tp != nil {
		t.tracer = tp.Tracer("wippy-runtime")
		t.tracing = true
	}
	// Bootstrap rare event-driven counters with zero so dashboards have
	// visible series even before the corresponding event ever fires.
	if coll != nil {
		coll.CounterAdd("pg_circuit_breaker_trips_total", 0, metrics.Labels{"pg": "_init"})
		coll.GaugeSet("pg_circuit_breaker_state", 0, metrics.Labels{"pg": "_init"})
		coll.CounterAdd("pg_retry_total", 0, metrics.Labels{"pg": "_init", "op": "noop", "attempt": "1"})
		coll.CounterAdd("pg_retry_giveup_total", 0, metrics.Labels{"pg": "_init", "op": "noop"})
		coll.CounterAdd("pg_retry_dropped_total", 0, metrics.Labels{"pg": "_init", "op": "noop"})
		coll.GaugeSet("pg_retry_queue_size", 0, metrics.Labels{"pg": "_init"})
		coll.GaugeSet("pg_dispatcher_inflight", 0, metrics.Labels{"pg": "_init"})
		coll.CounterAdd("pg_queue_dropped_total", 0, metrics.Labels{"pg": "_init", "reason": "noop"})
		coll.CounterAdd("pg_fence_rejection_total", 0, metrics.Labels{"pg": "_init", "reason": "noop"})
		coll.CounterAdd("pg_broadcast_dropped_total", 0,
			metrics.Labels{"pg": "_init", "reason": "noop"})
		coll.CounterAdd("pg_monitors_evicted_total", 0, metrics.Labels{"reason": "node_left"})
		coll.CounterAdd("pg_monitors_evicted_total", 0, metrics.Labels{"reason": "ttl"})
		coll.CounterAdd("pg_circuit_breaker_evicted_total", 0, metrics.Labels{"reason": "cap"})
	}
	return t
}

// recordMonitorsEvicted is incremented when monitor entries are removed
// in bulk via a node-leave or TTL sweep, rather than via an explicit
// Demonitor / process exit. Without this counter the leak that the
// eviction path fixes would be invisible in dashboards.
func (t *telemetry) recordMonitorsEvicted(reason string, n int) {
	if t == nil || t.coll == nil || n <= 0 {
		return
	}
	t.coll.CounterAdd("pg_monitors_evicted_total", float64(n),
		metrics.Labels{"reason": reason})
}

// recordDiscoverTargets emits the size of each discover fan-out so the soak
// can verify the initial cap holds and gossip-driven discovery keeps the
// cluster converged without N² messaging on simultaneous restart.
func (t *telemetry) recordDiscoverTargets(phase string, picked, total int) {
	if t == nil || t.coll == nil {
		return
	}
	labels := metrics.Labels{"phase": phase}
	t.coll.HistogramObserve("pg_discover_targets", float64(picked), labels)
	t.coll.HistogramObserve("pg_discover_pool_size", float64(total), labels)
}

// recordCircuitBreakerEvicted is incremented when the breakers map
// reaches its cap and an arbitrary entry is dropped. Should normally
// stay at zero — non-zero indicates that NodeLeft cleanup is missing
// nodes (e.g., long-lived split-brain) and the defense-in-depth cap
// is doing real work.
func (t *telemetry) recordCircuitBreakerEvicted(reason string) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("pg_circuit_breaker_evicted_total",
		metrics.Labels{"reason": reason})
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

// attemptBucket maps an integer attempt count to a bounded label so the
// `pg_retry_total` series cardinality is finite under prolonged churn.
// Buckets: "1" (first attempt), "2-3" (early retries), "4+" (struggling).
func attemptBucket(attempt int) string {
	switch {
	case attempt <= 1:
		return "1"
	case attempt <= 3:
		return "2-3"
	default:
		return "4+"
	}
}

func (t *telemetry) recordRetry(pg, op string, attempt int) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.CounterInc("pg_retry_total", metrics.Labels{"pg": pg, "op": op, "attempt": attemptBucket(attempt)})
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

func (t *telemetry) recordBroadcastDropped(pg, reason string) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.CounterInc("pg_broadcast_dropped_total",
		metrics.Labels{"pg": pg, "reason": reason})
}

func (t *telemetry) startSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	if t == nil || !t.tracing {
		return ctx, noopSpan
	}

	return t.tracer.Start(ctx, name, opts...)
}

func (t *telemetry) setSpanError(span trace.Span, err error) {
	if err == nil || span == nil || t == nil || !t.tracing {
		return
	}

	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}
