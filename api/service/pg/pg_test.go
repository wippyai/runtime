// SPDX-License-Identifier: MPL-2.0

package pg_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/dispatcher"
	pgapi "github.com/wippyai/runtime/api/service/pg"
	"github.com/wippyai/runtime/api/pid"
)

func testPID(host, uniq string) pid.PID {
	p := pid.PID{Host: host, UniqID: uniq}
	return p.Precomputed()
}

func TestCommandIDs(t *testing.T) {
	assert.Equal(t, dispatcher.CommandID(200), pgapi.Join)
	assert.Equal(t, dispatcher.CommandID(201), pgapi.Leave)
	assert.Equal(t, dispatcher.CommandID(202), pgapi.GetMembers)
	assert.Equal(t, dispatcher.CommandID(203), pgapi.GetLocalMembers)
	assert.Equal(t, dispatcher.CommandID(204), pgapi.WhichGroups)
	assert.Equal(t, dispatcher.CommandID(205), pgapi.Broadcast)
	assert.Equal(t, dispatcher.CommandID(206), pgapi.BroadcastLocal)
	assert.Equal(t, dispatcher.CommandID(207), pgapi.WhichLocalGroups)
	assert.Equal(t, dispatcher.CommandID(208), pgapi.Monitor)
	assert.Equal(t, dispatcher.CommandID(209), pgapi.Events)
	assert.Equal(t, dispatcher.CommandID(210), pgapi.JoinGroups)
	assert.Equal(t, dispatcher.CommandID(211), pgapi.LeaveGroups)
}

func TestProtocolTopics(t *testing.T) {
	assert.Equal(t, "pg.discover", pgapi.TopicDiscover)
	assert.Equal(t, "pg.sync", pgapi.TopicSync)
	assert.Equal(t, "pg.join", pgapi.TopicJoin)
	assert.Equal(t, "pg.leave", pgapi.TopicLeave)
}

func TestJoinCmd(t *testing.T) {
	t.Run("Acquire and Release", func(t *testing.T) {
		cmd := pgapi.AcquireJoinCmd()
		require.NotNil(t, cmd)

		p := testPID("test", "1")
		cmd.Caller = p
		cmd.Group = "workers"

		assert.Equal(t, pgapi.Join, cmd.CmdID())
		assert.Equal(t, "workers", cmd.Group)
		assert.Equal(t, p, cmd.Caller)

		cmd.Release()
	})

	t.Run("Pool reuse clears fields", func(t *testing.T) {
		cmd1 := pgapi.AcquireJoinCmd()
		cmd1.Group = "test-group"
		cmd1.Caller = testPID("host", "1")
		cmd1.Release()

		cmd2 := pgapi.AcquireJoinCmd()
		assert.Empty(t, cmd2.Group)
		assert.Equal(t, pid.PID{}, cmd2.Caller)
		cmd2.Release()
	})
}

func TestLeaveCmd(t *testing.T) {
	cmd := pgapi.AcquireLeaveCmd()
	require.NotNil(t, cmd)

	p := testPID("test", "2")
	cmd.Caller = p
	cmd.Group = "workers"

	assert.Equal(t, pgapi.Leave, cmd.CmdID())
	assert.Equal(t, "workers", cmd.Group)
	assert.Equal(t, p, cmd.Caller)

	cmd.Release()
}

func TestGetMembersCmd(t *testing.T) {
	cmd := pgapi.AcquireGetMembersCmd()
	require.NotNil(t, cmd)

	cmd.Group = "workers"

	assert.Equal(t, pgapi.GetMembers, cmd.CmdID())
	assert.Equal(t, "workers", cmd.Group)

	cmd.Release()
}

func TestGetLocalMembersCmd(t *testing.T) {
	cmd := pgapi.AcquireGetLocalMembersCmd()
	require.NotNil(t, cmd)

	cmd.Group = "workers"

	assert.Equal(t, pgapi.GetLocalMembers, cmd.CmdID())
	assert.Equal(t, "workers", cmd.Group)

	cmd.Release()
}

func TestWhichGroupsCmd(t *testing.T) {
	cmd := pgapi.AcquireWhichGroupsCmd()
	require.NotNil(t, cmd)

	assert.Equal(t, pgapi.WhichGroups, cmd.CmdID())

	cmd.Release()
}

func TestBroadcastCmd(t *testing.T) {
	cmd := pgapi.AcquireBroadcastCmd()
	require.NotNil(t, cmd)

	p := testPID("test", "3")
	cmd.From = p
	cmd.Group = "workers"
	cmd.Topic = "hello"

	assert.Equal(t, pgapi.Broadcast, cmd.CmdID())
	assert.Equal(t, p, cmd.From)
	assert.Equal(t, "workers", cmd.Group)
	assert.Equal(t, "hello", cmd.Topic)

	cmd.Release()
}

