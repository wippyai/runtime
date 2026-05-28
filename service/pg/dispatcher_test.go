// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/dispatcher"
	pgapi "github.com/wippyai/runtime/api/pg"
)

// mockResultReceiver captures CompleteYield calls for assertion.
type mockResultReceiver struct {
	data any
	err  error
}

func (m *mockResultReceiver) CompleteYield(_ uint64, data any, err error) {
	m.data = data
	m.err = err
}

func newTestDispatcher(t *testing.T) (*Dispatcher, *Service, *mockRouter) {
	t.Helper()
	svc, router, _ := startTestService(t)
	d := NewDispatcher()
	return d, svc, router
}

func TestNewDispatcher(t *testing.T) {
	d := NewDispatcher()
	require.NotNil(t, d)
}

func TestDispatcher_RegisterAll(t *testing.T) {
	d, _, _ := newTestDispatcher(t)

	registered := make(map[dispatcher.CommandID]dispatcher.Handler)
	register := func(id dispatcher.CommandID, h dispatcher.Handler) {
		registered[id] = h
	}

	d.RegisterAll(register)

	assert.NotNil(t, registered[pgapi.Join])
	assert.NotNil(t, registered[pgapi.Leave])
	assert.NotNil(t, registered[pgapi.GetMembers])
	assert.NotNil(t, registered[pgapi.GetLocalMembers])
	assert.NotNil(t, registered[pgapi.WhichGroups])
	assert.NotNil(t, registered[pgapi.WhichLocalGroups])
	assert.NotNil(t, registered[pgapi.Broadcast])
	assert.NotNil(t, registered[pgapi.BroadcastLocal])
	assert.NotNil(t, registered[pgapi.Monitor])
	assert.NotNil(t, registered[pgapi.Events])
	assert.NotNil(t, registered[pgapi.JoinGroups])
	assert.NotNil(t, registered[pgapi.LeaveGroups])
	assert.Len(t, registered, 12)
}

func TestDispatcher_HandleJoin(t *testing.T) {
	d, svc, _ := newTestDispatcher(t)

	p1 := mkPID("host1", "1")
	cmd := &pgapi.JoinCmd{
		Instance: svc,
		Caller:   p1,
		Group:    "workers",
	}

	receiver := &mockResultReceiver{}
	err := d.handleJoin(context.Background(), cmd, 1, receiver)
	require.NoError(t, err)
	assert.Nil(t, receiver.err)

	result, ok := receiver.data.(pgapi.JoinResult)
	require.True(t, ok)
	assert.Nil(t, result.Error)

	members := svc.GetMembers("workers")
	assert.Len(t, members, 1)
}

func TestDispatcher_HandleLeave(t *testing.T) {
	d, svc, _ := newTestDispatcher(t)

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))

	cmd := &pgapi.LeaveCmd{
		Instance: svc,
		Caller:   p1,
		Group:    "workers",
	}

	receiver := &mockResultReceiver{}
	err := d.handleLeave(context.Background(), cmd, 1, receiver)
	require.NoError(t, err)

	result, ok := receiver.data.(pgapi.LeaveResult)
	require.True(t, ok)
	assert.Nil(t, result.Error)

	members := svc.GetMembers("workers")
	assert.Empty(t, members)
}

func TestDispatcher_HandleLeaveNotJoined(t *testing.T) {
	d, svc, _ := newTestDispatcher(t)

	p1 := mkPID("host1", "1")
	cmd := &pgapi.LeaveCmd{
		Instance: svc,
		Caller:   p1,
		Group:    "workers",
	}

	receiver := &mockResultReceiver{}
	err := d.handleLeave(context.Background(), cmd, 1, receiver)
	require.NoError(t, err)

	result, ok := receiver.data.(pgapi.LeaveResult)
	require.True(t, ok)
	assert.ErrorIs(t, result.Error, ErrNotJoined)
}

