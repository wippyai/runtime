// SPDX-License-Identifier: MPL-2.0

package global

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/pid"
)

// openPending applies a fresh pending and returns its epoch.
func openPending(t *testing.T, fsm *FSM, name string, p pid.PID, node pid.NodeID, required []pid.NodeID, index uint64) uint64 {
	t.Helper()
	resp := applyAt(t, fsm, &Command{
		Type:             CmdRegisterPending,
		Name:             name,
		PID:              p,
		NodeID:           node,
		RequiredNodes:    required,
		DeadlineUnixNano: 1 << 62,
	}, index)
	rr, ok := resp.(*RegisterResult)
	require.True(t, ok)
	require.Nil(t, rr.Err)
	return rr.FenceToken
}

// --- B: CmdDropRequired ---

// TestDropRequired_PromotesWhenAcksCoverReduced proves that dropping a departed
// required node lets the existing acks cover the reduced set and promotes the
// entry to active in the same Apply.
func TestDropRequired_PromotesWhenAcksCoverReduced(t *testing.T) {
	fsm := NewFSM()
	p := makePID("node-1", "worker", "p1")
	epoch := openPending(t, fsm, "root.drop", p, "node-1",
		[]pid.NodeID{"node-1", "node-2", "node-3"}, 10)

	applyAt(t, fsm, &Command{Type: CmdRegisterAck, Name: "root.drop", Epoch: epoch, AckerNode: "node-1"}, 11)
	applyAt(t, fsm, &Command{Type: CmdRegisterAck, Name: "root.drop", Epoch: epoch, AckerNode: "node-3"}, 12)

	require.NotNil(t, fsm.State().pendingByName("root.drop"), "still pending while node-2 required")

	resp := applyAt(t, fsm, &Command{Type: CmdDropRequired, Name: "root.drop", Epoch: epoch, NodeID: "node-2"}, 13)
	dr, ok := resp.(*DropRequiredResult)
	require.True(t, ok, "drop response must be typed, got %T", resp)
	assert.True(t, dr.Dropped)
	assert.True(t, dr.Activated, "acks now cover the reduced required set")

	assert.Nil(t, fsm.State().pendingByName("root.drop"), "promoted out of pending")
	gotPID, gotIdx, found := fsm.State().LookupWithIndex("root.drop")
	require.True(t, found)
	assert.Equal(t, p, gotPID)
	assert.Equal(t, uint64(13), gotIdx, "activation index is the drop apply index")
}

// TestDropRequired_NoPromoteWhenStillMissing verifies dropping one node when
// another is still unacked keeps the entry pending.
func TestDropRequired_NoPromoteWhenStillMissing(t *testing.T) {
	fsm := NewFSM()
	p := makePID("node-1", "worker", "p1")
	epoch := openPending(t, fsm, "root.partial", p, "node-1",
		[]pid.NodeID{"node-1", "node-2", "node-3"}, 20)

	applyAt(t, fsm, &Command{Type: CmdRegisterAck, Name: "root.partial", Epoch: epoch, AckerNode: "node-1"}, 21)

	resp := applyAt(t, fsm, &Command{Type: CmdDropRequired, Name: "root.partial", Epoch: epoch, NodeID: "node-2"}, 22)
	dr := resp.(*DropRequiredResult)
	assert.True(t, dr.Dropped)
	assert.False(t, dr.Activated, "node-3 still unacked")

	pv := fsm.State().pendingByName("root.partial")
	require.NotNil(t, pv)
	assert.Equal(t, []pid.NodeID{"node-1", "node-3"}, pv.RequiredNodes)
}

// TestDropRequired_Idempotent proves a duplicate drop is harmless and never
// double-promotes.
func TestDropRequired_Idempotent(t *testing.T) {
	fsm := NewFSM()
	p := makePID("node-1", "worker", "p1")
	epoch := openPending(t, fsm, "root.dupdrop", p, "node-1",
		[]pid.NodeID{"node-1", "node-2"}, 30)
	applyAt(t, fsm, &Command{Type: CmdRegisterAck, Name: "root.dupdrop", Epoch: epoch, AckerNode: "node-1"}, 31)

	r1 := applyAt(t, fsm, &Command{Type: CmdDropRequired, Name: "root.dupdrop", Epoch: epoch, NodeID: "node-2"}, 32).(*DropRequiredResult)
	assert.True(t, r1.Activated, "single ack now covers reduced set {node-1}")

	// Re-issuing the same drop on an already-promoted entry is a no-op.
	r2 := applyAt(t, fsm, &Command{Type: CmdDropRequired, Name: "root.dupdrop", Epoch: epoch, NodeID: "node-2"}, 33).(*DropRequiredResult)
	assert.False(t, r2.Dropped, "entry no longer pending")
	assert.False(t, r2.Activated)

	_, idx, found := fsm.State().LookupWithIndex("root.dupdrop")
	require.True(t, found)
	assert.Equal(t, uint64(32), idx, "no re-promote at the second drop index")
}