func TestBroadcastLocalCmd(t *testing.T) {
	cmd := pgapi.AcquireBroadcastLocalCmd()
	require.NotNil(t, cmd)

	p := testPID("test", "4")
	cmd.From = p
	cmd.Group = "workers"
	cmd.Topic = "hello"

	assert.Equal(t, pgapi.BroadcastLocal, cmd.CmdID())
	assert.Equal(t, p, cmd.From)
	assert.Equal(t, "workers", cmd.Group)
	assert.Equal(t, "hello", cmd.Topic)

	cmd.Release()
}

func TestWhichLocalGroupsCmd(t *testing.T) {
	t.Run("Acquire and Release", func(t *testing.T) {
		cmd := pgapi.AcquireWhichLocalGroupsCmd()
		require.NotNil(t, cmd)
		assert.Equal(t, pgapi.WhichLocalGroups, cmd.CmdID())
		cmd.Release()
	})
}

func TestMonitorCmd(t *testing.T) {
	t.Run("Acquire and Release", func(t *testing.T) {
		cmd := pgapi.AcquireMonitorCmd()
		require.NotNil(t, cmd)

		p := testPID("test", "5")
		cmd.Group = "workers"
		cmd.PID = p
		cmd.Topic = "pg.event"

		assert.Equal(t, pgapi.Monitor, cmd.CmdID())
		assert.Equal(t, "workers", cmd.Group)
		assert.Equal(t, p, cmd.PID)
		assert.Equal(t, "pg.event", cmd.Topic)

		cmd.Release()
	})

	t.Run("Pool reuse clears fields", func(t *testing.T) {
		cmd1 := pgapi.AcquireMonitorCmd()
		cmd1.Group = "test-group"
		cmd1.PID = testPID("host", "1")
		cmd1.Topic = "test-topic"
		cmd1.Release()

		cmd2 := pgapi.AcquireMonitorCmd()
		assert.Empty(t, cmd2.Group)
		assert.Equal(t, pid.PID{}, cmd2.PID)
		assert.Empty(t, cmd2.Topic)
		cmd2.Release()
	})
}

func TestEventsCmd(t *testing.T) {
	t.Run("Acquire and Release", func(t *testing.T) {
		cmd := pgapi.AcquireEventsCmd()
		require.NotNil(t, cmd)

		p := testPID("test", "6")
		cmd.PID = p
		cmd.Topic = "pg.event"

		assert.Equal(t, pgapi.Events, cmd.CmdID())
		assert.Equal(t, p, cmd.PID)
		assert.Equal(t, "pg.event", cmd.Topic)

		cmd.Release()
	})

	t.Run("Pool reuse clears fields", func(t *testing.T) {
		cmd1 := pgapi.AcquireEventsCmd()
		cmd1.PID = testPID("host", "1")
		cmd1.Topic = "test-topic"
		cmd1.Release()

		cmd2 := pgapi.AcquireEventsCmd()
		assert.Equal(t, pid.PID{}, cmd2.PID)
		assert.Empty(t, cmd2.Topic)
		cmd2.Release()
	})
}

func TestJoinGroupsCmd(t *testing.T) {
	t.Run("Acquire and Release", func(t *testing.T) {
		cmd := pgapi.AcquireJoinGroupsCmd()
		require.NotNil(t, cmd)

		p := testPID("test", "7")
		cmd.Caller = p
		cmd.Groups = []string{"workers", "managers"}

		assert.Equal(t, pgapi.JoinGroups, cmd.CmdID())
		assert.Equal(t, p, cmd.Caller)
		assert.Equal(t, []string{"workers", "managers"}, cmd.Groups)

		cmd.Release()
	})

	t.Run("Pool reuse clears fields", func(t *testing.T) {
		cmd1 := pgapi.AcquireJoinGroupsCmd()
		cmd1.Caller = testPID("host", "1")
		cmd1.Groups = []string{"a", "b", "c"}
		cmd1.Release()

		cmd2 := pgapi.AcquireJoinGroupsCmd()
		assert.Equal(t, pid.PID{}, cmd2.Caller)
		assert.Empty(t, cmd2.Groups)
		cmd2.Release()
	})
}

func TestLeaveGroupsCmd(t *testing.T) {
	t.Run("Acquire and Release", func(t *testing.T) {
		cmd := pgapi.AcquireLeaveGroupsCmd()
		require.NotNil(t, cmd)

		p := testPID("test", "8")
		cmd.Caller = p
		cmd.Groups = []string{"workers", "managers"}

		assert.Equal(t, pgapi.LeaveGroups, cmd.CmdID())
		assert.Equal(t, p, cmd.Caller)
		assert.Equal(t, []string{"workers", "managers"}, cmd.Groups)

		cmd.Release()
	})

	t.Run("Pool reuse clears fields", func(t *testing.T) {
		cmd1 := pgapi.AcquireLeaveGroupsCmd()
		cmd1.Caller = testPID("host", "1")
		cmd1.Groups = []string{"a", "b"}
		cmd1.Release()

		cmd2 := pgapi.AcquireLeaveGroupsCmd()
		assert.Equal(t, pid.PID{}, cmd2.Caller)
		assert.Empty(t, cmd2.Groups)
		cmd2.Release()
	})
}
