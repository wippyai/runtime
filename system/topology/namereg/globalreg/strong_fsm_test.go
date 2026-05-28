// SPDX-License-Identifier: MPL-2.0

package globalreg

import (
	"bytes"
	"io"
	"testing"

	hraft "github.com/hashicorp/raft"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/topology/namereg/globalreg"
	"github.com/wippyai/runtime/api/pid"
)

// memSink is an in-memory hraft.SnapshotSink used to round-trip FSM
// snapshots through Persist / Restore without going through disk.
type memSink struct {
	*bytes.Buffer
	cancelled bool
}

func (m *memSink) Close() error                { return nil }
func (m *memSink) ID() string                  { return "mem" }
func (m *memSink) Cancel() error               { m.cancelled = true; return nil }
func (m *memSink) Write(p []byte) (int, error) { return m.Buffer.Write(p) }

type nopReadCloser struct{ io.Reader }

func (nopReadCloser) Close() error { return nil }

// applyAt is a small wrapper to apply a command at a specific Raft log
// index — the FSM treats the index as both the fence token and the
// epoch for Strong registrations.
func applyAt(t *testing.T, fsm *FSM, cmd *Command, index uint64) any {
	t.Helper()
	data, err := EncodeCommand(cmd)
	require.NoError(t, err)
	return fsm.Apply(&hraft.Log{Data: data, Index: index})
}

// TestStrongPending_HappyPath drives a pending → ack → active transition
// across three live nodes. The FSM must record the ack set, promote to
// active exactly when the set is full, and use the activation index as
// the fence token.
func TestStrongPending_HappyPath(t *testing.T) {
	fsm := NewFSM()
	p := makePID("node-1", "worker", "p1")
	required := []pid.NodeID{"node-1", "node-2", "node-3"}

	resp := applyAt(t, fsm, &Command{
		Type:             CmdRegisterPending,
		Name:             "root.svc",
		PID:              p,
		NodeID:           "node-1",
		RequiredNodes:    required,
		DeadlineUnixNano: 1 << 62,
	}, 10)
	rr, ok := resp.(*RegisterResult)
	require.True(t, ok)
	require.Nil(t, rr.Err)
	require.Equal(t, uint64(10), rr.FenceToken, "pending epoch should equal commit index")

	pv := fsm.State().pendingByName("root.svc")
	require.NotNil(t, pv)
	assert.Equal(t, uint64(10), pv.Epoch)
	assert.Equal(t, required, pv.RequiredNodes)
	assert.Len(t, pv.AckSet, 0)

	for i, n := range required {
		resp := applyAt(t, fsm, &Command{
			Type:      CmdRegisterAck,
			Name:      "root.svc",
			Epoch:     10,
			AckerNode: n,
		}, uint64(11+i))
		ack, ok := resp.(*AckResult)
		require.True(t, ok, "ack response must be typed")
		assert.True(t, ack.Recognized)
		if i < len(required)-1 {
			assert.False(t, ack.Complete, "not yet complete at i=%d", i)
		} else {
			assert.True(t, ack.Complete)
			assert.True(t, ack.Activated)
		}
	}

	pv = fsm.State().pendingByName("root.svc")
	assert.Nil(t, pv, "pending entry must be removed after activation")

	gotPID, gotIdx, found := fsm.State().LookupWithIndex("root.svc")
	require.True(t, found)
	assert.Equal(t, p, gotPID)
	assert.Equal(t, uint64(13), gotIdx, "fence token equals the activation index (last ack)")
}

// TestStrongPending_ExpiryMissingOneAck verifies that the FSM releases the
// reservation on expiry and reports exactly the missing node.
func TestStrongPending_ExpiryMissingOneAck(t *testing.T) {
	fsm := NewFSM()
	p := makePID("node-1", "worker", "p1")
	required := []pid.NodeID{"node-1", "node-2", "node-3"}

	applyAt(t, fsm, &Command{
		Type:             CmdRegisterPending,
		Name:             "root.svc",
		PID:              p,
		NodeID:           "node-1",
		RequiredNodes:    required,
		DeadlineUnixNano: 1 << 62,
	}, 20)

	applyAt(t, fsm, &Command{Type: CmdRegisterAck, Name: "root.svc", Epoch: 20, AckerNode: "node-1"}, 21)
	applyAt(t, fsm, &Command{Type: CmdRegisterAck, Name: "root.svc", Epoch: 20, AckerNode: "node-3"}, 22)

	resp := applyAt(t, fsm, &Command{
		Type:   CmdRegisterExpired,
		Name:   "root.svc",
		Epoch:  20,
		Reason: "missing_ack",
	}, 23)
	er, ok := resp.(*ExpireResult)
	require.True(t, ok)
	require.True(t, er.Removed)
	require.Equal(t, []pid.NodeID{"node-2"}, er.MissingAcks)

	_, _, found := fsm.State().LookupWithIndex("root.svc")
	assert.False(t, found, "expired reservation must not become authoritative")

	expired := fsm.State().expiredSnapshot()
	require.Len(t, expired, 1)
	assert.Equal(t, "root.svc", expired[0].Name)
	assert.Equal(t, "missing_ack", expired[0].Reason)
	assert.Equal(t, []pid.NodeID{"node-2"}, expired[0].MissingAcks)
}