// TestDropRequired_ThenStaleAck verifies an ack from the dropped node after the
// drop does not resurrect or re-promote.
func TestDropRequired_ThenStaleAck(t *testing.T) {
	fsm := NewFSM()
	p := makePID("node-1", "worker", "p1")
	epoch := openPending(t, fsm, "root.stale", p, "node-1",
		[]pid.NodeID{"node-1", "node-2"}, 40)
	applyAt(t, fsm, &Command{Type: CmdRegisterAck, Name: "root.stale", Epoch: epoch, AckerNode: "node-1"}, 41)

	applyAt(t, fsm, &Command{Type: CmdDropRequired, Name: "root.stale", Epoch: epoch, NodeID: "node-2"}, 42)
	_, idx, found := fsm.State().LookupWithIndex("root.stale")
	require.True(t, found)

	// Late ack from the dropped node: entry is already active, ack is a no-op.
	resp := applyAt(t, fsm, &Command{Type: CmdRegisterAck, Name: "root.stale", Epoch: epoch, AckerNode: "node-2"}, 43)
	ack := resp.(*AckResult)
	assert.False(t, ack.Recognized, "no pending entry to ack")

	_, idx2, _ := fsm.State().LookupWithIndex("root.stale")
	assert.Equal(t, idx, idx2, "activation index unchanged")
}

// TestAck_ThenDrop verifies an ack-then-drop ordering promotes correctly once.
func TestAck_ThenDrop(t *testing.T) {
	fsm := NewFSM()
	p := makePID("node-1", "worker", "p1")
	epoch := openPending(t, fsm, "root.ackdrop", p, "node-1",
		[]pid.NodeID{"node-1", "node-2", "node-3"}, 50)
	applyAt(t, fsm, &Command{Type: CmdRegisterAck, Name: "root.ackdrop", Epoch: epoch, AckerNode: "node-1"}, 51)
	applyAt(t, fsm, &Command{Type: CmdRegisterAck, Name: "root.ackdrop", Epoch: epoch, AckerNode: "node-2"}, 52)

	// Drop node-3 (never acked) -> acks {node-1,node-2} cover reduced set.
	dr := applyAt(t, fsm, &Command{Type: CmdDropRequired, Name: "root.ackdrop", Epoch: epoch, NodeID: "node-3"}, 53).(*DropRequiredResult)
	assert.True(t, dr.Activated)
	_, found := fsm.State().Lookup("root.ackdrop")
	assert.True(t, found)
}

// TestDropRequired_WrongEpoch verifies a drop carrying a stale epoch is ignored.
func TestDropRequired_WrongEpoch(t *testing.T) {
	fsm := NewFSM()
	p := makePID("node-1", "worker", "p1")
	epoch := openPending(t, fsm, "root.epoch", p, "node-1",
		[]pid.NodeID{"node-1", "node-2"}, 60)

	resp := applyAt(t, fsm, &Command{Type: CmdDropRequired, Name: "root.epoch", Epoch: epoch + 999, NodeID: "node-2"}, 61)
	dr := resp.(*DropRequiredResult)
	assert.False(t, dr.Dropped)

	pv := fsm.State().pendingByName("root.epoch")
	require.NotNil(t, pv)
	assert.Equal(t, []pid.NodeID{"node-1", "node-2"}, pv.RequiredNodes, "required set unchanged")
}

// --- Active-name terminal release events ---

// promoteToActive opens a single-required pending and acks it to active,
// returning the activation index. The promoted active entry carries
// RequiredNodes so a terminal removal can deliver an exclusion release.
func promoteToActive(t *testing.T, fsm *FSM, name string, p pid.PID, required []pid.NodeID, openIdx uint64) {
	t.Helper()
	epoch := openPending(t, fsm, name, p, p.Node, required, openIdx)
	for i, n := range required {
		applyAt(t, fsm, &Command{Type: CmdRegisterAck, Name: name, Epoch: epoch, AckerNode: n}, openIdx+uint64(i)+1)
	}
	_, found := fsm.State().Lookup(name)
	require.True(t, found, "entry promoted to active")
}

// TestActiveTerminal_PidExitFiresRelease proves that a process exit removing a
// promoted Strong name fires an ExpiredEvent carrying RequiredNodes so the
// exclusion can be released on the holders.
func TestActiveTerminal_PidExitFiresRelease(t *testing.T) {
	fsm := NewFSM()
	var got []ExpiredEvent
	fsm.SetOnExpired(func(ev ExpiredEvent) { got = append(got, ev) })

	p := makePID("node-1", "worker", "p1")
	required := []pid.NodeID{"node-1", "node-2"}
	promoteToActive(t, fsm, "root.pidexit", p, required, 400)

	applyAt(t, fsm, &Command{Type: CmdRemovePID, PID: p}, 410)

	require.Len(t, got, 1, "active-name pid exit fires exactly one terminal release")
	assert.Equal(t, "root.pidexit", got[0].Name)
	assert.Equal(t, required, got[0].RequiredNodes, "release carries the holder set")
}

