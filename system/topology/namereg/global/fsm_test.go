// SPDX-License-Identifier: MPL-2.0

package global

import (
	"bytes"
	"testing"

	hraft "github.com/hashicorp/raft"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/internal/telemetrytest"
)

func makePID(node, host, uniq string) pid.PID {
	return pid.PID{Node: node, Host: host, UniqID: uniq}
}

func applyCmd(t *testing.T, fsm *FSM, cmd *Command) any {
	t.Helper()
	data, err := EncodeCommand(cmd)
	require.NoError(t, err)
	return fsm.Apply(&hraft.Log{Data: data, Index: 1})
}

func TestFSM_Register(t *testing.T) {
	fsm := NewFSM()
	p := makePID("node1", "host1", "pid1")

	resp := applyCmd(t, fsm, &Command{Type: CmdRegister, Name: "svc", PID: p, NodeID: "node1"})
	result, ok := resp.(*RegisterResult)
	require.True(t, ok)
	assert.Nil(t, result.Err)
	assert.Equal(t, p, result.PID)

	// Verify lookup.
	found, ok := fsm.State().Lookup("svc")
	assert.True(t, ok)
	assert.Equal(t, p, found)
}

func TestFSM_Register_Idempotent(t *testing.T) {
	fsm := NewFSM()
	p := makePID("node1", "host1", "pid1")

	applyCmd(t, fsm, &Command{Type: CmdRegister, Name: "svc", PID: p, NodeID: "node1"})

	// Re-register same name+PID should succeed.
	resp := applyCmd(t, fsm, &Command{Type: CmdRegister, Name: "svc", PID: p, NodeID: "node1"})
	result := resp.(*RegisterResult)
	assert.Nil(t, result.Err)
	assert.Equal(t, p, result.PID)
}

func TestFSM_Register_Conflict(t *testing.T) {
	fsm := NewFSM()
	p1 := makePID("node1", "host1", "pid1")
	p2 := makePID("node1", "host1", "pid2")

	applyCmd(t, fsm, &Command{Type: CmdRegister, Name: "svc", PID: p1, NodeID: "node1"})

	resp := applyCmd(t, fsm, &Command{Type: CmdRegister, Name: "svc", PID: p2, NodeID: "node1"})
	result := resp.(*RegisterResult)
	assert.NotNil(t, result.Err)
	assert.Equal(t, p1, result.ExistingPID)
}

func TestFSM_Unregister(t *testing.T) {
	fsm := NewFSM()
	p := makePID("node1", "host1", "pid1")

	applyCmd(t, fsm, &Command{Type: CmdRegister, Name: "svc", PID: p, NodeID: "node1"})

	resp := applyCmd(t, fsm, &Command{Type: CmdUnregister, Name: "svc"})
	result := resp.(*UnregisterResult)
	assert.True(t, result.Removed)

	_, ok := fsm.State().Lookup("svc")
	assert.False(t, ok)
}

func TestFSM_Unregister_NotFound(t *testing.T) {
	fsm := NewFSM()

	resp := applyCmd(t, fsm, &Command{Type: CmdUnregister, Name: "nonexistent"})
	result := resp.(*UnregisterResult)
	assert.False(t, result.Removed)
}

func TestFSM_RemovePID(t *testing.T) {
	fsm := NewFSM()
	p := makePID("node1", "host1", "pid1")

	applyCmd(t, fsm, &Command{Type: CmdRegister, Name: "svc1", PID: p, NodeID: "node1"})
	applyCmd(t, fsm, &Command{Type: CmdRegister, Name: "svc2", PID: p, NodeID: "node1"})
	applyCmd(t, fsm, &Command{Type: CmdRegister, Name: "svc3", PID: p, NodeID: "node1"})

	resp := applyCmd(t, fsm, &Command{Type: CmdRemovePID, PID: p})
	result := resp.(*RemoveResult)
	assert.Equal(t, 3, result.Count)

	// All names should be gone.
	_, ok := fsm.State().Lookup("svc1")
	assert.False(t, ok)
	_, ok = fsm.State().Lookup("svc2")
	assert.False(t, ok)
	_, ok = fsm.State().Lookup("svc3")
	assert.False(t, ok)
}

