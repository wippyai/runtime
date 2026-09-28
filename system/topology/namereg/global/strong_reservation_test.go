// SPDX-License-Identifier: MPL-2.0

package global

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology/namereg/global"
)

// TestReservation_TracksPendingOwner checks replica-local Strong bookkeeping.
func TestReservation_TracksPendingOwner(t *testing.T) {
	fsm := NewFSM()
	mem := &fakeMembership{local: "node-1", ids: []string{"node-1", "node-2"}}
	svc := NewService(newDirectApplyRaft(fsm, true), fsm, &nopBus{}, nil, &nopRouter{}, mem, "node-1", noopLogger(), nil, nil, nil)

	// Two-node required set so node-1 acks (and reserves) but the entry stays
	// pending awaiting node-2.
	py := makePID("node-1", "host", "py")
	openPending(t, fsm, "root.reserved", py, "node-1", []pid.NodeID{"node-1", "node-2"}, 200)

	require.Eventually(t, func() bool {
		_, ok := svc.isStrongReserved("root.reserved")
		return ok
	}, time.Second, 5*time.Millisecond, "node-1 latches a reservation after acking")

	rp, reserved := svc.IsStrongReserved("root.reserved")
	require.True(t, reserved)
	assert.Equal(t, py, rp, "reservation surfaces the pending pid as taken")

	// Repeated inspection preserves the same pending observation.
	_, reservedSame := svc.IsStrongReserved("root.reserved")
	assert.True(t, reservedSame)
}

// TestObservation_PersistsThroughPromotion verifies pending ownership survives activation.
func TestObservation_PersistsThroughPromotion(t *testing.T) {
	fsm := NewFSM()
	mem := &fakeMembership{local: "node-1", ids: []string{"node-1", "node-2"}}
	svc := NewService(newDirectApplyRaft(fsm, true), fsm, &nopBus{}, nil, &nopRouter{}, mem, "node-1", noopLogger(), nil, nil, nil)

	py := makePID("node-1", "host", "py")
	epoch := openPending(t, fsm, "root.active", py, "node-1", []pid.NodeID{"node-1", "node-2"}, 210)

	require.Eventually(t, func() bool {
		_, ok := svc.isStrongReserved("root.active")
		return ok
	}, time.Second, 5*time.Millisecond)

	// node-2 acks -> promotes to active. The observation must NOT drop; it converts
	// Pending -> Active and persists.
	applyAt(t, fsm, &Command{Type: CmdRegisterAck, Name: "root.active", Epoch: epoch, AckerNode: "node-2"}, 211)

	rp, reserved := svc.isStrongReserved("root.active")
	require.True(t, reserved, "observation persists through promotion")
	assert.Equal(t, py, rp, "active observation still surfaces the owning pid")
}

// TestReservation_ReleasedOnExpired proves the reservation clears on the
// committed Expired terminal outcome (timeout or reject both arrive as expired).
func TestReservation_ReleasedOnExpired(t *testing.T) {
	fsm := NewFSM()
	mem := &fakeMembership{local: "node-1", ids: []string{"node-1", "node-2"}}
	svc := NewService(newDirectApplyRaft(fsm, true), fsm, &nopBus{}, nil, &nopRouter{}, mem, "node-1", noopLogger(), nil, nil, nil)

	py := makePID("node-1", "host", "py")
	epoch := openPending(t, fsm, "root.expire", py, "node-1", []pid.NodeID{"node-1", "node-2"}, 220)

	require.Eventually(t, func() bool {
		_, ok := svc.isStrongReserved("root.expire")
		return ok
	}, time.Second, 5*time.Millisecond)

	applyAt(t, fsm, &Command{Type: CmdRegisterExpired, Name: "root.expire", Epoch: epoch, Reason: "missing_ack"}, 221)

	_, reserved := svc.isStrongReserved("root.expire")
	assert.False(t, reserved, "reservation released on Expired terminal event")
}

// TestObservation_IndexedRelease proves a release for an older instance (epoch E1)
// must NOT clear a newer same-name observation held at epoch E2; only a release
// carrying E2 clears it.
func TestObservation_IndexedRelease(t *testing.T) {
	fsm := NewFSM()
	mem := &fakeMembership{local: "node-1", ids: []string{"node-1"}}
	svc := NewService(newDirectApplyRaft(fsm, true), fsm, &nopBus{}, nil, &nopRouter{}, mem, "node-1", noopLogger(), nil, nil, nil)

	const e1, e2 = uint64(10), uint64(20)
	py := makePID("node-1", "host", "py")
	svc.latchReservation("root.idx", py, e2)

	// A stale terminal for an older epoch must leave the newer observation intact.
	svc.releaseObservation("root.idx", e1)
	_, reserved := svc.isStrongReserved("root.idx")
	assert.True(t, reserved, "stale-epoch release must not clear a newer observation")

	// The matching epoch clears it.
	svc.releaseObservation("root.idx", e2)
	_, reserved = svc.isStrongReserved("root.idx")
	assert.False(t, reserved, "matching-epoch release clears the observation")
}