// TestActiveTerminal_ConsistentUnregisterFiresRelease proves a Consistent
// unregister of a promoted Strong name still releases the exclusion holders.
func TestActiveTerminal_ConsistentUnregisterFiresRelease(t *testing.T) {
	fsm := NewFSM()
	var got []ExpiredEvent
	fsm.SetOnExpired(func(ev ExpiredEvent) { got = append(got, ev) })

	p := makePID("node-1", "worker", "p1")
	required := []pid.NodeID{"node-1", "node-2"}
	promoteToActive(t, fsm, "root.cunreg", p, required, 420)

	applyAt(t, fsm, &Command{Type: CmdUnregister, Name: "root.cunreg"}, 430)

	require.Len(t, got, 1)
	assert.Equal(t, required, got[0].RequiredNodes)
}

// TestActiveTerminal_NodeRemovedFiresRelease proves removing a departed node's
// promoted Strong name releases the exclusion on the surviving holders.
func TestActiveTerminal_NodeRemovedFiresRelease(t *testing.T) {
	fsm := NewFSM()
	var got []ExpiredEvent
	fsm.SetOnExpired(func(ev ExpiredEvent) { got = append(got, ev) })

	p := makePID("node-1", "worker", "p1")
	required := []pid.NodeID{"node-1", "node-2"}
	promoteToActive(t, fsm, "root.noderm", p, required, 440)

	applyAt(t, fsm, &Command{Type: CmdRemoveNode, NodeID: "node-1"}, 450)

	require.Len(t, got, 1)
	assert.Equal(t, "root.noderm", got[0].Name)
	assert.Equal(t, required, got[0].RequiredNodes)
}

// --- A-hook: CmdRegisterReject ---

// TestRegisterReject_TerminalConflict proves a reject from a required node moves
// the pending to a terminal failed state distinct from a timeout expiry.
func TestRegisterReject_TerminalConflict(t *testing.T) {
	fsm := NewFSM()
	p := makePID("node-1", "worker", "p1")
	var got ExpiredEvent
	fsm.SetOnExpired(func(ev ExpiredEvent) { got = ev })

	epoch := openPending(t, fsm, "root.reject", p, "node-1",
		[]pid.NodeID{"node-1", "node-2", "node-3"}, 70)

	resp := applyAt(t, fsm, &Command{
		Type:      CmdRegisterReject,
		Name:      "root.reject",
		Epoch:     epoch,
		AckerNode: "node-2",
		Reason:    strongRejectConflict,
	}, 71)
	rr := resp.(*RejectResult)
	assert.True(t, rr.Rejected)

	assert.Nil(t, fsm.State().pendingByName("root.reject"), "removed from pending")
	_, found := fsm.State().Lookup("root.reject")
	assert.False(t, found, "rejected reservation never becomes authoritative")

	assert.Equal(t, strongRejectConflict, got.Reason)
	assert.Equal(t, pid.NodeID("node-2"), got.RejectedBy)

	hist := fsm.State().expiredSnapshot()
	require.Len(t, hist, 1)
	assert.Equal(t, strongRejectConflict, hist[0].Reason)
}

// TestRegisterReject_DominatesAcks verifies NACK dominates: a later ack on a
// rejected entry is a no-op and never resurrects/promotes it.
func TestRegisterReject_DominatesAcks(t *testing.T) {
	fsm := NewFSM()
	p := makePID("node-1", "worker", "p1")
	epoch := openPending(t, fsm, "root.dom", p, "node-1",
		[]pid.NodeID{"node-1", "node-2", "node-3"}, 80)

	// node-1 acks first.
	applyAt(t, fsm, &Command{Type: CmdRegisterAck, Name: "root.dom", Epoch: epoch, AckerNode: "node-1"}, 81)
	// node-2 rejects -> terminal.
	applyAt(t, fsm, &Command{Type: CmdRegisterReject, Name: "root.dom", Epoch: epoch, AckerNode: "node-2", Reason: strongRejectConflict}, 82)

	// Late acks from node-1 and node-3 must be no-ops.
	a3 := applyAt(t, fsm, &Command{Type: CmdRegisterAck, Name: "root.dom", Epoch: epoch, AckerNode: "node-3"}, 83).(*AckResult)
	assert.False(t, a3.Recognized)

	assert.Nil(t, fsm.State().pendingByName("root.dom"))
	_, found := fsm.State().Lookup("root.dom")
	assert.False(t, found, "must never resurrect after reject")
}

// TestRegisterReject_WrongEpoch verifies a stale-epoch reject is ignored.
func TestRegisterReject_WrongEpoch(t *testing.T) {
	fsm := NewFSM()
	p := makePID("node-1", "worker", "p1")
	epoch := openPending(t, fsm, "root.rej-epoch", p, "node-1",
		[]pid.NodeID{"node-1", "node-2"}, 90)

	resp := applyAt(t, fsm, &Command{Type: CmdRegisterReject, Name: "root.rej-epoch", Epoch: epoch + 1, AckerNode: "node-2", Reason: strongRejectConflict}, 91)
	rr := resp.(*RejectResult)
	assert.False(t, rr.Rejected)
	require.NotNil(t, fsm.State().pendingByName("root.rej-epoch"), "stale reject leaves the entry pending")
}