func TestFSM_RemoveNode(t *testing.T) {
	fsm := NewFSM()
	p1 := makePID("node1", "host1", "pid1")
	p2 := makePID("node1", "host1", "pid2")
	p3 := makePID("node2", "host1", "pid3")

	applyCmd(t, fsm, &Command{Type: CmdRegister, Name: "svc1", PID: p1, NodeID: "node1"})
	applyCmd(t, fsm, &Command{Type: CmdRegister, Name: "svc2", PID: p2, NodeID: "node1"})
	applyCmd(t, fsm, &Command{Type: CmdRegister, Name: "svc3", PID: p3, NodeID: "node2"})

	resp := applyCmd(t, fsm, &Command{Type: CmdRemoveNode, NodeID: "node1"})
	result := resp.(*RemoveResult)
	assert.Equal(t, 2, result.Count)

	// node1 names should be gone.
	_, ok := fsm.State().Lookup("svc1")
	assert.False(t, ok)
	_, ok = fsm.State().Lookup("svc2")
	assert.False(t, ok)

	// node2 name should still exist.
	found, ok := fsm.State().Lookup("svc3")
	assert.True(t, ok)
	assert.Equal(t, p3, found)
}

func TestFSM_LookupByPID(t *testing.T) {
	fsm := NewFSM()
	p := makePID("node1", "host1", "pid1")

	applyCmd(t, fsm, &Command{Type: CmdRegister, Name: "alpha", PID: p, NodeID: "node1"})
	applyCmd(t, fsm, &Command{Type: CmdRegister, Name: "beta", PID: p, NodeID: "node1"})

	names := fsm.State().LookupByPID(p)
	assert.Len(t, names, 2)
	assert.Contains(t, names, "alpha")
	assert.Contains(t, names, "beta")
}

func TestFSM_Snapshot_Restore(t *testing.T) {
	fsm1 := NewFSM()
	p1 := makePID("node1", "host1", "pid1")
	p2 := makePID("node2", "host2", "pid2")

	applyCmd(t, fsm1, &Command{Type: CmdRegister, Name: "svc1", PID: p1, NodeID: "node1"})
	applyCmd(t, fsm1, &Command{Type: CmdRegister, Name: "svc2", PID: p2, NodeID: "node2"})

	// Take snapshot.
	snap, err := fsm1.Snapshot()
	require.NoError(t, err)

	// Serialize snapshot.
	var buf bytes.Buffer
	sink := &bufSink{buf: &buf}
	err = snap.Persist(sink)
	require.NoError(t, err)

	// Restore into a fresh FSM.
	fsm2 := NewFSM()
	err = fsm2.Restore(&readCloser{buf: &buf})
	require.NoError(t, err)

	// Verify state.
	found, ok := fsm2.State().Lookup("svc1")
	assert.True(t, ok)
	assert.Equal(t, p1, found)
	foundWithIndex, token, ok := fsm2.State().LookupWithIndex("svc1")
	assert.True(t, ok)
	assert.Equal(t, p1, foundWithIndex)
	assert.Equal(t, uint64(1), token)

	found, ok = fsm2.State().Lookup("svc2")
	assert.True(t, ok)
	assert.Equal(t, p2, found)
	foundWithIndex, token, ok = fsm2.State().LookupWithIndex("svc2")
	assert.True(t, ok)
	assert.Equal(t, p2, foundWithIndex)
	assert.Equal(t, uint64(1), token)
}

func TestFSM_ShardDistribution(t *testing.T) {
	// Verify names hash to different shards (statistical check).
	fsm := NewFSM()
	p := makePID("node1", "host1", "pid1")

	for i := 0; i < 100; i++ {
		name := "service_" + string(rune('A'+i%26)) + "_" + string(rune('0'+i/26))
		applyCmd(t, fsm, &Command{Type: CmdRegister, Name: name, PID: p, NodeID: "node1"})
	}

	// Count names per shard.
	shardsUsed := 0
	for i := 0; i < shardCount; i++ {
		fsm.state.shards[i].mu.RLock()
		if len(fsm.state.shards[i].names) > 0 {
			shardsUsed++
		}
		fsm.state.shards[i].mu.RUnlock()
	}

	// With FNV-1a and 100 names across 16 shards, we expect most shards to be used.
	assert.Greater(t, shardsUsed, 4, "expected names to be distributed across multiple shards")
}

