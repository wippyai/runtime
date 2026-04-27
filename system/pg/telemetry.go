// SPDX-License-Identifier: MPL-2.0

package pg

import (
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
