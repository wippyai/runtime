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

	// leaveAllLocal now preserves duplicates for multi-join semantics
	// so that remote nodes can remove the correct number of occurrences.
	// The process joined workers, managers, workers — so we expect 3 entries.
	assert.Len(t, groups, 3, "should return 3 groups (with duplicates)")
	sort.Strings(groups)
	assert.Equal(t, []string{"managers", "workers", "workers"}, groups)

	assert.Empty(t, s.getMembers("workers"))
	assert.Empty(t, s.getMembers("managers"))
	assert.Empty(t, s.whichGroups())
}

func TestState_LeaveAllLocalMultiJoinSingleGroup(t *testing.T) {
	s := newState()
	p1 := mkPID("host1", "1")

	// Join same group 3 times
	s.joinLocal("workers", p1)
	s.joinLocal("workers", p1)
	s.joinLocal("workers", p1)

	groups := s.leaveAllLocal(p1)

	// Should return "workers" 3 times
	assert.Len(t, groups, 3)
	for _, g := range groups {
		assert.Equal(t, "workers", g)
	}

	assert.Empty(t, s.getMembers("workers"))
	assert.Empty(t, s.whichGroups())

	// Process should be fully removed from local tracking
	_, exists := s.local[p1.String()]
	assert.False(t, exists, "local process should be removed")
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

func TestState_WhichLocalGroups(t *testing.T) {
	s := newState()
	p1 := mkPID("host1", "1")
	p2 := mkPID("host1", "2")
	remote := mkNodePID("node-b", "host1", "3")

	s.joinLocal("workers", p1)
	s.joinLocal("managers", p2)
	s.joinRemote("node-b", "remote-only", []pid.PID{remote})

	groups := s.whichLocalGroups()
	sort.Strings(groups)
	assert.Equal(t, []string{"managers", "workers"}, groups)
}

func TestState_WhichLocalGroupsEmpty(t *testing.T) {
	s := newState()
	groups := s.whichLocalGroups()
	assert.Empty(t, groups)
}

func TestState_WhichLocalGroupsAfterLeave(t *testing.T) {
	s := newState()
	p1 := mkPID("host1", "1")

	s.joinLocal("workers", p1)
	s.leaveLocal("workers", p1)

	groups := s.whichLocalGroups()
	assert.Empty(t, groups)
}

func TestState_WhichLocalGroupsExcludesRemoteOnly(t *testing.T) {
	s := newState()
	remote := mkNodePID("node-b", "host1", "1")

	s.joinRemote("node-b", "workers", []pid.PID{remote})

	localGroups := s.whichLocalGroups()
	assert.Empty(t, localGroups)

	// But whichGroups should still return it
	allGroups := s.whichGroups()
	assert.Len(t, allGroups, 1)
	assert.Equal(t, "workers", allGroups[0])
}

func TestState_AllGroupMembers(t *testing.T) {
	s := newState()
	p1 := mkPID("host1", "1")
	p2 := mkPID("host1", "2")
	remote := mkNodePID("node-b", "host1", "3")

	s.joinLocal("workers", p1)
	s.joinLocal("workers", p2)
	s.joinRemote("node-b", "managers", []pid.PID{remote})

	result := s.allGroupMembers()
	require.Len(t, result, 2)
	assert.Len(t, result["workers"], 2)
	assert.Len(t, result["managers"], 1)
}

func TestState_AllGroupMembersEmpty(t *testing.T) {
	s := newState()
	result := s.allGroupMembers()
	assert.Empty(t, result)
}

func TestState_AllGroupMembersReturnsCopy(t *testing.T) {
	s := newState()
	p1 := mkPID("host1", "1")
	s.joinLocal("workers", p1)

	result1 := s.allGroupMembers()
	result2 := s.allGroupMembers()

	// Modifying one should not affect the other
	result1["workers"][0] = pid.PID{}
	assert.NotEqual(t, result1["workers"][0], result2["workers"][0])
}

func TestState_SnapshotGroup(t *testing.T) {
	s := newState()
	p1 := mkPID("host1", "1")
	p2 := mkPID("host1", "2")
	remote := mkNodePID("node-b", "host1", "3")

	s.joinLocal("workers", p1)
	s.joinLocal("workers", p2)
	s.joinRemote("node-b", "managers", []pid.PID{remote})

	workers := s.snapshotGroup("workers")
	require.NotNil(t, workers)
	assert.Len(t, workers.all, 2)
	assert.Len(t, workers.local, 2)

	managers := s.snapshotGroup("managers")
	require.NotNil(t, managers)
	assert.Len(t, managers.all, 1)
	assert.Empty(t, managers.local)
}

func TestState_SnapshotGroupAbsent(t *testing.T) {
	s := newState()
	assert.Nil(t, s.snapshotGroup("nonexistent"))
}

func TestState_SnapshotGroupImmutable(t *testing.T) {
	s := newState()
	p1 := mkPID("host1", "1")
	s.joinLocal("workers", p1)

	snap1 := s.snapshotGroup("workers")
	require.NotNil(t, snap1)

	p2 := mkPID("host1", "2")
	s.joinLocal("workers", p2)

	snap2 := s.snapshotGroup("workers")
	require.NotNil(t, snap2)

	assert.Len(t, snap1.all, 1)
	assert.Len(t, snap2.all, 2)
}

func TestState_DirtyTracking(t *testing.T) {
	s := newState()
	p1 := mkPID("host1", "1")

	assert.Empty(t, s.dirty)

	s.joinLocal("workers", p1)
	assert.True(t, s.dirty["workers"])

	clear(s.dirty)
	s.leaveLocal("workers", p1)
	assert.True(t, s.dirty["workers"])

	clear(s.dirty)
	rp := mkNodePID("node-b", "host1", "1")
	s.joinRemote("node-b", "managers", []pid.PID{rp})
	assert.True(t, s.dirty["managers"])
}

func TestState_LeaveRemoteMultiJoinConsistency(t *testing.T) {
	s := newState()
	rp1 := mkNodePID("node-b", "host1", "1")

	// Process joins same group 3 times (multi-join)
	s.joinRemote("node-b", "workers", []pid.PID{rp1})
	s.joinRemote("node-b", "workers", []pid.PID{rp1})
	s.joinRemote("node-b", "workers", []pid.PID{rp1})

	assert.Len(t, s.getMembers("workers"), 3)

	// Leave one occurrence
	s.leaveRemote("node-b", []pid.PID{rp1}, []string{"workers"})
	assert.Len(t, s.getMembers("workers"), 2)

	// Leave another
	s.leaveRemote("node-b", []pid.PID{rp1}, []string{"workers"})
	assert.Len(t, s.getMembers("workers"), 1)

	// Leave last
	s.leaveRemote("node-b", []pid.PID{rp1}, []string{"workers"})
	assert.Empty(t, s.getMembers("workers"))
	assert.Empty(t, s.whichGroups())
}

func TestState_LeaveRemoteMultiJoinMultiplePids(t *testing.T) {
	s := newState()
	rp1 := mkNodePID("node-b", "host1", "1")
	rp2 := mkNodePID("node-b", "host1", "2")

	// Each process joins twice
	s.joinRemote("node-b", "workers", []pid.PID{rp1, rp2})
	s.joinRemote("node-b", "workers", []pid.PID{rp1, rp2})

	assert.Len(t, s.getMembers("workers"), 4)

	// Leave one occurrence of each
	s.leaveRemote("node-b", []pid.PID{rp1, rp2}, []string{"workers"})
	assert.Len(t, s.getMembers("workers"), 2)

	// Leave remaining
	s.leaveRemote("node-b", []pid.PID{rp1, rp2}, []string{"workers"})
	assert.Empty(t, s.getMembers("workers"))
}

func TestState_CopyPIDs(t *testing.T) {
	t.Run("nil input", func(t *testing.T) {
		result := copyPIDs(nil)
		assert.Nil(t, result)
	})

	t.Run("empty input", func(t *testing.T) {
		result := copyPIDs([]pid.PID{})
		assert.Nil(t, result)
	})

	t.Run("returns independent copy", func(t *testing.T) {
		p1 := mkPID("host1", "1")
		orig := []pid.PID{p1}
		copied := copyPIDs(orig)

		copied[0] = pid.PID{}
		assert.NotEqual(t, orig[0], copied[0])
	})
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

func TestState_LeaveRemoteReturnsActuallyRemoved(t *testing.T) {
	s := newState()
	rp1 := mkNodePID("node-b", "host1", "1")

	// rp1 joins "workers" but NOT "managers"
	s.joinRemote("node-b", "workers", []pid.PID{rp1})

	// Leave both groups — only "workers" should appear in the result
	removed := s.leaveRemote("node-b", []pid.PID{rp1}, []string{"workers", "managers"})

	require.Len(t, removed, 1, "should only contain groups the PID was actually in")
	require.Contains(t, removed, "workers")
	assert.Equal(t, rp1.String(), removed["workers"][0].String())

	_, hasManagers := removed["managers"]
	assert.False(t, hasManagers, "should not contain 'managers' since rp1 was never in it")

	assert.Empty(t, s.getMembers("workers"))
}

func TestState_LeaveRemoteDoesNotCorruptOtherNodes(t *testing.T) {
	s := newState()
	rp1 := mkNodePID("node-b", "host1", "1")
	rp2 := mkNodePID("node-c", "host1", "2")

	// rp1 (node-b) joins "workers", rp2 (node-c) joins "workers" and "managers"
	s.joinRemote("node-b", "workers", []pid.PID{rp1})
	s.joinRemote("node-c", "workers", []pid.PID{rp2})
	s.joinRemote("node-c", "managers", []pid.PID{rp2})

	// Leave rp1 from both "workers" and "managers" on node-b.
	// rp1 was never in "managers" on node-b, so it must NOT remove
	// rp2 from "managers" (which belongs to node-c).
	removed := s.leaveRemote("node-b", []pid.PID{rp1}, []string{"workers", "managers"})

	require.Len(t, removed, 1)
	require.Contains(t, removed, "workers")

	// rp2 should still be in both groups
	assert.Len(t, s.getMembers("workers"), 1, "rp2 should remain in workers")
	assert.Equal(t, rp2.String(), s.getMembers("workers")[0].String())
	assert.Len(t, s.getMembers("managers"), 1, "rp2 should remain in managers")
	assert.Equal(t, rp2.String(), s.getMembers("managers")[0].String())
}

func TestState_LeaveRemoteReturnsNilForUnknownNode(t *testing.T) {
	s := newState()
	removed := s.leaveRemote("nonexistent", []pid.PID{mkPID("host1", "1")}, []string{"workers"})
	assert.Nil(t, removed)
}

func TestState_LeaveRemoteMultiJoinReturnsCorrectCount(t *testing.T) {
	s := newState()
	rp1 := mkNodePID("node-b", "host1", "1")

	// Join "workers" twice
	s.joinRemote("node-b", "workers", []pid.PID{rp1})
	s.joinRemote("node-b", "workers", []pid.PID{rp1})

	// Leave one occurrence
	removed := s.leaveRemote("node-b", []pid.PID{rp1}, []string{"workers"})
	require.Len(t, removed["workers"], 1, "should remove exactly one occurrence")
	assert.Len(t, s.getMembers("workers"), 1, "one occurrence should remain")

	// Leave the second
	removed = s.leaveRemote("node-b", []pid.PID{rp1}, []string{"workers"})
	require.Len(t, removed["workers"], 1)
	assert.Empty(t, s.getMembers("workers"))
}
