// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"strconv"
	"time"

	"github.com/wippyai/runtime/api/metrics"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// telemetry owns metric and span emission for the pg subsystem. All recorders
// are nil-safe so callers can ignore the absence of a configured collector or
// tracer (e.g., in unit tests that don't wire OTel).
type telemetry struct {
	coll metrics.Collector
}

func newTelemetry(coll metrics.Collector, _ otelmetric.MeterProvider, _ trace.TracerProvider) *telemetry {
	return &telemetry{coll: coll}
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

func (t *telemetry) recordDispatcherInflight(pg string, n int) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.GaugeSet("pg_dispatcher_inflight", float64(n), metrics.Labels{"pg": pg})
}

func (t *telemetry) recordFenceToken(pg, node string, token uint64) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.GaugeSet("pg_fence_token", float64(token), metrics.Labels{"pg": pg, "node": node})
}

func (t *telemetry) recordFenceRejection(pg, reason string) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.CounterInc("pg_fence_rejection_total", metrics.Labels{"pg": pg, "reason": reason})
}

func (t *telemetry) recordGlobalregSize(n int) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.GaugeSet("pg_globalreg_size", float64(n), nil)
}

func (t *telemetry) recordGlobalregDedupe() {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.CounterInc("pg_globalreg_dedupe_total", nil)
}
