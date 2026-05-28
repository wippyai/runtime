// SPDX-License-Identifier: MPL-2.0

package globalreg

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/topology/namereg/globalreg"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

// capturingRouter records every package the service sends so tests can assert
// on relay traffic (targets, topics) without a real mesh.
type capturingRouter struct {
	sent []capturedPkg
	mu   sync.Mutex
}

type capturedPkg struct {
	target pid.NodeID
	topic  relay.Topic
	body   []byte
}

func (r *capturingRouter) Send(pkg *relay.Package) error {
	r.mu.Lock()
	for _, m := range pkg.Messages {
		var body []byte
		if len(m.Payloads) > 0 {
			if b, ok := m.Payloads[0].Data().([]byte); ok {
				body = append(body, b...)
			}
		}
		r.sent = append(r.sent, capturedPkg{target: pkg.Target.Node, topic: m.Topic, body: body})
	}
	r.mu.Unlock()
	relay.ReleasePackage(pkg)
	return nil
}

func (r *capturingRouter) byTopic(topic relay.Topic) []capturedPkg {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []capturedPkg
	for _, p := range r.sent {
		if p.topic == topic {
			out = append(out, p)
		}
	}
	return out
}

// --- D: leader stamps RequiredNodes ---

// TestLeaderStampsRequiredNodes proves the LEADER stamps RequiredNodes from its
// own membership at apply time. The caller-provided command omits the set.
func TestLeaderStampsRequiredNodes(t *testing.T) {
	fsm := NewFSM()
	mem := &fakeMembership{local: "node-1", ids: []string{"node-1", "node-2", "node-3"}}
	svc := NewService(newDirectApplyRaft(fsm, true), fsm, &nopBus{}, nil, &nopRouter{}, mem, "node-1", noopLogger(), nil, nil, nil)

	// Caller intent carries NO RequiredNodes; the leader must stamp them.
	cmd := &Command{
		Type:             CmdRegisterPending,
		Name:             "root.stamp",
		PID:              makePID("node-1", "host", "p1"),
		NodeID:           "node-1",
		DeadlineUnixNano: 1 << 62,
	}
	resp, err := svc.applyCommand(cmd)
	require.NoError(t, err)
	rr := resp.(*RegisterResult)
	require.Nil(t, rr.Err)

	pv := fsm.State().pendingByName("root.stamp")
	require.NotNil(t, pv)
	assert.Equal(t, []pid.NodeID{"node-1", "node-2", "node-3"}, pv.RequiredNodes,
		"RequiredNodes stamped from the leader membership")
}

// TestNewLeaderInheritsCommittedRequired proves a committed pending entry keeps
// its original RequiredNodes; the stamping helper never re-stamps it.
func TestNewLeaderInheritsCommittedRequired(t *testing.T) {
	fsm := NewFSM()
	// Open with an explicit committed set of three nodes.
	openPending(t, fsm, "root.inherit", makePID("node-1", "host", "p1"), "node-1",
		[]pid.NodeID{"node-1", "node-2", "node-3"}, 100)

	// A "new leader" with a different (smaller) membership comes up.
	mem := &fakeMembership{local: "node-1", ids: []string{"node-1"}}
	svc := NewService(newDirectApplyRaft(fsm, true), fsm, &nopBus{}, nil, &nopRouter{}, mem, "node-1", noopLogger(), nil, nil, nil)

	// resumeStrongTimers / rebroadcast must not re-apply or re-stamp.
	svc.rebroadcastPending()

	pv := fsm.State().pendingByName("root.inherit")
	require.NotNil(t, pv)
	assert.Equal(t, []pid.NodeID{"node-1", "node-2", "node-3"}, pv.RequiredNodes,
		"committed RequiredNodes inherited, never re-stamped from new-leader membership")
}

// --- C: targeted relay nudge, no raft re-apply ---

