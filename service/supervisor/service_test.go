// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	processapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	supervisorapi "github.com/wippyai/runtime/api/service/supervisor"
	"github.com/wippyai/runtime/api/supervisor"
	topologyapi "github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/internal/uniqid"
)

// mockNode implements relay.Node for testing
type mockNode struct {
	sendErr    error
	attachErr  error
	attachCh   chan *relay.Package
	detachFunc context.CancelFunc
	sent       []*relay.Package
}

func (m *mockNode) Send(pkg *relay.Package) error {
	m.sent = append(m.sent, pkg)
	return m.sendErr
}

func (m *mockNode) ID() pid.NodeID { return "test-node" }

func (m *mockNode) RegisterHost(pid.HostID, relay.Receiver) error { return nil }
func (m *mockNode) UnregisterHost(pid.HostID)                     {}
func (m *mockNode) GetHost(pid.HostID) (relay.Receiver, bool)     { return nil, false }

func (m *mockNode) Attach(_ pid.PID, ch chan *relay.Package) (context.CancelFunc, error) {
	m.attachCh = ch
	if m.attachErr != nil {
		return nil, m.attachErr
	}
	m.detachFunc = func() {}
	return m.detachFunc, nil
}

func (m *mockNode) Detach(pid.PID) {}

// mockTopology implements topology.Topology for testing
type mockTopology struct {
	registerErr error
	registered  []pid.PID
	removed     []pid.PID
}

func (m *mockTopology) Register(p pid.PID) error {
	if m.registerErr != nil {
		return m.registerErr
	}
	m.registered = append(m.registered, p)
	return nil
}

func (m *mockTopology) Remove(p pid.PID) {
	m.removed = append(m.removed, p)
}

func (m *mockTopology) Monitor(_, _ pid.PID) error        { return nil }
func (m *mockTopology) Demonitor(_, _ pid.PID) error      { return nil }
func (m *mockTopology) Link(_, _ pid.PID) error           { return nil }
func (m *mockTopology) Unlink(_, _ pid.PID) error         { return nil }
func (m *mockTopology) GetLinks(pid.PID) []pid.PID        { return nil }
func (m *mockTopology) Complete(pid.PID, *runtime.Result) {}

// mockProcessManager implements process.Manager for testing
type mockProcessManager struct {
	startErr   error
	startedPID pid.PID
}

func (m *mockProcessManager) Start(_ context.Context, _ *processapi.Start) (pid.PID, error) {
	if m.startErr != nil {
		return pid.PID{}, m.startErr
	}
	return m.startedPID, nil
}

func (m *mockProcessManager) Cancel(context.Context, pid.PID, pid.PID, time.Time) error {
	return nil
}

func (m *mockProcessManager) Terminate(context.Context, pid.PID) error {
	return nil
}

func newTestService() *Service {
	pidGen := uniqid.NewPIDGenerator(uniqid.NewGenerator(), "test-node")
	id := registry.ID{NS: "test", Name: "svc"}
	config := supervisorapi.ServiceConfig{
		Process:   registry.ID{NS: "test", Name: "proc"},
		HostID:    "test-host",
		Lifecycle: supervisor.LifecycleConfig{StopTimeout: time.Second},
	}
	return NewService(id, config, pidGen)
}

func setupTestContext(node relay.Node, topo topologyapi.Topology, manager processapi.Manager) context.Context {
	appCtx := ctxapi.NewAppContext()
	ctx := ctxapi.WithAppContext(context.Background(), appCtx)
	if node != nil {
		ctx = relay.WithNode(ctx, node)
	}
	if topo != nil {
		ctx = topologyapi.WithTopology(ctx, topo)
	}
	if manager != nil {
		ctx = processapi.WithManager(ctx, manager)
	}
	return ctx
}

func TestNewService(t *testing.T) {
	pidGen := uniqid.NewPIDGenerator(uniqid.NewGenerator(), "test-node")
	id := registry.ID{NS: "test", Name: "svc"}
	config := supervisorapi.ServiceConfig{
		Process:   registry.ID{NS: "test", Name: "proc"},
		HostID:    "test-host",
		Lifecycle: supervisor.LifecycleConfig{},
	}

	svc := NewService(id, config, pidGen)

	assert.Equal(t, id, svc.id)
	assert.Equal(t, config, svc.config)
	assert.NotNil(t, svc.pidGen)
}

var _ supervisor.Service = (*Service)(nil)

func TestService_Start_Success(t *testing.T) {
	svc := newTestService()
	node := &mockNode{}
	topo := &mockTopology{}
	manager := &mockProcessManager{startedPID: pid.PID{UniqID: "child-123"}}
	ctx := setupTestContext(node, topo, manager)

	statusCh, err := svc.Start(ctx)

	require.NoError(t, err)
	assert.NotNil(t, statusCh)
	assert.Equal(t, "child-123", svc.childPID.UniqID)
	assert.Len(t, topo.registered, 1)
}

