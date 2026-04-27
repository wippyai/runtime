// SPDX-License-Identifier: MPL-2.0

package globalreg

import (
	"github.com/wippyai/runtime/api/metrics"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// telemetry owns metric emission for the globalreg subsystem. It emits with
// the same pg_* prefix used elsewhere in the runtime — those metric names
// are part of the Grafana dashboard contract and are independent of the
// owning Go package.
//
// All recorders are nil-safe so callers can ignore the absence of a
// configured collector (e.g., in unit tests that don't wire OTel).
type telemetry struct {
	coll metrics.Collector
}

// newTelemetry constructs a telemetry recorder. The tracer/meter providers
// are accepted to match the per-package constructor pattern but are not
// currently used — the globalreg subsystem only emits metrics.
func newTelemetry(coll metrics.Collector, _ otelmetric.MeterProvider, _ trace.TracerProvider) *telemetry {
	return &telemetry{coll: coll}
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