func TestDispatcher_HandleGetMembers(t *testing.T) {
	d, svc, _ := newTestDispatcher(t)

	p1 := mkPID("host1", "1")
	p2 := mkPID("host1", "2")
	require.NoError(t, svc.Join("workers", p1))
	require.NoError(t, svc.Join("workers", p2))

	cmd := &pgapi.GetMembersCmd{Instance: svc, Group: "workers"}

	receiver := &mockResultReceiver{}
	err := d.handleGetMembers(context.Background(), cmd, 1, receiver)
	require.NoError(t, err)

	result, ok := receiver.data.(pgapi.GetMembersResult)
	require.True(t, ok)
	assert.Len(t, result.Members, 2)
}

func TestDispatcher_HandleGetMembersEmpty(t *testing.T) {
	d, svc, _ := newTestDispatcher(t)

	cmd := &pgapi.GetMembersCmd{Instance: svc, Group: "nonexistent"}

	receiver := &mockResultReceiver{}
	err := d.handleGetMembers(context.Background(), cmd, 1, receiver)
	require.NoError(t, err)

	result, ok := receiver.data.(pgapi.GetMembersResult)
	require.True(t, ok)
	assert.Nil(t, result.Members)
}

func TestDispatcher_HandleGetLocalMembers(t *testing.T) {
	d, svc, _ := newTestDispatcher(t)

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))

	cmd := &pgapi.GetLocalMembersCmd{Instance: svc, Group: "workers"}

	receiver := &mockResultReceiver{}
	err := d.handleGetLocalMembers(context.Background(), cmd, 1, receiver)
	require.NoError(t, err)

	result, ok := receiver.data.(pgapi.GetLocalMembersResult)
	require.True(t, ok)
	assert.Len(t, result.Members, 1)
}

func TestDispatcher_HandleGetLocalMembersEmpty(t *testing.T) {
	d, svc, _ := newTestDispatcher(t)

	cmd := &pgapi.GetLocalMembersCmd{Instance: svc, Group: "nonexistent"}

	receiver := &mockResultReceiver{}
	err := d.handleGetLocalMembers(context.Background(), cmd, 1, receiver)
	require.NoError(t, err)

	result, ok := receiver.data.(pgapi.GetLocalMembersResult)
	require.True(t, ok)
	assert.Nil(t, result.Members)
}

func TestDispatcher_HandleWhichGroups(t *testing.T) {
	d, svc, _ := newTestDispatcher(t)

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))
	require.NoError(t, svc.Join("managers", p1))

	cmd := &pgapi.WhichGroupsCmd{Instance: svc}

	receiver := &mockResultReceiver{}
	err := d.handleWhichGroups(context.Background(), cmd, 1, receiver)
	require.NoError(t, err)

	result, ok := receiver.data.(pgapi.WhichGroupsResult)
	require.True(t, ok)
	assert.Len(t, result.Groups, 2)
}

func TestDispatcher_HandleWhichGroupsEmpty(t *testing.T) {
	d, svc, _ := newTestDispatcher(t)

	cmd := &pgapi.WhichGroupsCmd{Instance: svc}

	receiver := &mockResultReceiver{}
	err := d.handleWhichGroups(context.Background(), cmd, 1, receiver)
	require.NoError(t, err)

	result, ok := receiver.data.(pgapi.WhichGroupsResult)
	require.True(t, ok)
	assert.Empty(t, result.Groups)
}

func TestDispatcher_HandleBroadcast(t *testing.T) {
	d, svc, router := newTestDispatcher(t)

	p1 := mkPID("host1", "1")
	p2 := mkPID("host1", "2")
	require.NoError(t, svc.Join("workers", p1))
	require.NoError(t, svc.Join("workers", p2))

	router.reset()

	from := mkPID("host1", "sender")
	cmd := &pgapi.BroadcastCmd{
		Instance: svc,
		From:     from,
		Group:    "workers",
		Topic:    "hello",
	}

	receiver := &mockResultReceiver{}
	err := d.handleBroadcast(context.Background(), cmd, 1, receiver)
	require.NoError(t, err)

	result, ok := receiver.data.(pgapi.BroadcastResult)
	require.True(t, ok)
	assert.Equal(t, 2, result.Sent)

	// Wait for async delivery
	time.Sleep(50 * time.Millisecond)
	sends := router.getSends()
	assert.Len(t, sends, 2)
}

