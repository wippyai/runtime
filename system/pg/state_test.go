// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
)

func mkPID(host, uniq string) pid.PID {
	p := pid.PID{Host: host, UniqID: uniq}
	return p.Precomputed()
}

func mkNodePID(node, host, uniq string) pid.PID {
	p := pid.PID{Node: node, Host: host, UniqID: uniq}
	return p.Precomputed()
}

func TestState_JoinLocal(t *testing.T) {
	s := newState()
	p1 := mkPID("host1", "1")

	s.joinLocal("workers", p1)

	members := s.getMembers("workers")
	require.Len(t, members, 1)
	assert.Equal(t, p1.String(), members[0].String())

	localMembers := s.getLocalMembers("workers")
	require.Len(t, localMembers, 1)
	assert.Equal(t, p1.String(), localMembers[0].String())
}

func TestState_JoinLocalMultipleProcesses(t *testing.T) {
	s := newState()
	p1 := mkPID("host1", "1")
	p2 := mkPID("host1", "2")

	s.joinLocal("workers", p1)
	s.joinLocal("workers", p2)

	members := s.getMembers("workers")
	assert.Len(t, members, 2)
}

func TestState_JoinLocalMultipleGroups(t *testing.T) {
	s := newState()
	p1 := mkPID("host1", "1")

	s.joinLocal("workers", p1)
	s.joinLocal("managers", p1)

	assert.Len(t, s.getMembers("workers"), 1)
	assert.Len(t, s.getMembers("managers"), 1)

	groups := s.whichGroups()
	sort.Strings(groups)
	assert.Equal(t, []string{"managers", "workers"}, groups)
}

func TestState_JoinLocalMultiJoin(t *testing.T) {
	s := newState()
	p1 := mkPID("host1", "1")

	// Same process joins same group twice (Erlang pg allows this)
	s.joinLocal("workers", p1)
	s.joinLocal("workers", p1)

	members := s.getMembers("workers")
	assert.Len(t, members, 2)
}

func TestState_LeaveLocal(t *testing.T) {
	s := newState()
	p1 := mkPID("host1", "1")

	s.joinLocal("workers", p1)

	ok := s.leaveLocal("workers", p1)
	assert.True(t, ok)

	members := s.getMembers("workers")
	assert.Empty(t, members)

	// Group should be cleaned up
	assert.Empty(t, s.whichGroups())
}

func TestState_LeaveLocalNotJoined(t *testing.T) {
	s := newState()
	p1 := mkPID("host1", "1")

	ok := s.leaveLocal("workers", p1)
	assert.False(t, ok)
}

func TestState_LeaveLocalMultiJoin(t *testing.T) {
	s := newState()
	p1 := mkPID("host1", "1")

	s.joinLocal("workers", p1)
	s.joinLocal("workers", p1)

	// First leave removes one occurrence
	ok := s.leaveLocal("workers", p1)
	assert.True(t, ok)
	assert.Len(t, s.getMembers("workers"), 1)

	// Second leave removes the other
	ok = s.leaveLocal("workers", p1)
	assert.True(t, ok)
	assert.Empty(t, s.getMembers("workers"))

	// Third leave fails
	ok = s.leaveLocal("workers", p1)
	assert.False(t, ok)
}

func TestState_LeaveAllLocal(t *testing.T) {
	s := newState()
	p1 := mkPID("host1", "1")

	s.joinLocal("workers", p1)
	s.joinLocal("managers", p1)
	s.joinLocal("workers", p1) // multi-join

	groups := s.leaveAllLocal(p1)
	sort.Strings(groups)
	assert.Equal(t, []string{"managers", "workers"}, groups)

	assert.Empty(t, s.getMembers("workers"))
	assert.Empty(t, s.getMembers("managers"))
	assert.Empty(t, s.whichGroups())
}

func TestState_LeaveAllLocalNotJoined(t *testing.T) {
	s := newState()
	p1 := mkPID("host1", "1")

	groups := s.leaveAllLocal(p1)
	assert.Nil(t, groups)
}

func TestState_JoinRemote(t *testing.T) {
	s := newState()
	rp1 := mkNodePID("node-b", "host1", "1")
	rp2 := mkNodePID("node-b", "host1", "2")

	s.joinRemote("node-b", "workers", []pid.PID{rp1, rp2})

	members := s.getMembers("workers")
	assert.Len(t, members, 2)

	// Remote processes are not local
	localMembers := s.getLocalMembers("workers")
	assert.Empty(t, localMembers)
}

