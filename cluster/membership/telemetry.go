// SPDX-License-Identifier: MPL-2.0

package membership

import (
	"context"
	"time"

	"github.com/wippyai/runtime/api/metrics"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// telemetry owns metric and span emission for the membership/gossip subsystem.
// All recorders are nil-safe so callers can ignore the absence of a configured
// collector or tracer (e.g., in unit tests that don't wire OTel).
type telemetry struct {
	coll   metrics.Collector
	tracer trace.Tracer
}

func newTelemetry(coll metrics.Collector, mp otelmetric.MeterProvider, tp trace.TracerProvider) *telemetry {
	if tp == nil {
		tp = otel.GetTracerProvider()
	}
	_ = mp // metrics export is plumbed via metrics.Collector

	t := &telemetry{coll: coll, tracer: tp.Tracer("wippy-runtime")}
	if coll != nil {
		coll.CounterAdd("gossip_leave_total", 0, nil)
		coll.CounterAdd("gossip_probe_failures_total", 0, metrics.Labels{"target": "_init"})
		coll.CounterAdd("gossip_suspicion_resolutions_total", 0, metrics.Labels{"outcome": "alive"})
		coll.CounterAdd("gossip_suspicion_resolutions_total", 0, metrics.Labels{"outcome": "dead"})
		// Bootstrap user-broadcast counters so dashboards/gates can detect
		// silence (zero series) vs. genuinely-zero rate (visible series at 0).
		// Without this, a stuck NotifyMsg path looks identical to "no metric
		// configured."
		coll.CounterAdd("gossip_message_total", 0, metrics.Labels{"kind": "user", "direction": "rx"})
		coll.CounterAdd("gossip_message_total", 0, metrics.Labels{"kind": "user", "direction": "tx"})
	}
	return t
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

func (t *telemetry) recordProbe(err error, dur time.Duration) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.HistogramObserve("gossip_probe_duration_seconds", dur.Seconds(),
		metrics.Labels{"result": gossipResult(err)})
}

func (t *telemetry) recordProbeFailure(target string) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.CounterInc("gossip_probe_failures_total", metrics.Labels{"target": target})
}

func (t *telemetry) recordSuspicionOutcome(outcome string) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.CounterInc("gossip_suspicion_resolutions_total", metrics.Labels{"outcome": outcome})
}

func (t *telemetry) recordConvergence(dur time.Duration) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.HistogramObserve("gossip_convergence_seconds", dur.Seconds(), nil)
}

func (t *telemetry) startSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	if ctx == nil {
		ctx = context.Background()
	}

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