// TestObservation_ReleaseDeliveredOnTerminal proves the leader sends a targeted
// release to the observation holders (RequiredNodes) on a terminal expire so a
// non-leader holder drops its observation. Without delivery a follower that
// latched via the nudge would block the name forever (false block / leak).
func TestObservation_ReleaseDeliveredOnTerminal(t *testing.T) {
	fsm := NewFSM()
	mem := &fakeMembership{local: "node-1", ids: []string{"node-1", "node-2"}}
	router := &capturingRouter{}
	svc := NewService(newDirectApplyRaft(fsm, true), fsm, &nopBus{}, nil, router, mem, "node-1", noopLogger(), nil, nil, nil)

	py := makePID("node-1", "host", "py")
	epoch := openPending(t, fsm, "root.rel", py, "node-1", []pid.NodeID{"node-1", "node-2"}, 230)

	require.Eventually(t, func() bool {
		_, ok := svc.isStrongReserved("root.rel")
		return ok
	}, time.Second, 5*time.Millisecond)

	applyAt(t, fsm, &Command{Type: CmdRegisterExpired, Name: "root.rel", Epoch: epoch, Reason: "missing_ack"}, 231)

	// The leader must have sent a release to the remote holder node-2.
	rel := router.byTopic(topicReleaseObservation)
	var sawNode2 bool
	for _, r := range rel {
		if r.target == "node-2" {
			sawNode2 = true
		}
	}
	assert.True(t, sawNode2, "leader delivers release to remote holder node-2 on terminal")
}

// TestObservation_HolderReleasesOnDelivery proves a follower that latched an
// observation via the relay nudge drops it when it receives a release delivery,
// so IsStrongReserved goes false on the non-leader holder.
func TestObservation_HolderReleasesOnDelivery(t *testing.T) {
	fsm := NewFSM()
	mem := &fakeMembership{local: "node-2", ids: []string{"node-1", "node-2"}}
	raftStub := newDirectApplyRaft(fsm, false)
	raftStub.knownLeader = "node-1"
	router := &capturingRouter{}
	svc := NewService(raftStub, fsm, &nopBus{}, nil, router, mem, "node-2", noopLogger(), nil, nil, nil)

	// Seed the follower's FSM so its ack path latches the observation.
	py := makePID("node-1", "host", "py")
	epoch := openPending(t, fsm, "root.hold", py, "node-1", []pid.NodeID{"node-1", "node-2"}, 240)

	require.Eventually(t, func() bool {
		_, ok := svc.isStrongReserved("root.hold")
		return ok
	}, time.Second, 5*time.Millisecond, "follower latches an observation after acking")

	// Leader delivers a release for the held (name, epoch).
	body, err := marshalMsgpack(releaseEnvelope{Name: "root.hold", Epoch: epoch})
	require.NoError(t, err)
	pkg := relay.NewServicePackage("node-1", HostID, "node-2", HostID, topicReleaseObservation, payload.New(body))
	require.NoError(t, svc.Send(pkg))

	_, reserved := svc.isStrongReserved("root.hold")
	assert.False(t, reserved, "follower releases its observation on delivery")
}

// TestObservation_ActiveUnregisterDelivers proves unregistering an ACTIVE strong
// name delivers a release to the holders so the observation is cleared locally and
// remotely — no permanent false block and no observation leak.
func TestObservation_ActiveUnregisterDelivers(t *testing.T) {
	fsm := NewFSM()
	mem := &fakeMembership{local: "node-1", ids: []string{"node-1", "node-2"}}
	router := &capturingRouter{}
	svc := NewService(newDirectApplyRaft(fsm, true), fsm, &nopBus{}, nil, router, mem, "node-1", noopLogger(), nil, nil, nil)

	py := makePID("node-1", "host", "py")
	epoch := openPending(t, fsm, "root.unreg", py, "node-1", []pid.NodeID{"node-1", "node-2"}, 250)

	require.Eventually(t, func() bool {
		_, ok := svc.isStrongReserved("root.unreg")
		return ok
	}, time.Second, 5*time.Millisecond)

	// node-2 acks -> active; observation converts to Active and persists.
	applyAt(t, fsm, &Command{Type: CmdRegisterAck, Name: "root.unreg", Epoch: epoch, AckerNode: "node-2"}, 251)
	_, reserved := svc.isStrongReserved("root.unreg")
	require.True(t, reserved, "active observation held before unregister")

	// Unregister the ACTIVE strong name.
	_, err := svc.UnregisterScope(context.Background(), "root.unreg", global.Strong)
	require.NoError(t, err)

	// Local observation released.
	_, reserved = svc.isStrongReserved("root.unreg")
	assert.False(t, reserved, "active-name unregister releases the local observation")

	// And the remote holder node-2 received a release delivery.
	rel := router.byTopic(topicReleaseObservation)
	var sawNode2 bool
	for _, r := range rel {
		if r.target == "node-2" {
			sawNode2 = true
		}
	}
	assert.True(t, sawNode2, "active-name unregister delivers release to remote holder node-2")
}
