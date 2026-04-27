// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"time"

	"github.com/wippyai/runtime/api/metrics"
	"go.opentelemetry.io/otel"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// telemetry owns metric and span emission for the raft subsystem. All recorders
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
	return &telemetry{
		coll:   coll,
		tracer: tp.Tracer("wippy-runtime"),
	}
}

func raftStateValue(state string) float64 {
	switch state {
	case "leader":
		return 2
	case "candidate":
		return 1
	default:
		return 0
	}
}

func (t *telemetry) recordState(node, state string) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.GaugeSet("raft_state", raftStateValue(state), metrics.Labels{"node": node})
}

func (t *telemetry) recordTerm(term uint64) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.GaugeSet("raft_term", float64(term), nil)
}

func (t *telemetry) recordLeaderChange() {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.CounterInc("raft_leader_changes_total", nil)
}

func (t *telemetry) recordElection(dur time.Duration) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.HistogramObserve("raft_election_duration_seconds", dur.Seconds(), nil)
}