func TestDispatcher_HandleBroadcastEmptyGroup(t *testing.T) {
	d, svc, router := newTestDispatcher(t)

	router.reset()

	from := mkPID("host1", "sender")
	cmd := &pgapi.BroadcastCmd{
		Instance: svc,
		From:     from,
		Group:    "nonexistent",
		Topic:    "hello",
	}

	receiver := &mockResultReceiver{}
	err := d.handleBroadcast(context.Background(), cmd, 1, receiver)
	require.NoError(t, err)

	result, ok := receiver.data.(pgapi.BroadcastResult)
	require.True(t, ok)
	assert.Equal(t, 0, result.Sent)
}

func TestDispatcher_HandleBroadcastLocal(t *testing.T) {
	d, svc, router := newTestDispatcher(t)

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))

	router.reset()

	from := mkPID("host1", "sender")
	cmd := &pgapi.BroadcastLocalCmd{
		Instance: svc,
		From:     from,
		Group:    "workers",
		Topic:    "hello",
	}

	receiver := &mockResultReceiver{}
	err := d.handleBroadcastLocal(context.Background(), cmd, 1, receiver)
	require.NoError(t, err)

	result, ok := receiver.data.(pgapi.BroadcastLocalResult)
	require.True(t, ok)
	assert.Equal(t, 1, result.Sent)

	time.Sleep(50 * time.Millisecond)
	sends := router.getSends()
	assert.Len(t, sends, 1)
}

func TestDispatcher_HandleBroadcastLocalEmptyGroup(t *testing.T) {
	d, svc, router := newTestDispatcher(t)

	router.reset()

	from := mkPID("host1", "sender")
	cmd := &pgapi.BroadcastLocalCmd{
		Instance: svc,
		From:     from,
		Group:    "nonexistent",
		Topic:    "hello",
	}

	receiver := &mockResultReceiver{}
	err := d.handleBroadcastLocal(context.Background(), cmd, 1, receiver)
	require.NoError(t, err)

	result, ok := receiver.data.(pgapi.BroadcastLocalResult)
	require.True(t, ok)
	assert.Equal(t, 0, result.Sent)
}

// --- sendToMembers error path tests ---

func TestDispatcher_SendToMembersRouterError(t *testing.T) {
	d, svc, router := newTestDispatcher(t)

	p1 := mkPID("host1", "1")
	p2 := mkPID("host1", "2")
	require.NoError(t, svc.Join("workers", p1))
	require.NoError(t, svc.Join("workers", p2))

	// Make router fail
	router.mu.Lock()
	router.sendErr = errors.New("router send failed")
	router.mu.Unlock()

	router.reset()

	from := mkPID("host1", "sender")
	cmd := &pgapi.BroadcastCmd{
		Instance: svc,
		From:     from,
		Group:    "workers",
		Topic:    "hello",
	}

	receiver := &mockResultReceiver{}
	err := d.handleBroadcast(context.Background(), cmd, 1, receiver)
	require.NoError(t, err)

	result, ok := receiver.data.(pgapi.BroadcastResult)
	require.True(t, ok)
	assert.Equal(t, 0, result.Sent, "all sends should have failed")

	time.Sleep(50 * time.Millisecond)

	sends := router.getSends()
	assert.Empty(t, sends, "router rejected all sends")
}

func TestDispatcher_SendToMembersPartialFailure(t *testing.T) {
	d, svc, router := newTestDispatcher(t)

	p1 := mkPID("host1", "1")
	p2 := mkPID("host1", "2")
	p3 := mkPID("host1", "3")
	require.NoError(t, svc.Join("workers", p1))
	require.NoError(t, svc.Join("workers", p2))
	require.NoError(t, svc.Join("workers", p3))

	router.reset()

	from := mkPID("host1", "sender")

	// Verify all 3 members joined
	members := svc.GetMembers("workers")
	require.Len(t, members, 3)

	// Broadcast to all 3 — all should succeed with healthy router
	cmd := &pgapi.BroadcastCmd{
		Instance: svc,
		From:     from,
		Group:    "workers",
		Topic:    "hello",
	}

	receiver := &mockResultReceiver{}
	err := d.handleBroadcast(context.Background(), cmd, 1, receiver)
	require.NoError(t, err)

	result, ok := receiver.data.(pgapi.BroadcastResult)
	require.True(t, ok)
	assert.Equal(t, 3, result.Sent)
}