func TestState_MixedLocalAndRemote(t *testing.T) {
	s := newState()
	local := mkPID("host1", "1")
	remote := mkNodePID("node-b", "host1", "2")

	s.joinLocal("workers", local)
	s.joinRemote("node-b", "workers", []pid.PID{remote})

	members := s.getMembers("workers")
	assert.Len(t, members, 2)

	localMembers := s.getLocalMembers("workers")
	assert.Len(t, localMembers, 1)
	assert.Equal(t, local.String(), localMembers[0].String())
}

func TestState_LeaveRemote(t *testing.T) {
	s := newState()
	rp1 := mkNodePID("node-b", "host1", "1")
	rp2 := mkNodePID("node-b", "host1", "2")

	s.joinRemote("node-b", "workers", []pid.PID{rp1, rp2})
	s.leaveRemote("node-b", []pid.PID{rp1}, []string{"workers"})

	members := s.getMembers("workers")
	require.Len(t, members, 1)
	assert.Equal(t, rp2.String(), members[0].String())
}

func TestState_LeaveRemoteAllFromGroup(t *testing.T) {
	s := newState()
	rp1 := mkNodePID("node-b", "host1", "1")

	s.joinRemote("node-b", "workers", []pid.PID{rp1})
	s.leaveRemote("node-b", []pid.PID{rp1}, []string{"workers"})

	assert.Empty(t, s.getMembers("workers"))
	assert.Empty(t, s.whichGroups())
}

func TestState_RemoveNode(t *testing.T) {
	s := newState()
	rp1 := mkNodePID("node-b", "host1", "1")
	rp2 := mkNodePID("node-b", "host1", "2")
	rp3 := mkNodePID("node-c", "host1", "3")

	s.joinRemote("node-b", "workers", []pid.PID{rp1, rp2})
	s.joinRemote("node-c", "workers", []pid.PID{rp3})

	s.removeNode("node-b")

	members := s.getMembers("workers")
	require.Len(t, members, 1)
	assert.Equal(t, rp3.String(), members[0].String())
}

func TestState_RemoveNodeNonexistent(t *testing.T) {
	s := newState()
	// Should not panic
	s.removeNode("nonexistent")
}

func TestState_SyncRemote(t *testing.T) {
	s := newState()
	rp1 := mkNodePID("node-b", "host1", "1")
	rp2 := mkNodePID("node-b", "host1", "2")

	// Initial join
	s.joinRemote("node-b", "workers", []pid.PID{rp1})

	// Sync replaces all state for node-b
	s.syncRemote("node-b", map[string][]pid.PID{
		"managers": {rp2},
	})

	assert.Empty(t, s.getMembers("workers"))
	members := s.getMembers("managers")
	require.Len(t, members, 1)
	assert.Equal(t, rp2.String(), members[0].String())
}

func TestState_GetMembersNonexistent(t *testing.T) {
	s := newState()
	members := s.getMembers("nonexistent")
	assert.Nil(t, members)
}

func TestState_GetLocalMembersNonexistent(t *testing.T) {
	s := newState()
	members := s.getLocalMembers("nonexistent")
	assert.Nil(t, members)
}

func TestState_WhichGroupsEmpty(t *testing.T) {
	s := newState()
	groups := s.whichGroups()
	assert.Empty(t, groups)
}

func TestState_AllLocalPids(t *testing.T) {
	s := newState()
	p1 := mkPID("host1", "1")
	p2 := mkPID("host1", "2")

	s.joinLocal("workers", p1)
	s.joinLocal("workers", p2)
	s.joinLocal("managers", p1)

	result := s.allLocalPids()
	assert.Len(t, result["workers"], 2)
	assert.Len(t, result["managers"], 1)
}

func TestState_GetMembersReturnsCopy(t *testing.T) {
	s := newState()
	p1 := mkPID("host1", "1")
	s.joinLocal("workers", p1)

	members1 := s.getMembers("workers")
	members2 := s.getMembers("workers")

	// Modifying one should not affect the other
	members1[0] = pid.PID{}
	assert.NotEqual(t, members1[0].String(), members2[0].String())
}
