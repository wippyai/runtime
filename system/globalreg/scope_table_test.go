// SPDX-License-Identifier: MPL-2.0

package globalreg

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/globalreg"
)

// newTestService constructs a Service backed by a direct-apply Raft and
// flagged as ready so Lookup(WithFence) returns the FSM-resident entry.
func newTestService(fsm *FSM, leader bool) *Service {
	svc := NewService(newDirectApplyRaft(fsm, leader), fsm, &nopBus{}, nil, &nopRouter{}, nil, "node-1", noopLogger(), nil, nil, nil)
	svc.mu.Lock()
	svc.ready = true
	svc.mu.Unlock()
	return svc
}

// TestScopeTable_Consistent verifies the Consistent path through
// RegisterScope and UnregisterScope using a stub Raft service that
// directly applies the encoded command to the FSM.
func TestScopeTable_Consistent(t *testing.T) {
	fsm := NewFSM()
	svc := newTestService(fsm, true)

	p := makePID("node-1", "host", "1")
	out, err := svc.RegisterScope(context.Background(), "svc", p, globalreg.Consistent)
	require.NoError(t, err)
	assert.Equal(t, p, out.PID)
	assert.Equal(t, globalreg.RegisterStateActive, out.State)
	assert.NotZero(t, out.Epoch)

	res, err := svc.Lookup(context.Background(), "svc", globalreg.WithFence())
	require.NoError(t, err)
	assert.True(t, res.Found)
	assert.Equal(t, p, res.PID)
	assert.Equal(t, out.Epoch, res.FenceToken)

	removed, err := svc.UnregisterScope(context.Background(), "svc", globalreg.Consistent)
	require.NoError(t, err)
	assert.True(t, removed)
}

// TestScopeTable_BackCompatRegister verifies that the legacy Register
// signature (no mode) still works after the API extension.
func TestScopeTable_BackCompatRegister(t *testing.T) {
	fsm := NewFSM()
	svc := newTestService(fsm, true)

	p := makePID("node-1", "host", "1")
	got, err := svc.Register(context.Background(), "svc", p)
	require.NoError(t, err)
	assert.Equal(t, p, got)

	ok, err := svc.Unregister(context.Background(), "svc")
	require.NoError(t, err)
	assert.True(t, ok)
}
