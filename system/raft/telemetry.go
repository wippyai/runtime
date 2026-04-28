// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"context"
	"time"

	"github.com/wippyai/runtime/api/metrics"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
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
	t := &telemetry{
		coll:   coll,
		tracer: tp.Tracer("wippy-runtime"),
	}
	if coll != nil {
		// Bootstrap rare event-driven counters with a zero increment so
		// dashboards have visible series even before the first event. Avoid
		// HistogramObserve here — that would add a real observation, which
		// breaks unit tests that count observations.
		coll.CounterAdd("raft_leader_changes_total", 0, nil)
		coll.CounterAdd("raft_snapshot_total", 0, metrics.Labels{"result": "ok"})
		coll.CounterAdd("raft_request_vote_total", 0, metrics.Labels{"peer": "_init", "result": "ok"})
		coll.CounterAdd("raft_install_snapshot_total", 0, metrics.Labels{"peer": "_init", "result": "ok"})
	}
	return t
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

func (t *telemetry) recordCommitIndex(idx uint64) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.GaugeSet("raft_commit_index", float64(idx), nil)
}

func (t *telemetry) recordLastLogIndex(node string, idx uint64) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.GaugeSet("raft_last_log_index", float64(idx), metrics.Labels{"node": node})
}

func (t *telemetry) recordLogLag(node string, lag int64) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.GaugeSet("raft_log_lag", float64(lag), metrics.Labels{"node": node})
}

func (t *telemetry) recordAppendEntries(peer string, err error, dur time.Duration) {
	if t == nil || t.coll == nil {
		return
	}

	res := raftResultLabel(err)
	t.coll.CounterInc("raft_append_entries_total", metrics.Labels{"peer": peer, "result": res})
	t.coll.HistogramObserve("raft_append_entries_duration_seconds", dur.Seconds(),
		metrics.Labels{"peer": peer, "result": res})
}

func (t *telemetry) recordRequestVote(peer string, err error, dur time.Duration) {
	if t == nil || t.coll == nil {
		return
	}

	res := raftResultLabel(err)
	t.coll.CounterInc("raft_request_vote_total", metrics.Labels{"peer": peer, "result": res})
	t.coll.HistogramObserve("raft_request_vote_duration_seconds", dur.Seconds(),
		metrics.Labels{"peer": peer, "result": res})
}

func (t *telemetry) recordInstallSnapshot(peer string, err error, dur time.Duration) {
	if t == nil || t.coll == nil {
		return
	}

	res := raftResultLabel(err)
	t.coll.CounterInc("raft_install_snapshot_total", metrics.Labels{"peer": peer, "result": res})
	t.coll.HistogramObserve("raft_install_snapshot_duration_seconds", dur.Seconds(),
		metrics.Labels{"peer": peer, "result": res})
}

func raftResultLabel(err error) string {
	if err != nil {
		return "err"
	}

	return "ok"
}

func (t *telemetry) recordVoterLadder(voters, nonVoters, voterCap int) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.GaugeSet("raft_voters", float64(voters), nil)
	t.coll.GaugeSet("raft_non_voters", float64(nonVoters), nil)
	t.coll.GaugeSet("raft_voter_cap", float64(voterCap), nil)
}

func (t *telemetry) recordSnapshot(err error, dur time.Duration, sizeBytes int64) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.CounterInc("raft_snapshot_total", metrics.Labels{"result": raftResultLabel(err)})
	t.coll.HistogramObserve("raft_snapshot_duration_seconds", dur.Seconds(), nil)
	t.coll.HistogramObserve("raft_snapshot_size_bytes", float64(sizeBytes), nil)
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
