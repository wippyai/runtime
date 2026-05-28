// SPDX-License-Identifier: MPL-2.0

package globalreg

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	clusterapi "github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/globalreg"
)

// fakeMembership returns a fixed live set. Used to drive RegisterScope(Strong)
// without spinning a real cluster.
type fakeMembership struct {
	local string
	ids   []string
}

func (m *fakeMembership) Nodes() []clusterapi.NodeInfo {
	out := make([]clusterapi.NodeInfo, 0, len(m.ids))
	for _, id := range m.ids {
		out = append(out, clusterapi.NodeInfo{ID: id})
	}
	return out
}
func (m *fakeMembership) LocalNode() clusterapi.NodeInfo {
	return clusterapi.NodeInfo{ID: m.local}
}

func (m *fakeMembership) UpdateMeta(map[string]string) {}

// newStrongTestService wires a single-node service in leader role with a fake
// membership and direct-apply raft. handlePendingEvent will fire an ack for
// the local node synchronously inside FSM.Apply, which completes the set on
// a one-node cluster.
func newStrongTestService(t *testing.T, livePeers ...string) *Service {
	t.Helper()
	fsm := NewFSM()
	mem := &fakeMembership{local: "node-1"}
	mem.ids = append([]string{"node-1"}, livePeers...)
	svc := NewService(newDirectApplyRaft(fsm, true), fsm, &nopBus{}, nil, &nopRouter{}, mem, "node-1", noopLogger(), nil, nil, nil)
	svc.mu.Lock()
	svc.ready = true
	svc.mu.Unlock()
	return svc
}

// TestRegisterStrong_SingleNode validates the simplest Strong flow: the local
// node is the only required acker, so the Pending Apply causes an immediate
// self-ack and the watcher returns RegisterStateActive.
func TestRegisterStrong_SingleNode(t *testing.T) {
	svc := newStrongTestService(t)
	p := makePID("node-1", "host", "p1")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out, err := svc.RegisterScope(ctx, "system.root", p, globalreg.Strong)
	require.NoError(t, err)
	assert.Equal(t, p, out.PID)
	assert.Equal(t, globalreg.RegisterStateActive, out.State)
	assert.NotZero(t, out.Epoch)

	res, err := svc.Lookup(ctx, "system.root")
	require.NoError(t, err)
	assert.True(t, res.Found)
	assert.Equal(t, p, res.PID)
	_, gotIdx, found := svc.fsm.State().LookupWithIndex("system.root")
	require.True(t, found)
	assert.Equal(t, out.Epoch, gotIdx)
}

// TestRegisterStrong_TimeoutMissingAck simulates a peer that never acks.
// The handlePendingEvent path only fires the self-ack; the test shrinks
// the package-level StrongDeadline so the leader timer fires quickly and
// surfaces the StrongRegistrationTimeoutError before the parent ctx
// deadline.
func TestRegisterStrong_TimeoutMissingAck(t *testing.T) {
	orig := globalreg.StrongDeadline
	globalreg.StrongDeadline = 400 * time.Millisecond
	defer func() { globalreg.StrongDeadline = orig }()

	svc := newStrongTestService(t, "node-2")
	p := makePID("node-1", "host", "p1")

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	_, err := svc.RegisterScope(ctx, "system.timeout", p, globalreg.Strong)
	require.Error(t, err)
	var tErr *globalreg.StrongRegistrationTimeoutError
	require.True(t, errors.As(err, &tErr), "error should be StrongRegistrationTimeoutError, got %T: %v", err, err)
	assert.Equal(t, "system.timeout", tErr.Name)
	assert.Contains(t, tErr.MissingAcks, "node-2")

	res, _ := svc.Lookup(ctx, "system.timeout")
	assert.False(t, res.Found, "expired reservation must not be looked up")
}

// TestRegisterStrong_RebroadcastIdempotent verifies that a rebroadcast after
// activation is a no-op: no pending entries remain, so no nudge is sent and the
// active registration is untouched.
func TestRegisterStrong_RebroadcastIdempotent(t *testing.T) {
	svc := newStrongTestService(t)
	p := makePID("node-1", "host", "p1")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out, err := svc.RegisterScope(ctx, "system.idem", p, globalreg.Strong)
	require.NoError(t, err)
	require.Equal(t, globalreg.RegisterStateActive, out.State)

	// Calling rebroadcastPending after activation must be a no-op.
	svc.rebroadcastPending()

	res, _ := svc.Lookup(ctx, "system.idem")
	assert.True(t, res.Found)
	assert.Equal(t, p, res.PID)
}