func TestService_Start_NoRelayNode(t *testing.T) {
	svc := newTestService()
	ctx := setupTestContext(nil, &mockTopology{}, &mockProcessManager{})

	statusCh, err := svc.Start(ctx)

	require.ErrorIs(t, err, ErrNoRelayNode)
	assert.Nil(t, statusCh)
}

func TestService_Start_NoTopology(t *testing.T) {
	svc := newTestService()
	ctx := setupTestContext(&mockNode{}, nil, &mockProcessManager{})

	statusCh, err := svc.Start(ctx)

	require.ErrorIs(t, err, ErrNoTopology)
	assert.Nil(t, statusCh)
}

func TestService_Start_NoProcessManager(t *testing.T) {
	svc := newTestService()
	ctx := setupTestContext(&mockNode{}, &mockTopology{}, nil)

	statusCh, err := svc.Start(ctx)

	require.ErrorIs(t, err, ErrNoProcessManager)
	assert.Nil(t, statusCh)
}

func TestService_Start_RegisterPIDError(t *testing.T) {
	svc := newTestService()
	topo := &mockTopology{registerErr: errors.New("register failed")}
	ctx := setupTestContext(&mockNode{}, topo, &mockProcessManager{})

	statusCh, err := svc.Start(ctx)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "register supervisor pid")
	assert.Nil(t, statusCh)
}

func TestService_Start_AttachRelayError(t *testing.T) {
	svc := newTestService()
	node := &mockNode{attachErr: errors.New("attach failed")}
	topo := &mockTopology{}
	ctx := setupTestContext(node, topo, &mockProcessManager{})

	statusCh, err := svc.Start(ctx)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "attach to relay")
	assert.Nil(t, statusCh)
	assert.Len(t, topo.removed, 1) // cleanup
}

func TestService_Start_ProcessStartError(t *testing.T) {
	svc := newTestService()
	node := &mockNode{}
	topo := &mockTopology{}
	manager := &mockProcessManager{startErr: errors.New("start failed")}
	ctx := setupTestContext(node, topo, manager)

	statusCh, err := svc.Start(ctx)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "start process")
	assert.Nil(t, statusCh)
	assert.Len(t, topo.removed, 1) // cleanup
}

func TestService_Stop_NotStarted(t *testing.T) {
	svc := newTestService()
	ctx := setupTestContext(&mockNode{}, nil, nil)

	err := svc.Stop(ctx)

	require.NoError(t, err)
}

func TestService_Stop_NoRelayNode(t *testing.T) {
	svc := newTestService()
	svc.statusCh = make(chan any, 1)
	ctx := setupTestContext(nil, nil, nil)

	err := svc.Stop(ctx)

	require.ErrorIs(t, err, ErrNoRelayNode)
}

func TestService_Stop_SendCancelError(t *testing.T) {
	svc := newTestService()
	svc.statusCh = make(chan any, 1)
	svc.childPID = pid.PID{UniqID: "child-123"}
	node := &mockNode{sendErr: errors.New("send failed")}
	ctx := setupTestContext(node, nil, nil)

	err := svc.Stop(ctx)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "send cancel")
}

func TestService_Stop_AlreadyExitedDoesNotSendCancel(t *testing.T) {
	svc := newTestService()
	svc.statusCh = make(chan any, 1)
	svc.statusCh <- errors.New("process failed")
	close(svc.statusCh)
	svc.childPID = pid.PID{UniqID: "child-123"}
	node := &mockNode{sendErr: errors.New("process not found")}
	ctx := setupTestContext(node, nil, nil)

	err := svc.Stop(ctx)

	require.NoError(t, err)
	assert.Empty(t, node.sent)
}

func TestService_Stop_AlreadyClosedDoesNotSendCancel(t *testing.T) {
	svc := newTestService()
	svc.statusCh = make(chan any)
	close(svc.statusCh)
	svc.childPID = pid.PID{UniqID: "child-123"}
	node := &mockNode{sendErr: errors.New("process not found")}
	ctx := setupTestContext(node, nil, nil)

	err := svc.Stop(ctx)

	require.NoError(t, err)
	assert.Empty(t, node.sent)
}

func TestService_Stop_Success(t *testing.T) {
	svc := newTestService()
	svc.statusCh = make(chan any, 1)
	svc.childPID = pid.PID{UniqID: "child-123"}
	svc.supervisorPID = pid.PID{UniqID: "supervisor-123"}
	node := &mockNode{}
	ctx := setupTestContext(node, nil, nil)

	go func() {
		time.Sleep(10 * time.Millisecond)
		close(svc.statusCh)
	}()

	err := svc.Stop(ctx)

	require.NoError(t, err)
	assert.Len(t, node.sent, 1)
}

