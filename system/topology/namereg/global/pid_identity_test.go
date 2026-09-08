// SPDX-License-Identifier: MPL-2.0

package global

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
)

func TestStateSameOwnerIgnoresStringCache(t *testing.T) {
	p := pid.PID{Node: "node", Host: "process", UniqID: "owner"}
	s := newShardedState()
	_, _, out := s.register("active", p.Precomputed(), p.Node, 1)
	require.Equal(t, registerInserted, out)
	_, _, out = s.register("active", p, p.Node, 2)
	require.Equal(t, registerDedupe, out)
	_, pending := s.registerPending("pending", p.Precomputed(), p.Node, 3, nil, 100, 0)
	require.Equal(t, pendingInserted, pending)
	_, pending = s.registerPending("pending", p, p.Node, 3, nil, 100, 0)
	require.Equal(t, pendingDedupe, pending)
	_, removed := s.unreservePending("pending", p)
	require.True(t, removed)
	_, pending = s.registerPending("wildcard", p, p.Node, 4, nil, 100, 0)
	require.Equal(t, pendingInserted, pending)
	zero := pid.PID{}
	zero = zero.Precomputed()
	_, removed = s.unreservePending("wildcard", zero)
	require.True(t, removed)
}

func TestServiceSameOwnerIgnoresStringCache(t *testing.T) {
	p := pid.PID{Node: "node", Host: "process", UniqID: "owner"}
	presence := newFakeLocalPresence()
	presence.setLocal("local", p.Precomputed())
	presence.setEventual("eventual", p.Precomputed())

	svc := &Service{}
	svc.SetLocalPresence(presence)
	for _, name := range []string{"local", "eventual"} {
		_, conflict := svc.localConflict(name, p)
		require.False(t, conflict)
	}
}

func TestFSMIncomingWinnerIgnoresStringCache(t *testing.T) {
	existing := pid.PID{Node: "node", Host: "process", UniqID: "existing"}
	incoming := pid.PID{Node: "node", Host: "process", UniqID: "incoming"}
	fsm := NewFSM(func(_ string, _, _ pid.PID) pid.PID {
		return incoming.Precomputed()
	})

	first := fsm.applyRegister(&Command{Type: CmdRegister, Name: "name", PID: existing}, 1)
	require.NoError(t, first.(*RegisterResult).Err)
	second := fsm.applyRegister(&Command{Type: CmdRegister, Name: "name", PID: incoming}, 2)
	result := second.(*RegisterResult)
	require.NoError(t, result.Err)
	require.True(t, result.PID.Equal(incoming))
	resolved, found := fsm.State().Lookup("name")
	require.True(t, found)
	require.True(t, resolved.Equal(incoming))
}
