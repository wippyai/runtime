package topology

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/api/topology"
	"go.uber.org/zap"
)

type mockTopology struct {
	mu          sync.Mutex
	registered  map[string]bool
	monitors    map[string][]string
	links       map[string][]string
	notified    map[string]*runtime.Result
	removed     []string
	registerErr error
	waitErr     error
	linkErr     error
}

func newMockTopology() *mockTopology {
	return &mockTopology{
		registered: make(map[string]bool),
		monitors:   make(map[string][]string),
		links:      make(map[string][]string),
		notified:   make(map[string]*runtime.Result),
	}
}

func (m *mockTopology) Register(p pid.PID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.registerErr != nil {
		return m.registerErr
	}
	m.registered[p.String()] = true
	return nil
}

func (m *mockTopology) Monitor(caller, p pid.PID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.waitErr != nil {
		return m.waitErr
	}
	m.monitors[p.String()] = append(m.monitors[p.String()], caller.String())
	return nil
}

func (m *mockTopology) Demonitor(_, _ pid.PID) error {
	return nil
}

func (m *mockTopology) Link(from, to pid.PID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.linkErr != nil {
		return m.linkErr
	}
	m.links[from.String()] = append(m.links[from.String()], to.String())
	return nil
}

func (m *mockTopology) Unlink(_, _ pid.PID) error {
	return nil
}

func (m *mockTopology) GetLinks(_ pid.PID) []pid.PID {
	return nil
}

func (m *mockTopology) Notify(p pid.PID, result *runtime.Result) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notified[p.String()] = result
}

func (m *mockTopology) Remove(p pid.PID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removed = append(m.removed, p.String())
	delete(m.registered, p.String())
}

type mockPIDRegistry struct {
	mu      sync.Mutex
	removed []string
}

func (m *mockPIDRegistry) Register(_ string, _ pid.PID) error {
	return nil
}

func (m *mockPIDRegistry) Unregister(_ string) bool {
	return true
}

func (m *mockPIDRegistry) Lookup(_ string) (pid.PID, bool) {
	return pid.PID{}, false
}

func (m *mockPIDRegistry) Remove(p pid.PID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removed = append(m.removed, p.String())
}

func createContextWithOptions(parent pid.PID, monitor, link bool) context.Context {
	appCtx := ctxapi.NewAppContext()
	ctx := ctxapi.WithAppContext(context.Background(), appCtx)
	ctx, fc := ctxapi.OpenFrameContext(ctx)

	options := attrs.NewBag()
	if parent.UniqID != "" {
		options.Set(process.LifecycleParentKey, parent)
	}
	if monitor {
		options.Set(process.LifecycleMonitorKey, true)
	}
	if link {
		options.Set(process.LifecycleLinkKey, true)
	}

	_ = fc.Set(runtime.FrameLifecycleOptionsKey, options)
	return ctx
}

func TestLifecycle_OnStart_RegistersProcess(t *testing.T) {
	topo := newMockTopology()
	logger := zap.NewNop()

	lc := NewLifecycle(topo, nil, logger)

	testPID := pid.PID{UniqID: "test-process"}
	ctx := createContextWithOptions(pid.PID{}, false, false)

	lc.OnStart(ctx, testPID, nil)

	assert.True(t, topo.registered[testPID.String()])
}

func TestLifecycle_OnStart_SetsUpMonitoring(t *testing.T) {
	topo := newMockTopology()
	logger := zap.NewNop()

	lc := NewLifecycle(topo, nil, logger)

	parentPID := pid.PID{UniqID: "parent"}
	childPID := pid.PID{UniqID: "child"}
	ctx := createContextWithOptions(parentPID, true, false)

	lc.OnStart(ctx, childPID, nil)

	assert.True(t, topo.registered[childPID.String()])
	require.Contains(t, topo.monitors[childPID.String()], parentPID.String())
}

func TestLifecycle_OnStart_SetsUpLink(t *testing.T) {
	topo := newMockTopology()
	logger := zap.NewNop()

	lc := NewLifecycle(topo, nil, logger)

	parentPID := pid.PID{UniqID: "parent"}
	childPID := pid.PID{UniqID: "child"}
	ctx := createContextWithOptions(parentPID, false, true)

	lc.OnStart(ctx, childPID, nil)

	assert.True(t, topo.registered[childPID.String()])
	require.Contains(t, topo.links[parentPID.String()], childPID.String())
}

func TestLifecycle_OnStart_MonitorAndLink(t *testing.T) {
	topo := newMockTopology()
	logger := zap.NewNop()

	lc := NewLifecycle(topo, nil, logger)

	parentPID := pid.PID{UniqID: "parent"}
	childPID := pid.PID{UniqID: "child"}
	ctx := createContextWithOptions(parentPID, true, true)

	lc.OnStart(ctx, childPID, nil)

	assert.True(t, topo.registered[childPID.String()])
	require.Contains(t, topo.monitors[childPID.String()], parentPID.String())
	require.Contains(t, topo.links[parentPID.String()], childPID.String())
}

func TestLifecycle_OnStart_NoParent(t *testing.T) {
	topo := newMockTopology()
	logger := zap.NewNop()

	lc := NewLifecycle(topo, nil, logger)

	childPID := pid.PID{UniqID: "child"}
	ctx := createContextWithOptions(pid.PID{}, true, true)

	lc.OnStart(ctx, childPID, nil)

	assert.True(t, topo.registered[childPID.String()])
	assert.Empty(t, topo.monitors)
	assert.Empty(t, topo.links)
}

func TestLifecycle_OnComplete_NotifiesAndRemoves(t *testing.T) {
	topo := newMockTopology()
	pidReg := &mockPIDRegistry{}
	logger := zap.NewNop()

	lc := NewLifecycle(topo, pidReg, logger)

	testPID := pid.PID{UniqID: "test-process"}
	result := &runtime.Result{Value: nil, Error: nil}

	lc.OnComplete(context.Background(), testPID, result)

	assert.Contains(t, topo.notified, testPID.String())
	assert.Equal(t, result, topo.notified[testPID.String()])
	assert.Contains(t, topo.removed, testPID.String())
	assert.Contains(t, pidReg.removed, testPID.String())
}

func TestLifecycle_OnComplete_ConvertsErrExit(t *testing.T) {
	topo := newMockTopology()
	logger := zap.NewNop()

	lc := NewLifecycle(topo, nil, logger)

	testPID := pid.PID{UniqID: "test-process"}
	result := &runtime.Result{Error: supervisor.ErrExit}

	lc.OnComplete(context.Background(), testPID, result)

	assert.Nil(t, topo.notified[testPID.String()].Error)
}

func TestLifecycle_OnComplete_PreservesOtherErrors(t *testing.T) {
	topo := newMockTopology()
	logger := zap.NewNop()

	lc := NewLifecycle(topo, nil, logger)

	testPID := pid.PID{UniqID: "test-process"}
	customErr := errors.New("custom error")
	result := &runtime.Result{Error: customErr}

	lc.OnComplete(context.Background(), testPID, result)

	assert.Equal(t, customErr, topo.notified[testPID.String()].Error)
}

func TestLifecycle_OnStart_NoContext(t *testing.T) {
	topo := newMockTopology()
	logger := zap.NewNop()

	lc := NewLifecycle(topo, nil, logger)

	testPID := pid.PID{UniqID: "test-process"}

	lc.OnStart(context.Background(), testPID, nil)

	assert.True(t, topo.registered[testPID.String()])
}

var _ topology.Topology = (*mockTopology)(nil)
var _ topology.PIDRegistry = (*mockPIDRegistry)(nil)
