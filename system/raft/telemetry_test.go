// SPDX-License-Identifier: MPL-2.0

package raft

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

func TestRaftTelemetry_State(t *testing.T) {
	tt, rec := newTestTel(t)
	tt.recordState("node-a", "leader")
	if v := rec.GaugeValue("raft_state", metrics.Labels{"node": "node-a"}); v != 2 {
		t.Fatalf("raft_state(leader): want 2, got %v", v)
	}
	tt.recordState("node-a", "candidate")
	if v := rec.GaugeValue("raft_state", metrics.Labels{"node": "node-a"}); v != 1 {
		t.Fatalf("raft_state(candidate): want 1, got %v", v)
	}
	tt.recordState("node-a", "follower")
	if v := rec.GaugeValue("raft_state", metrics.Labels{"node": "node-a"}); v != 0 {
		t.Fatalf("raft_state(follower): want 0, got %v", v)
	}
}

func TestRaftTelemetry_TermAndLeaderChange(t *testing.T) {
	tt, rec := newTestTel(t)
	tt.recordTerm(5)
	if v := rec.GaugeValue("raft_term", nil); v != 5 {
		t.Fatalf("raft_term: want 5, got %v", v)
	}
	tt.recordLeaderChange()
	tt.recordLeaderChange()
	if v := rec.CounterValue("raft_leader_changes_total", nil); v != 2 {
		t.Fatalf("raft_leader_changes_total: want 2, got %v", v)
	}
}

func TestRaftTelemetry_Election(t *testing.T) {
	tt, rec := newTestTel(t)
	tt.recordElection(50 * time.Millisecond)
	if c := rec.HistogramCount("raft_election_duration_seconds", nil); c != 1 {
		t.Fatalf("raft_election_duration_seconds: want 1, got %v", c)
	}
}

func TestRaftTelemetry_NilSafe(t *testing.T) {
	tt := newTelemetry(nil, nil, nil)
	tt.recordState("node-a", "leader")
	tt.recordTerm(1)
	tt.recordLeaderChange()
	tt.recordElection(time.Millisecond)
}