func TestService_Stop_ContextCanceled(t *testing.T) {
	svc := newTestService()
	svc.statusCh = make(chan any, 1)
	svc.childPID = pid.PID{UniqID: "child-123"}
	node := &mockNode{}
	ctx, cancel := context.WithCancel(setupTestContext(node, nil, nil))

	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := svc.Stop(ctx)

	require.ErrorIs(t, err, context.Canceled)
}

func TestService_MonitorLoop_ContextCanceled(t *testing.T) {
	svc := newTestService()
	svc.statusCh = make(chan any, 1)
	svc.detachFn = func() {}
	monitorCh := make(chan *relay.Package, 1)
	ctx, cancel := context.WithCancel(context.Background())

	go svc.monitorLoop(ctx, monitorCh)

	cancel()
	time.Sleep(50 * time.Millisecond)

	select {
	case _, ok := <-svc.statusCh:
		assert.False(t, ok) // channel should be closed
	default:
		t.Fatal("status channel should be closed")
	}
}

func TestService_MonitorLoop_ChannelClosed(t *testing.T) {
	svc := newTestService()
	svc.statusCh = make(chan any, 1)
	svc.detachFn = func() {}
	monitorCh := make(chan *relay.Package, 1)
	ctx := context.Background()

	go svc.monitorLoop(ctx, monitorCh)

	close(monitorCh)
	time.Sleep(50 * time.Millisecond)

	select {
	case status := <-svc.statusCh:
		assert.Equal(t, supervisor.ErrExit, status)
	default:
		t.Fatal("should receive exit status")
	}
}

func TestService_MonitorLoop_ExitEventWithError(t *testing.T) {
	svc := newTestService()
	svc.statusCh = make(chan any, 1)
	svc.detachFn = func() {}
	monitorCh := make(chan *relay.Package, 1)
	ctx := context.Background()

	go svc.monitorLoop(ctx, monitorCh)

	exitEvent := &topologyapi.ExitEvent{
		Kind:   topologyapi.Exit,
		From:   pid.PID{UniqID: "child"},
		Result: &runtime.Result{Error: errors.New("process error")},
	}
	pkg := relay.NewPackage(pid.PID{}, pid.PID{}, topologyapi.TopicEvents, payload.New(exitEvent))
	monitorCh <- pkg

	time.Sleep(50 * time.Millisecond)

	select {
	case status := <-svc.statusCh:
		err, ok := status.(error)
		require.True(t, ok)
		assert.Contains(t, err.Error(), "process failed")
	default:
		t.Fatal("should receive error status")
	}
}

func TestService_MonitorLoop_ExitEventWithoutError(t *testing.T) {
	svc := newTestService()
	svc.statusCh = make(chan any, 1)
	svc.detachFn = func() {}
	monitorCh := make(chan *relay.Package, 1)
	ctx := context.Background()

	go svc.monitorLoop(ctx, monitorCh)

	exitEvent := &topologyapi.ExitEvent{
		Kind: topologyapi.Exit,
		From: pid.PID{UniqID: "child"},
	}
	pkg := relay.NewPackage(pid.PID{}, pid.PID{}, topologyapi.TopicEvents, payload.New(exitEvent))
	monitorCh <- pkg

	time.Sleep(50 * time.Millisecond)

	select {
	case status := <-svc.statusCh:
		assert.Equal(t, supervisor.ErrExit, status)
	default:
		t.Fatal("should receive exit status")
	}
}

func TestService_MonitorLoop_IgnoresNonEventsTopic(t *testing.T) {
	svc := newTestService()
	svc.statusCh = make(chan any, 1)
	svc.detachFn = func() {}
	monitorCh := make(chan *relay.Package, 1)
	ctx, cancel := context.WithCancel(context.Background())

	go svc.monitorLoop(ctx, monitorCh)

	pkg := relay.NewPackage(pid.PID{}, pid.PID{}, "other-topic", payload.New("data"))
	monitorCh <- pkg

	time.Sleep(50 * time.Millisecond)

	select {
	case <-svc.statusCh:
		t.Fatal("should not receive status for non-events topic")
	default:
		// expected
	}

	cancel()
}

func BenchmarkService_Start(b *testing.B) {
	node := &mockNode{}
	topo := &mockTopology{}
	manager := &mockProcessManager{startedPID: pid.PID{UniqID: "child-123"}}
	ctx := setupTestContext(node, topo, manager)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		svc := newTestService()
		_, _ = svc.Start(ctx)
	}
}

func BenchmarkNewService(b *testing.B) {
	pidGen := uniqid.NewPIDGenerator(uniqid.NewGenerator(), "test-node")
	id := registry.ID{NS: "test", Name: "svc"}
	config := supervisorapi.ServiceConfig{
		Process:   registry.ID{NS: "test", Name: "proc"},
		HostID:    "test-host",
		Lifecycle: supervisor.LifecycleConfig{},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = NewService(id, config, pidGen)
	}
}
