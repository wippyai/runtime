// SPDX-License-Identifier: MPL-2.0

package membership

import (
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

func TestGossipTelemetry_NilSafe(t *testing.T) {
	tt := newTelemetry(nil, nil, nil)
	tt.recordMembers("alive", 1)
	tt.recordJoin(nil)
	tt.recordLeave()
	tt.recordMessage("ping", "tx", 0)
	_ = time.Now
}

var errSomething = errStub("boom")

type errStub string

func (e errStub) Error() string { return string(e) }