func TestDispatcher_BroadcastLocalRouterError(t *testing.T) {
	d, svc, router := newTestDispatcher(t)

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))

	// Make router fail
	router.mu.Lock()
	router.sendErr = errors.New("router send failed")
	router.mu.Unlock()

	router.reset()

	from := mkPID("host1", "sender")
	cmd := &pgapi.BroadcastLocalCmd{
		Instance: svc,
		From:     from,
		Group:    "workers",
		Topic:    "hello",
	}

	receiver := &mockResultReceiver{}
	err := d.handleBroadcastLocal(context.Background(), cmd, 1, receiver)
	require.NoError(t, err)

	result, ok := receiver.data.(pgapi.BroadcastLocalResult)
	require.True(t, ok)
	assert.Equal(t, 0, result.Sent, "all local sends should have failed")
}

// --- WhichLocalGroups dispatcher tests ---

func TestDispatcher_HandleWhichLocalGroups(t *testing.T) {
	d, svc, _ := newTestDispatcher(t)

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))
	require.NoError(t, svc.Join("managers", p1))

	cmd := &pgapi.WhichLocalGroupsCmd{Instance: svc}

	receiver := &mockResultReceiver{}
	err := d.handleWhichLocalGroups(context.Background(), cmd, 1, receiver)
	require.NoError(t, err)

	result, ok := receiver.data.(pgapi.WhichLocalGroupsResult)
	require.True(t, ok)
	assert.Len(t, result.Groups, 2)
}

func TestDispatcher_HandleWhichLocalGroupsEmpty(t *testing.T) {
	d, svc, _ := newTestDispatcher(t)

	cmd := &pgapi.WhichLocalGroupsCmd{Instance: svc}

	receiver := &mockResultReceiver{}
	err := d.handleWhichLocalGroups(context.Background(), cmd, 1, receiver)
	require.NoError(t, err)

	result, ok := receiver.data.(pgapi.WhichLocalGroupsResult)
	require.True(t, ok)
	assert.Empty(t, result.Groups)
}

// --- Monitor dispatcher tests ---

func TestDispatcher_HandleMonitor(t *testing.T) {
	d, svc, _ := newTestDispatcher(t)

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))

	monitorPID := mkPID("host1", "monitor")
	cmd := &pgapi.MonitorCmd{
		Instance: svc,
		Group:    "workers",
		PID:      monitorPID,
		Topic:    "pg.event",
	}

	receiver := &mockResultReceiver{}
	err := d.handleMonitor(context.Background(), cmd, 1, receiver)
	require.NoError(t, err)

	result, ok := receiver.data.(pgapi.MonitorResult)
	require.True(t, ok)
	assert.Len(t, result.Members, 1)
	assert.NotNil(t, result.Unsubscribe)

	result.Unsubscribe()
	time.Sleep(50 * time.Millisecond)
}

func TestDispatcher_HandleMonitorEmpty(t *testing.T) {
	d, svc, _ := newTestDispatcher(t)

	monitorPID := mkPID("host1", "monitor")
	cmd := &pgapi.MonitorCmd{
		Instance: svc,
		Group:    "nonexistent",
		PID:      monitorPID,
		Topic:    "pg.event",
	}

	receiver := &mockResultReceiver{}
	err := d.handleMonitor(context.Background(), cmd, 1, receiver)
	require.NoError(t, err)

	result, ok := receiver.data.(pgapi.MonitorResult)
	require.True(t, ok)
	assert.Nil(t, result.Members)
	assert.NotNil(t, result.Unsubscribe)

	result.Unsubscribe()
}

