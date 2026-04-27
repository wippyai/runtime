// SPDX-License-Identifier: MPL-2.0

package membership

import (
	"github.com/wippyai/runtime/api/metrics"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// telemetry owns metric and span emission for the membership/gossip subsystem.
// All recorders are nil-safe so callers can ignore the absence of a configured
// collector or tracer (e.g., in unit tests that don't wire OTel).
type telemetry struct {
	coll metrics.Collector
}

func newTelemetry(coll metrics.Collector, mp otelmetric.MeterProvider, tp trace.TracerProvider) *telemetry {
	_ = mp // metrics export is plumbed via metrics.Collector
	_ = tp // tracer wiring lands with span emission in a follow-up task

	return &telemetry{coll: coll}
}

func gossipResult(err error) string {
	if err != nil {
		return "err"
	}

	return "ok"
}

func (t *telemetry) recordMembers(state string, n int) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.GaugeSet("gossip_members", float64(n), metrics.Labels{"state": state})
}

func (t *telemetry) recordJoin(err error) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.CounterInc("gossip_join_total", metrics.Labels{"result": gossipResult(err)})
}

func (t *telemetry) recordLeave() {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.CounterInc("gossip_leave_total", nil)
}

func (t *telemetry) recordMessage(kind, direction string, bytes int) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.CounterInc("gossip_message_total", metrics.Labels{"kind": kind, "direction": direction})
	if bytes > 0 {
		t.coll.HistogramObserve("gossip_message_bytes", float64(bytes),
			metrics.Labels{"kind": kind, "direction": direction})
	}
}