// TestStrongPending_ConcurrentConflict proves that two different PIDs
// cannot both reserve the same strong name: the second loses.
func TestStrongPending_ConcurrentConflict(t *testing.T) {
	fsm := NewFSM()
	pa := makePID("node-1", "worker", "a")
	pb := makePID("node-2", "worker", "b")
	required := []pid.NodeID{"node-1", "node-2"}

	resp1 := applyAt(t, fsm, &Command{
		Type:          CmdRegisterPending,
		Name:          "root.racy",
		PID:           pa,
		NodeID:        "node-1",
		RequiredNodes: required,
	}, 30)
	r1, ok := resp1.(*RegisterResult)
	require.True(t, ok)
	require.Nil(t, r1.Err)

	resp2 := applyAt(t, fsm, &Command{
		Type:          CmdRegisterPending,
		Name:          "root.racy",
		PID:           pb,
		NodeID:        "node-2",
		RequiredNodes: required,
	}, 31)
	r2, ok := resp2.(*RegisterResult)
	require.True(t, ok)
	require.NotNil(t, r2.Err)
	require.Equal(t, globalreg.ErrPendingConflict, r2.Err)
	assert.Equal(t, pa, r2.ExistingPID)
}

// TestStrongPending_DeterministicSnapshot proves that a replay of the same
// pending command yields the same RequiredNodes view on every replica.
func TestStrongPending_DeterministicSnapshot(t *testing.T) {
	required := []pid.NodeID{"alpha", "beta", "gamma"}
	encoded, err := EncodeCommand(&Command{
		Type:          CmdRegisterPending,
		Name:          "root.snap",
		PID:           makePID("alpha", "worker", "x"),
		NodeID:        "alpha",
		RequiredNodes: required,
	})
	require.NoError(t, err)

	apply := func() []pid.NodeID {
		fsm := NewFSM()
		fsm.Apply(&hraft.Log{Data: encoded, Index: 42})
		v := fsm.State().pendingByName("root.snap")
		require.NotNil(t, v)
		out := make([]pid.NodeID, len(v.RequiredNodes))
		copy(out, v.RequiredNodes)
		return out
	}

	a := apply()
	b := apply()
	require.Equal(t, a, b, "RequiredNodes must be deterministic across replicas")
}

// TestStrongPending_IdempotentAcks verifies that re-applying the same ack
// does not double-count nor re-trigger activation.
func TestStrongPending_IdempotentAcks(t *testing.T) {
	fsm := NewFSM()
	required := []pid.NodeID{"node-1", "node-2"}
	applyAt(t, fsm, &Command{
		Type:          CmdRegisterPending,
		Name:          "root.idem",
		PID:           makePID("node-1", "worker", "p"),
		NodeID:        "node-1",
		RequiredNodes: required,
	}, 50)

	applyAt(t, fsm, &Command{Type: CmdRegisterAck, Name: "root.idem", Epoch: 50, AckerNode: "node-1"}, 51)
	res := applyAt(t, fsm, &Command{Type: CmdRegisterAck, Name: "root.idem", Epoch: 50, AckerNode: "node-1"}, 52)
	ack, ok := res.(*AckResult)
	require.True(t, ok)
	assert.True(t, ack.Recognized)
	assert.False(t, ack.Complete, "duplicate ack must not complete the set")

	applyAt(t, fsm, &Command{Type: CmdRegisterAck, Name: "root.idem", Epoch: 50, AckerNode: "node-2"}, 53)

	_, idx, found := fsm.State().LookupWithIndex("root.idem")
	require.True(t, found, "activated entry should be looked up")
	assert.Equal(t, uint64(53), idx)
}

// TestStrongPending_SnapshotRoundTrip verifies that pending reservations
// survive a Snapshot/Restore cycle.
func TestStrongPending_SnapshotRoundTrip(t *testing.T) {
	fsm := NewFSM()
	required := []pid.NodeID{"a", "b", "c"}
	applyAt(t, fsm, &Command{
		Type:             CmdRegisterPending,
		Name:             "root.snap-rt",
		PID:              makePID("a", "h", "1"),
		NodeID:           "a",
		RequiredNodes:    required,
		DeadlineUnixNano: 1234567,
	}, 70)
	applyAt(t, fsm, &Command{Type: CmdRegisterAck, Name: "root.snap-rt", Epoch: 70, AckerNode: "a"}, 71)

	snap, err := fsm.Snapshot()
	require.NoError(t, err)
	buf := &bytes.Buffer{}
	sink := &memSink{Buffer: buf}
	require.NoError(t, snap.Persist(sink))

	dst := NewFSM()
	require.NoError(t, dst.Restore(nopReadCloser{Reader: bytes.NewReader(buf.Bytes())}))

	pv := dst.State().pendingByName("root.snap-rt")
	require.NotNil(t, pv)
	assert.Equal(t, uint64(70), pv.Epoch)
	assert.Equal(t, required, pv.RequiredNodes)
	assert.Equal(t, []pid.NodeID{"a"}, pv.AckSet)
	assert.Equal(t, int64(1234567), pv.DeadlineUnixNano)
}
