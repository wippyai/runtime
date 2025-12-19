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

func (m *mockTopology) Complete(p pid.PID, result *runtime.Result) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notified[p.String()] = result
	m.removed = append(m.removed, p.String())
	delete(m.registered, p.String())
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

func (m *mockPIDRegistry) Register(_ string, p pid.PID) (pid.PID, error) {
	return p, nil
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

	err := lc.OnStart(ctx, testPID, nil)
	require.NoError(t, err)

	assert.True(t, topo.registered[testPID.String()])
}

func TestLifecycle_OnStart_SetsUpMonitoring(t *testing.T) {
	topo := newMockTopology()
	logger := zap.NewNop()

	lc := NewLifecycle(topo, nil, logger)

	parentPID := pid.PID{UniqID: "parent"}
	childPID := pid.PID{UniqID: "child"}
	ctx := createContextWithOptions(parentPID, true, false)

	err := lc.OnStart(ctx, childPID, nil)
	require.NoError(t, err)

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

	err := lc.OnStart(ctx, childPID, nil)
	require.NoError(t, err)

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

	err := lc.OnStart(ctx, childPID, nil)
	require.NoError(t, err)

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

	err := lc.OnStart(ctx, childPID, nil)
	require.NoError(t, err)

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

	err := lc.OnStart(context.Background(), testPID, nil)
	require.NoError(t, err)

	assert.True(t, topo.registered[testPID.String()])
}

func TestLifecycle_OnStart_RegisterFails(t *testing.T) {
	topo := newMockTopology()
	topo.registerErr = errors.New("registration failed")
	logger := zap.NewNop()

	lc := NewLifecycle(topo, nil, logger)

	parentPID := pid.PID{UniqID: "parent"}
	childPID := pid.PID{UniqID: "child"}
	ctx := createContextWithOptions(parentPID, true, true)

	// Should not panic, just log warning and return early
	_ = lc.OnStart(ctx, childPID, nil)

	// Should not have set up monitoring or linking since register failed
	assert.Empty(t, topo.monitors)
	assert.Empty(t, topo.links)
}

func TestLifecycle_OnStart_MonitorFails(t *testing.T) {
	topo := newMockTopology()
	topo.waitErr = errors.New("monitor failed")
	logger := zap.NewNop()

	lc := NewLifecycle(topo, nil, logger)

	parentPID := pid.PID{UniqID: "parent"}
	childPID := pid.PID{UniqID: "child"}
	ctx := createContextWithOptions(parentPID, true, false)

	// Should not panic, just log warning
	_ = lc.OnStart(ctx, childPID, nil)

	// Registration should still succeed
	assert.True(t, topo.registered[childPID.String()])
	// Monitor should have failed
	assert.Empty(t, topo.monitors)
}

func TestLifecycle_OnStart_LinkFails(t *testing.T) {
	topo := newMockTopology()
	topo.linkErr = errors.New("link failed")
	logger := zap.NewNop()

	lc := NewLifecycle(topo, nil, logger)

	parentPID := pid.PID{UniqID: "parent"}
	childPID := pid.PID{UniqID: "child"}
	ctx := createContextWithOptions(parentPID, false, true)

	// Should not panic, just log warning
	_ = lc.OnStart(ctx, childPID, nil)

	// Registration should still succeed
	assert.True(t, topo.registered[childPID.String()])
	// Link should have failed
	assert.Empty(t, topo.links)
}

func TestLifecycle_OnStart_InvalidOptionsType(t *testing.T) {
	topo := newMockTopology()
	logger := zap.NewNop()

	lc := NewLifecycle(topo, nil, logger)

	testPID := pid.PID{UniqID: "test-process"}

	// Create context with non-attrs.Attributes options
	appCtx := ctxapi.NewAppContext()
	ctx := ctxapi.WithAppContext(context.Background(), appCtx)
	ctx, fc := ctxapi.OpenFrameContext(ctx)
	_ = fc.Set(runtime.FrameLifecycleOptionsKey, "not-an-attributes")

	err := lc.OnStart(ctx, testPID, nil)
	require.NoError(t, err)

	// Registration should succeed, but no monitor/link setup
	assert.True(t, topo.registered[testPID.String()])
	assert.Empty(t, topo.monitors)
	assert.Empty(t, topo.links)
}

func TestLifecycle_OnStart_NameRegistration(t *testing.T) {
	topo := newMockTopology()
	pidReg := NewPIDRegistry()
	logger := zap.NewNop()

	lc := NewLifecycle(topo, pidReg, logger)

	testPID := pid.PID{UniqID: "test-process"}

	// Create context with name in lifecycle options
	appCtx := ctxapi.NewAppContext()
	ctx := ctxapi.WithAppContext(context.Background(), appCtx)
	ctx, fc := ctxapi.OpenFrameContext(ctx)
	options := attrs.NewBag()
	options.Set(process.LifecycleNameKey, "my-service")
	_ = fc.Set(runtime.FrameLifecycleOptionsKey, options)

	err := lc.OnStart(ctx, testPID, nil)
	require.NoError(t, err)

	// Verify name was registered
	foundPID, ok := pidReg.Lookup("my-service")
	assert.True(t, ok)
	assert.Equal(t, testPID, foundPID)
}

func TestLifecycle_OnStart_NameAlreadyTaken(t *testing.T) {
	topo := newMockTopology()
	pidReg := NewPIDRegistry()
	logger := zap.NewNop()

	lc := NewLifecycle(topo, pidReg, logger)

	existingPID := pid.PID{UniqID: "existing-process"}
	newPID := pid.PID{UniqID: "new-process"}

	// Register existing process with name
	_, err := pidReg.Register("my-service", existingPID)
	require.NoError(t, err)

	// Try to start a new process with the same name
	appCtx := ctxapi.NewAppContext()
	ctx := ctxapi.WithAppContext(context.Background(), appCtx)
	ctx, fc := ctxapi.OpenFrameContext(ctx)
	options := attrs.NewBag()
	options.Set(process.LifecycleNameKey, "my-service")
	_ = fc.Set(runtime.FrameLifecycleOptionsKey, options)

	err = lc.OnStart(ctx, newPID, nil)
	require.Error(t, err)

	// Verify error is ErrNameAlreadyRegistered with correct existing PID in details
	require.True(t, errors.Is(err, topology.ErrNameAlreadyRegistered))
	existingFromErr, ok := topology.GetExistingPID(err)
	require.True(t, ok)
	assert.Equal(t, existingPID, existingFromErr)
}

func TestNameAlreadyRegisteredError(t *testing.T) {
	existingPID := pid.PID{UniqID: "existing"}
	err := topology.NameAlreadyRegisteredError(existingPID)

	// Verify error message
	assert.Contains(t, err.Error(), "name already registered")

	// Verify errors.Is works
	assert.True(t, errors.Is(err, topology.ErrNameAlreadyRegistered))

	// Verify GetExistingPID extracts the PID
	gotPID, ok := topology.GetExistingPID(err)
	require.True(t, ok)
	assert.Equal(t, existingPID, gotPID)

	// Verify GetExistingPID returns false for unrelated errors
	_, ok = topology.GetExistingPID(errors.New("unrelated"))
	assert.False(t, ok)

	// Verify GetExistingPID returns false for nil
	_, ok = topology.GetExistingPID(nil)
	assert.False(t, ok)
}

var _ topology.Topology = (*mockTopology)(nil)
var _ topology.PIDRegistry = (*mockPIDRegistry)(nil)
