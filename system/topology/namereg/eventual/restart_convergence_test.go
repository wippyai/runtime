// SPDX-License-Identifier: MPL-2.0

package eventual_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/system/eventbus"
	"github.com/wippyai/runtime/system/topology/namereg/eventual"
)

// A fresh local registration mints a counter above cv[localNode], so a restarted
// node (localCounter back to 0) dominates the prior incarnation's counter relearned
// via gossip.
func TestRegister_SeedsCounterAboveObservedOrigin(t *testing.T) {
	st := eventual.NewState("ctrl")

	// cv[localNode] reaches 5 via an applied tombstone for our own origin.
	st.Apply(&eventual.Entry{
		Name:    "app.control:rpc",
		Node:    st.LocalNode(),
		Counter: 5,
		Wall:    1,
		Deleted: true,
	})

	p := pid.PID{Node: "ctrl", Host: "h", UniqID: "new"}
	res := st.Register("app.control:rpc", p, 2, 0)

	require.True(t, res.Won, "fresh local registration must win over our own prior tombstone")
	assert.Greater(t, res.Entry.Counter, uint64(5),
		"mint must seed above cv[localNode]=5 so a restarted node dominates its prior incarnation")
}

// A node that registers a name, is reaped on departure, then restarts (fresh
// State, localCounter 0) and re-registers must converge back to live on every
// replica — not stay stuck behind the prior incarnation's reap tombstone.
func TestRestart_ReclaimsOwnNameAfterReapTombstone(t *testing.T) {
	const name = "app.control:rpc"
	ctx := context.Background()

	// Peer that observes the control node and reaps it on departure.
	busPeer := eventbus.NewBus()
	peer := eventual.NewService(eventual.Config{LocalNodeID: "peer", Bus: busPeer})
	require.NoError(t, peer.Start(ctx))
	defer peer.Stop()

	// First incarnation of the control node. A couple of warmup registrations push
	// the origin counter above 1 (realistic), then the target name.
	ctrl1 := eventual.NewService(eventual.Config{LocalNodeID: "ctrl"})
	require.NoError(t, ctrl1.Start(ctx))
	warm := pid.PID{Node: "ctrl", Host: "h", UniqID: "w"}
	_, err := ctrl1.Register("app.control:warm-a", warm)
	require.NoError(t, err)
	_, err = ctrl1.Register("app.control:warm-b", warm)
	require.NoError(t, err)
	pid1 := pid.PID{Node: "ctrl", Host: "h", UniqID: "rpc-1"}
	_, err = ctrl1.Register(name, pid1)
	require.NoError(t, err)

	for _, f := range ctrl1.DrainBroadcasts(0, 0) {
		peer.OnFrame(f)
	}
	r, _ := peer.Lookup(ctx, name)
	require.True(t, r.Found, "peer must see the control name live before the restart")
	require.NoError(t, ctrl1.Stop())

	// The control node departs: the peer reaps its bindings into tombstones at the
	// pre-restart counter.
	busPeer.Send(ctx, nodeLeftEvent("ctrl"))
	require.Eventually(t, func() bool {
		rr, _ := peer.Lookup(ctx, name)
		return !rr.Found
	}, 2*time.Second, 5*time.Millisecond, "peer must reap the departed control node's binding")

	// Control node RESTARTS: brand-new State (localCounter resets to 0), same id.
	ctrl2 := eventual.NewService(eventual.Config{LocalNodeID: "ctrl"})
	require.NoError(t, ctrl2.Start(ctx))
	defer ctrl2.Stop()
	pid2 := pid.PID{Node: "ctrl", Host: "h", UniqID: "rpc-2"}
	_, err = ctrl2.Register(name, pid2)
	require.NoError(t, err)

	// Bounded anti-entropy exchange until convergence.
	converged := func() bool {
		for i := 0; i < 20; i++ {
			for _, f := range ctrl2.DrainBroadcasts(0, 0) {
				peer.OnFrame(f)
			}
			for _, f := range peer.DrainBroadcasts(0, 0) {
				ctrl2.OnFrame(f)
			}
			rc, _ := ctrl2.Lookup(ctx, name)
			rp, _ := peer.Lookup(ctx, name)
			if rc.Found && rp.Found && rc.PID == pid2 && rp.PID == pid2 {
				return true
			}
		}
		return false
	}
	require.True(t, converged(),
		"after restart the control node must reclaim its own name and converge live on every replica")

	rc, _ := ctrl2.Lookup(ctx, name)
	rp, _ := peer.Lookup(ctx, name)
	assert.True(t, rc.Found && rc.PID == pid2, "restarted node resolves its own name to the new pid")
	assert.True(t, rp.Found && rp.PID == pid2, "peer converges to the restarted node's new pid")
}