func TestFSM_ConcurrentReads(t *testing.T) {
	fsm := NewFSM()
	p := makePID("node1", "host1", "pid1")

	// Register some names.
	for i := 0; i < 50; i++ {
		name := "svc_" + string(rune('a'+i%26))
		applyCmd(t, fsm, &Command{Type: CmdRegister, Name: name, PID: p, NodeID: "node1"})
	}

	// Concurrent reads should not race or panic.
	done := make(chan struct{})
	for g := 0; g < 10; g++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for i := 0; i < 100; i++ {
				name := "svc_" + string(rune('a'+i%26))
				fsm.State().Lookup(name)
				fsm.State().LookupByPID(p)
			}
		}()
	}
	for g := 0; g < 10; g++ {
		<-done
	}
}

func TestCommand_EncodeDecode(t *testing.T) {
	p := makePID("node1", "host1", "pid1")
	original := &Command{
		Type:   CmdRegister,
		Name:   "test_service",
		PID:    p,
		NodeID: "node1",
	}

	data, err := EncodeCommand(original)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	decoded, err := DecodeCommand(data)
	require.NoError(t, err)
	assert.Equal(t, original.Type, decoded.Type)
	assert.Equal(t, original.Name, decoded.Name)
	assert.Equal(t, original.NodeID, decoded.NodeID)
}

func TestFSM_Telemetry_EmitsFenceTokenAndSize(t *testing.T) {
	rec := telemetrytest.NewRecorder()
	fsm := NewFSM()
	fsm.SetTelemetry(newTelemetry(rec, nil, nil, "_test"))

	// Initial size sample fires on SetTelemetry.
	if v := rec.GaugeValue("pg_globalreg_size", nil); v != 0 {
		t.Fatalf("initial pg_globalreg_size: want 0, got %v", v)
	}

	p := makePID("node1", "host1", "pid1")
	data, err := EncodeCommand(&Command{Type: CmdRegister, Name: "svc", PID: p, NodeID: "node1"})
	require.NoError(t, err)
	fsm.Apply(&hraft.Log{Data: data, Index: 42})

	if v := rec.GaugeValue("pg_globalreg_size", nil); v != 1 {
		t.Fatalf("pg_globalreg_size after register: want 1, got %v", v)
	}
	if v := rec.GaugeValue("pg_fence_token", metrics.Labels{"pg": HostID, "node": "node1"}); v != 42 {
		t.Fatalf("pg_fence_token: want 42, got %v", v)
	}
}

func TestFSM_Telemetry_EmitsDedupe(t *testing.T) {
	rec := telemetrytest.NewRecorder()
	fsm := NewFSM()
	fsm.SetTelemetry(newTelemetry(rec, nil, nil, "_test"))

	p := makePID("node1", "host1", "pid1")
	cmd, err := EncodeCommand(&Command{Type: CmdRegister, Name: "svc", PID: p, NodeID: "node1"})
	require.NoError(t, err)
	fsm.Apply(&hraft.Log{Data: cmd, Index: 1})
	// Idempotent re-registration should be counted as dedupe.
	fsm.Apply(&hraft.Log{Data: cmd, Index: 2})

	if v := rec.CounterValue("pg_globalreg_dedupe_total", nil); v != 1 {
		t.Fatalf("pg_globalreg_dedupe_total: want 1, got %v", v)
	}
}

func TestFSM_Telemetry_SizeShrinksOnUnregister(t *testing.T) {
	rec := telemetrytest.NewRecorder()
	fsm := NewFSM()
	fsm.SetTelemetry(newTelemetry(rec, nil, nil, "_test"))

	p := makePID("node1", "host1", "pid1")
	regData, err := EncodeCommand(&Command{Type: CmdRegister, Name: "svc", PID: p, NodeID: "node1"})
	require.NoError(t, err)
	fsm.Apply(&hraft.Log{Data: regData, Index: 1})

	unregData, err := EncodeCommand(&Command{Type: CmdUnregister, Name: "svc"})
	require.NoError(t, err)
	fsm.Apply(&hraft.Log{Data: unregData, Index: 2})

	if v := rec.GaugeValue("pg_globalreg_size", nil); v != 0 {
		t.Fatalf("pg_globalreg_size after unregister: want 0, got %v", v)
	}
}

// --- Test helpers ---

type bufSink struct {
	buf *bytes.Buffer
}

func (s *bufSink) Write(p []byte) (int, error) { return s.buf.Write(p) }
func (s *bufSink) Close() error                { return nil }
func (s *bufSink) ID() string                  { return "test" }
func (s *bufSink) Cancel() error               { return nil }

type readCloser struct {
	buf *bytes.Buffer
}

func (r *readCloser) Read(p []byte) (int, error) { return r.buf.Read(p) }
func (r *readCloser) Close() error               { return nil }
