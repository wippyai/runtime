// SPDX-License-Identifier: MPL-2.0

package membership

import (
	"context"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/internal/telemetrytest"
)

func newTestTel(t *testing.T) (*telemetry, *telemetrytest.Recorder) {
	t.Helper()
	rec := telemetrytest.NewRecorder()
	return newTelemetry(rec, nil, nil), rec
}

func TestGossipTelemetry_Members(t *testing.T) {
	tt, rec := newTestTel(t)
	tt.recordMembers("alive", 5)
	tt.recordMembers("suspect", 1)
	tt.recordMembers("dead", 0)
	if v := rec.GaugeValue("gossip_members", metrics.Labels{"state": "alive"}); v != 5 {
		t.Fatalf("alive: want 5, got %v", v)
	}
	if v := rec.GaugeValue("gossip_members", metrics.Labels{"state": "suspect"}); v != 1 {
		t.Fatalf("suspect: want 1, got %v", v)
	}
}

func TestGossipTelemetry_JoinLeave(t *testing.T) {
	tt, rec := newTestTel(t)
	tt.recordJoin(nil)
	tt.recordJoin(errSomething)
	tt.recordLeave()
	if v := rec.CounterValue("gossip_join_total", metrics.Labels{"result": "ok"}); v != 1 {
		t.Fatalf("join ok: want 1, got %v", v)
	}
	if v := rec.CounterValue("gossip_join_total", metrics.Labels{"result": "err"}); v != 1 {
		t.Fatalf("join err: want 1, got %v", v)
	}
	if v := rec.CounterValue("gossip_leave_total", nil); v != 1 {
		t.Fatalf("leave: want 1, got %v", v)
	}
}

func TestGossipTelemetry_Message(t *testing.T) {
	tt, rec := newTestTel(t)
	tt.recordMessage("ping", "tx", 64)
	tt.recordMessage("ping", "rx", 64)
	if v := rec.CounterValue("gossip_message_total", metrics.Labels{"kind": "ping", "direction": "tx"}); v != 1 {
		t.Fatalf("tx: want 1, got %v", v)
	}
	if c := rec.HistogramCount("gossip_message_bytes", metrics.Labels{"kind": "ping", "direction": "tx"}); c != 1 {
		t.Fatalf("bytes obs: want 1, got %v", c)
	}
}

func TestGossipTelemetry_Probe(t *testing.T) {
	tt, rec := newTestTel(t)
	tt.recordProbe(nil, 5*time.Millisecond)
	tt.recordProbe(errSomething, 0)
	tt.recordProbeFailure("node-x")
	if c := rec.HistogramCount("gossip_probe_duration_seconds", metrics.Labels{"result": "ok"}); c != 1 {
		t.Fatalf("probe ok hist: want 1, got %v", c)
	}

	if v := rec.CounterValue("gossip_probe_failures_total", metrics.Labels{"target": "node-x"}); v != 1 {
		t.Fatalf("probe failures: want 1, got %v", v)
	}
}

func TestGossipTelemetry_SuspicionAndConvergence(t *testing.T) {
	tt, rec := newTestTel(t)
	tt.recordSuspicionOutcome("alive")
	tt.recordSuspicionOutcome("dead")
	tt.recordConvergence(2 * time.Second)
	if v := rec.CounterValue("gossip_suspicion_resolutions_total", metrics.Labels{"outcome": "alive"}); v != 1 {
		t.Fatalf("suspicion alive: want 1, got %v", v)
	}

	if v := rec.CounterValue("gossip_suspicion_resolutions_total", metrics.Labels{"outcome": "dead"}); v != 1 {
		t.Fatalf("suspicion dead: want 1, got %v", v)
	}

	if c := rec.HistogramCount("gossip_convergence_seconds", nil); c != 1 {
		t.Fatalf("convergence hist: want 1, got %v", c)
	}
}

func TestGossipTelemetry_NilSafe(t *testing.T) {
	tt := newTelemetry(nil, nil, nil)
	tt.recordMembers("alive", 1)
	tt.recordJoin(nil)
	tt.recordLeave()
	tt.recordMessage("ping", "tx", 0)
	tt.recordProbe(nil, time.Millisecond)
	tt.recordProbeFailure("n")
	tt.recordSuspicionOutcome("alive")
	tt.recordConvergence(time.Second)
}

func TestGossipTelemetry_Spans(t *testing.T) {
	tt, _ := newTestTel(t)
	ctx, span := tt.startSpan(context.Background(), "gossip.test")
	if span == nil {
		t.Fatal("expected non-nil span")
	}

	tt.setSpanError(span, errSomething)
	tt.setSpanError(span, nil) // nil-error path is a no-op
	span.End()

	// nil-context path must not panic and must still return a usable span.
	var nilCtx context.Context
	_, span2 := tt.startSpan(nilCtx, "gossip.test")
	if span2 == nil {
		t.Fatal("expected non-nil span from nil ctx")
	}
	span2.End()

	_ = ctx
}

var errSomething = errStub("boom")

type errStub string

func (e errStub) Error() string { return string(e) }