// --- Events dispatcher tests ---

func TestDispatcher_HandleEvents(t *testing.T) {
	d, svc, _ := newTestDispatcher(t)

	p1 := mkPID("host1", "1")
	p2 := mkPID("host1", "2")
	require.NoError(t, svc.Join("workers", p1))
	require.NoError(t, svc.Join("managers", p2))

	eventsPID := mkPID("host1", "events")
	cmd := &pgapi.EventsCmd{
		Instance: svc,
		PID:      eventsPID,
		Topic:    "pg.event",
	}

	receiver := &mockResultReceiver{}
	err := d.handleEvents(context.Background(), cmd, 1, receiver)
	require.NoError(t, err)

	result, ok := receiver.data.(pgapi.EventsResult)
	require.True(t, ok)
	assert.Len(t, result.Groups, 2)
	assert.Len(t, result.Groups["workers"], 1)
	assert.Len(t, result.Groups["managers"], 1)
	assert.NotNil(t, result.Unsubscribe)

	result.Unsubscribe()
	time.Sleep(50 * time.Millisecond)
}

func TestDispatcher_HandleEventsEmpty(t *testing.T) {
	d, svc, _ := newTestDispatcher(t)

	eventsPID := mkPID("host1", "events")
	cmd := &pgapi.EventsCmd{
		Instance: svc,
		PID:      eventsPID,
		Topic:    "pg.event",
	}

	receiver := &mockResultReceiver{}
	err := d.handleEvents(context.Background(), cmd, 1, receiver)
	require.NoError(t, err)

	result, ok := receiver.data.(pgapi.EventsResult)
	require.True(t, ok)
	assert.Empty(t, result.Groups)
	assert.NotNil(t, result.Unsubscribe)

	result.Unsubscribe()
}

// --- JoinGroups dispatcher tests ---

func TestDispatcher_HandleJoinGroups(t *testing.T) {
	d, svc, _ := newTestDispatcher(t)

	p1 := mkPID("host1", "1")
	cmd := &pgapi.JoinGroupsCmd{
		Instance: svc,
		Caller:   p1,
		Groups:   []string{"workers", "managers"},
	}

	receiver := &mockResultReceiver{}
	err := d.handleJoinGroups(context.Background(), cmd, 1, receiver)
	require.NoError(t, err)

	result, ok := receiver.data.(pgapi.JoinGroupsResult)
	require.True(t, ok)
	assert.Nil(t, result.Error)

	assert.Len(t, svc.GetMembers("workers"), 1)
	assert.Len(t, svc.GetMembers("managers"), 1)
}

// --- LeaveGroups dispatcher tests ---

func TestDispatcher_HandleLeaveGroups(t *testing.T) {
	d, svc, _ := newTestDispatcher(t)

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.JoinGroups([]string{"workers", "managers"}, p1))

	cmd := &pgapi.LeaveGroupsCmd{
		Instance: svc,
		Caller:   p1,
		Groups:   []string{"workers", "managers"},
	}

	receiver := &mockResultReceiver{}
	err := d.handleLeaveGroups(context.Background(), cmd, 1, receiver)
	require.NoError(t, err)

	result, ok := receiver.data.(pgapi.LeaveGroupsResult)
	require.True(t, ok)
	assert.Nil(t, result.Error)

	assert.Empty(t, svc.GetMembers("workers"))
	assert.Empty(t, svc.GetMembers("managers"))
}

func TestDispatcher_HandleLeaveGroupsNotJoined(t *testing.T) {
	d, svc, _ := newTestDispatcher(t)

	p1 := mkPID("host1", "1")
	cmd := &pgapi.LeaveGroupsCmd{
		Instance: svc,
		Caller:   p1,
		Groups:   []string{"workers"},
	}

	receiver := &mockResultReceiver{}
	err := d.handleLeaveGroups(context.Background(), cmd, 1, receiver)
	require.NoError(t, err)

	result, ok := receiver.data.(pgapi.LeaveGroupsResult)
	require.True(t, ok)
	assert.ErrorIs(t, result.Error, ErrNotJoined)
}