// TestRebroadcastSendsTargetedNudge proves rebroadcastPending sends a
// CheckStrongPending relay message to ONLY the missing nodes and does NOT
// re-apply CmdRegisterPending to raft.
func TestRebroadcastSendsTargetedNudge(t *testing.T) {
	fsm := NewFSM()
	mem := &fakeMembership{local: "node-1", ids: []string{"node-1", "node-2", "node-3"}}
	router := &capturingRouter{}
	raftStub := newDirectApplyRaft(fsm, true)
	svc := NewService(raftStub, fsm, &nopBus{}, nil, router, mem, "node-1", noopLogger(), nil, nil, nil)
	// Detach the self-ack hook so seeding state below does not spawn an async
	// raft write that would confound the no-raft-write assertion.
	fsm.SetOnPending(nil)

	// Seed a pending entry with node-1 already acked, leaving {node-2,node-3}
	// missing — exactly the targets rebroadcast must nudge.
	epoch := openPending(t, fsm, "root.nudge", makePID("node-1", "host", "p1"), "node-1",
		[]pid.NodeID{"node-1", "node-2", "node-3"}, 200)
	applyAt(t, fsm, &Command{Type: CmdRegisterAck, Name: "root.nudge", Epoch: epoch, AckerNode: "node-1"}, 201)

	idxBefore := raftStub.idx.Load()
	svc.rebroadcastPending()

	assert.Equal(t, idxBefore, raftStub.idx.Load(), "rebroadcast must not write to raft")

	nudges := router.byTopic(topicCheckPending)
	targets := make(map[pid.NodeID]bool)
	for _, n := range nudges {
		targets[n.target] = true
	}
	assert.True(t, targets["node-2"], "missing node-2 nudged")
	assert.True(t, targets["node-3"], "missing node-3 nudged")
	assert.False(t, targets["node-1"], "acked node-1 not nudged")
}

// TestCheckPendingReAcksIdempotently proves a nudged node, on receiving
// CheckStrongPending, re-runs its ack path which is idempotent via recordAck.
func TestCheckPendingReAcksIdempotently(t *testing.T) {
	fsm := NewFSM()
	mem := &fakeMembership{local: "node-2", ids: []string{"node-1", "node-2"}}
	router := &capturingRouter{}
	// node-2 is a follower (not leader) so its ack flows over the relay.
	raftStub := newDirectApplyRaft(fsm, false)
	raftStub.knownLeader = "node-1"
	svc := NewService(raftStub, fsm, &nopBus{}, nil, router, mem, "node-2", noopLogger(), nil, nil, nil)
	// Detach the open's self-ack so the only relay ack observed is the one the
	// nudge handler produces.
	fsm.SetOnPending(nil)

	// Seed the follower's FSM with the pending entry so its ack path has state.
	epoch := openPending(t, fsm, "root.reack", makePID("node-1", "host", "p1"), "node-1",
		[]pid.NodeID{"node-1", "node-2"}, 300)

	body, err := marshalMsgpack(checkPendingEnvelope{Name: "root.reack", Epoch: epoch})
	require.NoError(t, err)
	pkg := relay.NewServicePackage("node-1", HostID, "node-2", HostID, topicCheckPending, payload.New(body))
	require.NoError(t, svc.Send(pkg))

	// The follower re-sends its ack to the leader over the relay (idempotent).
	acks := router.byTopic(topicRegisterAck)
	require.NotEmpty(t, acks, "nudged follower re-acks via relay")
}

// --- A-hook end to end: caller surfaces conflict distinct from timeout ---

// TestRegisterStrong_RejectSurfacesConflict drives the caller waiting path to a
// reject outcome and asserts the error is a conflict, not a timeout.
func TestRegisterStrong_RejectSurfacesConflict(t *testing.T) {
	fsm := NewFSM()
	mem := &fakeMembership{local: "node-1", ids: []string{"node-1", "node-2"}}
	svc := NewService(newDirectApplyRaft(fsm, true), fsm, &nopBus{}, nil, &nopRouter{}, mem, "node-1", noopLogger(), nil, nil, nil)
	svc.mu.Lock()
	svc.ready = true
	svc.mu.Unlock()

	p := makePID("node-1", "host", "p1")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		_, err := svc.RegisterScope(ctx, "root.confl", p, globalreg.Strong)
		errCh <- err
	}()

	// Wait until the pending entry exists, then reject from a required node.
	require.Eventually(t, func() bool {
		return fsm.State().pendingByName("root.confl") != nil
	}, time.Second, 5*time.Millisecond)
	pv := fsm.State().pendingByName("root.confl")
	_, err := svc.applyCommand(&Command{
		Type:      CmdRegisterReject,
		Name:      "root.confl",
		Epoch:     pv.Epoch,
		AckerNode: "node-2",
		Reason:    strongRejectConflict,
	})
	require.NoError(t, err)

	select {
	case err := <-errCh:
		require.Error(t, err)
		var cErr *globalreg.StrongConflictError
		require.ErrorAs(t, err, &cErr, "expected StrongConflictError, got %T: %v", err, err)
		var tErr *globalreg.StrongRegistrationTimeoutError
		assert.False(t, errors.As(err, &tErr), "conflict must be distinct from timeout")
	case <-time.After(2 * time.Second):
		t.Fatal("caller did not observe the reject outcome")
	}
}
