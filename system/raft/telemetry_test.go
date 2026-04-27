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
	tt.recordCommitIndex(1)
	tt.recordLastLogIndex("node-a", 1)
	tt.recordLogLag("node-a", 0)
	tt.recordAppendEntries("peer", nil, time.Millisecond)
	tt.recordVoterLadder(1, 0, 5)
	tt.recordSnapshot(nil, time.Millisecond, 0)
}

func TestRaftTelemetry_CommitLog(t *testing.T) {
	tt, rec := newTestTel(t)
	tt.recordCommitIndex(100)
	tt.recordLastLogIndex("node-a", 95)
	tt.recordLogLag("node-a", 5)
	if v := rec.GaugeValue("raft_commit_index", nil); v != 100 {
		t.Fatalf("raft_commit_index: want 100, got %v", v)
	}
	if v := rec.GaugeValue("raft_last_log_index", metrics.Labels{"node": "node-a"}); v != 95 {
		t.Fatalf("raft_last_log_index: want 95, got %v", v)
	}
	if v := rec.GaugeValue("raft_log_lag", metrics.Labels{"node": "node-a"}); v != 5 {
		t.Fatalf("raft_log_lag: want 5, got %v", v)
	}
}

func TestRaftTelemetry_AppendEntries(t *testing.T) {
	tt, rec := newTestTel(t)
	tt.recordAppendEntries("node-b", nil, 10*time.Millisecond)
	tt.recordAppendEntries("node-b", errSomething, 0)
	if v := rec.CounterValue("raft_append_entries_total", metrics.Labels{"peer": "node-b", "result": "ok"}); v != 1 {
		t.Fatalf("AE ok counter: want 1, got %v", v)
	}
	if v := rec.CounterValue("raft_append_entries_total", metrics.Labels{"peer": "node-b", "result": "err"}); v != 1 {
		t.Fatalf("AE err counter: want 1, got %v", v)
	}
	if c := rec.HistogramCount("raft_append_entries_duration_seconds", metrics.Labels{"peer": "node-b", "result": "ok"}); c != 1 {
		t.Fatalf("AE duration count: want 1, got %v", c)
	}
}

func TestRaftTelemetry_VoterLadder(t *testing.T) {
	tt, rec := newTestTel(t)
	tt.recordVoterLadder(3, 2, 5)
	if v := rec.GaugeValue("raft_voters", nil); v != 3 {
		t.Fatalf("raft_voters: want 3, got %v", v)
	}
	if v := rec.GaugeValue("raft_non_voters", nil); v != 2 {
		t.Fatalf("raft_non_voters: want 2, got %v", v)
	}
	if v := rec.GaugeValue("raft_voter_cap", nil); v != 5 {
		t.Fatalf("raft_voter_cap: want 5, got %v", v)
	}
}

func TestRaftTelemetry_Snapshot(t *testing.T) {
	tt, rec := newTestTel(t)
	tt.recordSnapshot(nil, 100*time.Millisecond, 4096)
	if v := rec.CounterValue("raft_snapshot_total", metrics.Labels{"result": "ok"}); v != 1 {
		t.Fatalf("raft_snapshot_total: want 1, got %v", v)
	}
	if c := rec.HistogramCount("raft_snapshot_duration_seconds", nil); c != 1 {
		t.Fatalf("raft_snapshot_duration_seconds: want 1, got %v", c)
	}
	if c := rec.HistogramCount("raft_snapshot_size_bytes", nil); c != 1 {
		t.Fatalf("raft_snapshot_size_bytes: want 1, got %v", c)
	}
}

var errSomething = errStub("boom")

type errStub string

func (e errStub) Error() string { return string(e) }
