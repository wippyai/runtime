// SPDX-License-Identifier: MPL-2.0

package pg_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	pgapi "github.com/wippyai/runtime/api/pg"
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
}

func TestHostID(t *testing.T) {
	assert.Equal(t, pid.HostID("pg"), pgapi.HostID)
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

// mockProcessGroups is a minimal implementation for context tests.
type mockProcessGroups struct {
	name string
}

func (m *mockProcessGroups) Join(_ string, _ pid.PID) error     { return nil }
func (m *mockProcessGroups) Leave(_ string, _ pid.PID) error    { return nil }
func (m *mockProcessGroups) GetMembers(_ string) []pid.PID      { return nil }
func (m *mockProcessGroups) GetLocalMembers(_ string) []pid.PID { return nil }
func (m *mockProcessGroups) WhichGroups() []string              { return nil }
func (m *mockProcessGroups) Broadcast(_ pid.PID, _ string, _ string, _ payload.Payloads) error {
	return nil
}
func (m *mockProcessGroups) BroadcastLocal(_ pid.PID, _ string, _ string, _ payload.Payloads) error {
	return nil
}

func TestContextIntegration(t *testing.T) {
	t.Run("GetProcessGroups returns nil without AppContext", func(t *testing.T) {
		ctx := context.Background()
		pg := pgapi.GetProcessGroups(ctx)
		assert.Nil(t, pg)
	})

	t.Run("WithProcessGroups returns unmodified ctx without AppContext", func(t *testing.T) {
		ctx := context.Background()
		mock := &mockProcessGroups{name: "test"}
		result := pgapi.WithProcessGroups(ctx, mock)
		// Should return the same context (no AppContext to store in)
		assert.Equal(t, ctx, result)
		// GetProcessGroups still returns nil
		assert.Nil(t, pgapi.GetProcessGroups(result))
	})

	t.Run("WithProcessGroups and GetProcessGroups round-trip", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()
		mock := &mockProcessGroups{name: "round-trip"}
		ctx = pgapi.WithProcessGroups(ctx, mock)

		got := pgapi.GetProcessGroups(ctx)
		require.NotNil(t, got)
		assert.Equal(t, mock, got)
	})

	t.Run("WithProcessGroups idempotent - second call does not overwrite", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()
		first := &mockProcessGroups{name: "first"}
		second := &mockProcessGroups{name: "second"}

		ctx = pgapi.WithProcessGroups(ctx, first)
		ctx = pgapi.WithProcessGroups(ctx, second) // should be no-op

		got := pgapi.GetProcessGroups(ctx)
		require.NotNil(t, got)
		m, ok := got.(*mockProcessGroups)
		require.True(t, ok)
		assert.Equal(t, "first", m.name, "second WithProcessGroups should not overwrite")
	})

	t.Run("GetProcessGroups returns nil when key not set", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()
		got := pgapi.GetProcessGroups(ctx)
		assert.Nil(t, got)
	})
}
