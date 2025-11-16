package process

import (
	"context"
	"errors"
	"testing"
	"time"

	toposystem "github.com/wippyai/runtime/system/topology"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pidgen"
	api "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/internal/uniqid"
	"go.uber.org/zap"
)

// Mock implementation for HostLookup interface (unique to manager tests)
type managerHostLookup struct {
	hosts map[string]api.Host
}

func newManagerHostLookup() *managerHostLookup {
	return &managerHostLookup{
		hosts: make(map[string]api.Host),
	}
}

func (m *managerHostLookup) AddHost(id string, host api.Host) {
	m.hosts[id] = host
}

func (m *managerHostLookup) GetHost(hostID string) (api.Host, bool) {
	host, exists := m.hosts[hostID]
	return host, exists
}

// Manager-specific managed host mock
type managerManagedHost struct {
	launchErr    error
	terminateErr error
	sendErr      error
	launched     bool
	terminated   bool
	lastLaunch   *api.Launch
	lastCancel   *relay.Package
}

func (m *managerManagedHost) Send(pkg *relay.Package) error {
	m.lastCancel = pkg
	return m.sendErr
}

func (m *managerManagedHost) Terminate(_ context.Context, _ relay.PID) error {
	m.terminated = true
	return m.terminateErr
}

func (m *managerManagedHost) Launch(_ context.Context, launch *api.Launch) (relay.PID, error) {
	if m.launchErr != nil {
		return relay.PID{}, m.launchErr
	}

	m.launched = true
	m.lastLaunch = launch

	// Return a Target with the provided values but a modified UniqID
	return relay.PID{
		Node:   launch.PID.Node,
		Host:   launch.PID.Host,
		UniqID: "managed-host-assigned-" + launch.PID.UniqID,
	}, nil
}

// Manager-specific delegated host mock
type managerDelegatedHost struct {
	launchErr     error
	terminateErr  error
	sendErr       error
	launched      bool
	terminated    bool
	lastPID       relay.PID
	lastLifecycle api.Lifecycle
	lastDispatch  *api.Dispatch
	lastCancel    *relay.Package
}

func (m *managerDelegatedHost) Send(pkg *relay.Package) error {
	m.lastCancel = pkg
	return m.sendErr
}

func (m *managerDelegatedHost) Terminate(_ context.Context, _ relay.PID) error {
	m.terminated = true
	return m.terminateErr
}

// Dispatch implements the Delegated interface
func (m *managerDelegatedHost) Dispatch(_ context.Context, lf api.Lifecycle, dispatch *api.Dispatch) (relay.PID, error) {
	if m.launchErr != nil {
		return relay.PID{}, m.launchErr
	}

	m.launched = true
	m.lastPID = dispatch.PID
	m.lastLifecycle = lf
	m.lastDispatch = dispatch

	// Return a PID with the provided values but a modified UniqID
	return relay.PID{
		Node:   "delegated-node",
		Host:   dispatch.PID.Host,
		UniqID: "delegated-host-assigned-" + dispatch.PID.UniqID,
	}, nil
}

// Manager-specific mock process
type managerProcessMock struct {
	createErr error
}

func (m *managerProcessMock) Create(_ registry.ID) (api.Process, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	// Use the existing mockProcess type which is already defined in prototype_registry_test.go
	return &mockProcess{}, nil
}

// Updated mockProcess implementation for manager_test.go
type mockProcess struct {
	sendErr error
	stepErr error
}

func (m *mockProcess) Send(_ *relay.Package) error {
	return m.sendErr
}

func (m *mockProcess) Start(_ context.Context, _ relay.PID, _ payload.Payloads) error {
	return nil
}

func (m *mockProcess) Step() (api.StepResult, error) {
	if m.stepErr != nil {
		return api.StepDone, m.stepErr
	}
	return api.StepIdle, nil
}

func (m *mockProcess) Terminate() {
	// No-op implementation for tests
}

// Manager-specific mock topology
type managerTopology struct {
	registerErr error
	waitErr     error
	linkErr     error

	registered []relay.PID
	waited     map[relay.PID]relay.PID
	linked     map[relay.PID]relay.PID
	notified   map[relay.PID]*runtime.Result
	removed    []relay.PID
}

func newManagerTopology() *managerTopology {
	return &managerTopology{
		waited:   make(map[relay.PID]relay.PID),
		linked:   make(map[relay.PID]relay.PID),
		notified: make(map[relay.PID]*runtime.Result),
	}
}

func (m *managerTopology) Register(pid relay.PID) error {
	if m.registerErr != nil {
		return m.registerErr
	}
	m.registered = append(m.registered, pid)
	return nil
}

func (m *managerTopology) Wait(from, to relay.PID) error {
	if m.waitErr != nil {
		return m.waitErr
	}
	m.waited[from] = to
	return nil
}

func (m *managerTopology) Link(from, to relay.PID) error {
	if m.linkErr != nil {
		return m.linkErr
	}
	m.linked[from] = to
	return nil
}

func (m *managerTopology) Notify(pid relay.PID, result *runtime.Result) {
	m.notified[pid] = result
}

func (m *managerTopology) Remove(pid relay.PID) {
	m.removed = append(m.removed, pid)
}

func (m *managerTopology) Unlink(from, _ relay.PID) error {
	delete(m.linked, from)
	return nil
}

func (m *managerTopology) GetLinks(_ relay.PID) []relay.PID {
	return nil
}

func (m *managerTopology) Release(caller, _ relay.PID) error {
	delete(m.waited, caller)
	return nil
}

// Helper to create a context with mock topology
//
//nolint:unparam // managerTopology return value used in some tests
func contextWithManagerTopology() (context.Context, *managerTopology) {
	topo := newManagerTopology()
	ctx := ctxapi.NewRootContext()

	ctx = topology.WithTopology(ctx, topo)
	ctx = topology.WithRegistry(ctx, toposystem.NewPIDRegistry(toposystem.PIDRegistryConfig{}))

	// Add PID generator to context
	uniqGen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(uniqGen, "")
	ctx = pidgen.WithGenerator(ctx, pidGen)

	return ctx, topo
}

func TestManager_Start_ManagedHost(t *testing.T) {
	// Setup test dependencies
	nodeID := relay.NodeID("test-node")
	logger := zap.NewNop()

	hostLookup := newManagerHostLookup()
	factory := &managerProcessMock{}
	ctx, _ := contextWithManagerTopology()

	// Create the managed host mock
	managedHost := &managerManagedHost{}
	hostID := "managed-host"
	hostLookup.AddHost(hostID, managedHost)

	// Create the manager
	manager := NewProcessManager(hostLookup, factory, nodeID, logger)

	// Test data
	sourceID := registry.ID{NS: "test", Name: "process"}
	uniqID := "test-uniq"
	inputs := payload.Payloads{}
	parentPID := relay.PID{
		Node:   "parent-node",
		Host:   "parent-host",
		UniqID: "parent-uniq",
	}

	// Execute the test
	opts := attrs.NewBag()
	opts.Set(api.LifecycleParentKey, parentPID)
	opts.Set(api.LifecycleMonitorKey, true)
	opts.Set(api.LifecycleLinkKey, true)

	startReq := &api.Start{
		HostID:  hostID,
		Source:  sourceID,
		UniqID:  uniqID,
		Input:   inputs,
		Options: opts,
	}

	resultPID, err := manager.Start(ctx, startReq)

	// Assertions
	require.NoError(t, err)
	assert.True(t, managedHost.launched, "Expected host Launch to be called")
	assert.Equal(t, "managed-host-assigned-test-uniq", resultPID.UniqID)
	assert.Equal(t, hostID, resultPID.Host)
	assert.Equal(t, nodeID, resultPID.Node)

	// Verify Launch parameters
	require.NotNil(t, managedHost.lastLaunch)
	assert.Equal(t, uniqID, managedHost.lastLaunch.PID.UniqID)
	assert.Equal(t, nodeID, managedHost.lastLaunch.PID.Node)
	assert.Equal(t, hostID, managedHost.lastLaunch.PID.Host)
	assert.Equal(t, inputs, managedHost.lastLaunch.Input)
	// Lifecycle is stored in Options
	parentVal, _ := managedHost.lastLaunch.Options.Get(api.LifecycleParentKey)
	assert.Equal(t, parentPID, parentVal)
	assert.True(t, managedHost.lastLaunch.Options.GetBool(api.LifecycleMonitorKey, false))
	assert.True(t, managedHost.lastLaunch.Options.GetBool(api.LifecycleLinkKey, false))
}

func TestManager_Start_DelegatedHost(t *testing.T) {
	// Setup test dependencies
	nodeID := relay.NodeID("test-node")
	logger := zap.NewNop()

	hostLookup := newManagerHostLookup()
	factory := &managerProcessMock{}
	ctx, _ := contextWithManagerTopology()

	// Create the delegated host mock
	delegatedHost := &managerDelegatedHost{}
	hostID := "delegated-host"
	hostLookup.AddHost(hostID, delegatedHost)

	// Create the manager
	manager := NewProcessManager(hostLookup, factory, nodeID, logger)

	// Test data
	sourceID := registry.ID{NS: "test", Name: "process"}
	uniqID := "test-uniq"
	inputs := payload.Payloads{}
	lifecycle := api.Lifecycle{
		Monitor: true,
		Link:    true,
	}

	opts := attrs.NewBag()
	opts.Set(api.LifecycleMonitorKey, true)
	opts.Set(api.LifecycleLinkKey, true)

	// Execute the test
	startReq := &api.Start{
		HostID:  hostID,
		Source:  sourceID,
		UniqID:  uniqID,
		Input:   inputs,
		Options: opts,
	}

	resultPID, err := manager.Start(ctx, startReq)

	// Assertions
	require.NoError(t, err)
	assert.True(t, delegatedHost.launched, "Expected host Launch to be called")
	assert.Equal(t, "delegated-host-assigned-test-uniq", resultPID.UniqID)
	assert.Equal(t, hostID, resultPID.Host)
	assert.Equal(t, "delegated-node", resultPID.Node)

	// Verify Dispatch parameters
	assert.Equal(t, uniqID, delegatedHost.lastPID.UniqID)
	assert.Equal(t, hostID, delegatedHost.lastPID.Host)
	require.NotNil(t, delegatedHost.lastDispatch)
	assert.Equal(t, inputs, delegatedHost.lastDispatch.Input)
	assert.Equal(t, sourceID, delegatedHost.lastDispatch.Source)
	assert.Equal(t, lifecycle, delegatedHost.lastLifecycle)
}

func TestManager_Start_HostNotFound(t *testing.T) {
	// Setup test dependencies
	nodeID := relay.NodeID("test-node")
	logger := zap.NewNop()

	hostLookup := newManagerHostLookup()
	factory := &managerProcessMock{}
	ctx, _ := contextWithManagerTopology()

	// Create the manager
	manager := NewProcessManager(hostLookup, factory, nodeID, logger)

	// Test data
	hostID := "nonexistent-host"
	sourceID := registry.ID{NS: "test", Name: "process"}

	// Execute the test
	startReq := &api.Start{
		HostID: hostID,
		Source: sourceID,
	}

	_, err := manager.Start(ctx, startReq)

	// Assertions
	require.Error(t, err)
	assert.Contains(t, err.Error(), "host not found")
}

func TestManager_Start_ProcessCreationFails(t *testing.T) {
	// Setup test dependencies
	nodeID := relay.NodeID("test-node")
	logger := zap.NewNop()

	hostLookup := newManagerHostLookup()
	factory := &managerProcessMock{
		createErr: errors.New("process creation error"),
	}
	ctx, _ := contextWithManagerTopology()

	// Create the managed host mock
	managedHost := &managerManagedHost{}
	hostID := "managed-host"
	hostLookup.AddHost(hostID, managedHost)

	// Create the manager
	manager := NewProcessManager(hostLookup, factory, nodeID, logger)

	// Test data
	sourceID := registry.ID{NS: "test", Name: "process"}

	// Execute the test
	startReq := &api.Start{
		HostID: hostID,
		Source: sourceID,
	}

	_, err := manager.Start(ctx, startReq)

	// Assertions
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to init launch")
	assert.False(t, managedHost.launched, "Host should not be launched when process creation fails")
}

func TestManager_Cancel(t *testing.T) {
	// Setup test dependencies
	nodeID := relay.NodeID("test-node")
	logger := zap.NewNop()

	hostLookup := newManagerHostLookup()
	factory := &managerProcessMock{}
	ctx := context.Background()

	// Create the host mock
	host := &managerManagedHost{}
	hostID := "test-host"
	hostLookup.AddHost(hostID, host)

	// Create the manager
	manager := NewProcessManager(hostLookup, factory, nodeID, logger)

	// Test data
	fromPID := relay.PID{
		Node:   "from-node",
		Host:   "from-host",
		UniqID: "from-uniq",
	}

	targetPID := relay.PID{
		Node:   "target-node",
		Host:   hostID,
		UniqID: "target-uniq",
	}

	deadline := time.Now().Add(5 * time.Second)

	// Execute the test
	err := manager.Cancel(ctx, fromPID, targetPID, deadline)

	// Assertions
	require.NoError(t, err)
	assert.NotNil(t, host.lastCancel, "Expected cancel message to be sent")

	// Verify the cancel message contents
	if host.lastCancel != nil {
		assert.Equal(t, targetPID, host.lastCancel.Target)

		// Check if there's at least one message
		require.GreaterOrEqual(t, len(host.lastCancel.Messages), 1)

		// The first message should be for the events topic
		message := host.lastCancel.Messages[0]
		assert.Equal(t, topology.TopicEvents, message.Topic)

		// There should be at least one payload
		require.GreaterOrEqual(t, len(message.Payloads), 1)

		// The payload should be a CancelEvent
		if p := message.Payloads[0]; p != nil {
			if cancelEvent, ok := p.Data().(*topology.CancelEvent); ok {
				assert.Equal(t, fromPID, cancelEvent.From)
				assert.Equal(t, topology.KindCancel, cancelEvent.Kind)
				assert.WithinDuration(t, deadline, cancelEvent.Deadline, time.Second)
			} else {
				t.Fatalf("Expected CancelEvent, got %T", p.Data())
			}
		}
	}
}

func TestManager_Cancel_HostNotFound(t *testing.T) {
	// Setup test dependencies
	nodeID := relay.NodeID("test-node")
	logger := zap.NewNop()

	hostLookup := newManagerHostLookup()
	factory := &managerProcessMock{}
	ctx := context.Background()

	// Create the manager
	manager := NewProcessManager(hostLookup, factory, nodeID, logger)

	// Test data
	fromPID := relay.PID{
		Node:   "from-node",
		Host:   "from-host",
		UniqID: "from-uniq",
	}

	targetPID := relay.PID{
		Node:   "target-node",
		Host:   "nonexistent-host",
		UniqID: "target-uniq",
	}

	deadline := time.Now().Add(5 * time.Second)

	// Execute the test
	err := manager.Cancel(ctx, fromPID, targetPID, deadline)

	// Assertions
	require.Error(t, err)
	assert.Contains(t, err.Error(), "host not found")
}

func TestManager_Terminate(t *testing.T) {
	// Setup test dependencies
	nodeID := relay.NodeID("test-node")
	logger := zap.NewNop()

	hostLookup := newManagerHostLookup()
	factory := &managerProcessMock{}
	ctx := context.Background()

	// Create the host mock
	host := &managerManagedHost{}
	hostID := "test-host"
	hostLookup.AddHost(hostID, host)

	// Create the manager
	manager := NewProcessManager(hostLookup, factory, nodeID, logger)

	// Test data
	pid := relay.PID{
		Node:   "node",
		Host:   hostID,
		UniqID: "uniq",
	}

	// Execute the test
	err := manager.Terminate(ctx, pid)

	// Assertions
	require.NoError(t, err)
	assert.True(t, host.terminated, "Expected host Terminate to be called")
}

func TestManager_Terminate_HostNotFound(t *testing.T) {
	// Setup test dependencies
	nodeID := relay.NodeID("test-node")
	logger := zap.NewNop()

	hostLookup := newManagerHostLookup()
	factory := &managerProcessMock{}
	ctx := context.Background()

	// Create the manager
	manager := NewProcessManager(hostLookup, factory, nodeID, logger)

	// Test data
	pid := relay.PID{
		Node:   "node",
		Host:   "nonexistent-host",
		UniqID: "uniq",
	}

	// Execute the test
	err := manager.Terminate(ctx, pid)

	// Assertions
	require.Error(t, err)
	assert.Contains(t, err.Error(), "host not found")
}

func TestManager_GeneratesUniqID(t *testing.T) {
	// Setup test dependencies
	nodeID := relay.NodeID("test-node")
	logger := zap.NewNop()

	hostLookup := newManagerHostLookup()
	factory := &managerProcessMock{}
	ctx, _ := contextWithManagerTopology()

	// Create the managed host mock
	managedHost := &managerManagedHost{}
	hostID := "managed-host"
	hostLookup.AddHost(hostID, managedHost)

	// Create the manager
	manager := NewProcessManager(hostLookup, factory, nodeID, logger)

	// Test data with no UniqID specified
	sourceID := registry.ID{NS: "test", Name: "process"}
	inputs := payload.Payloads{}

	// Execute the test
	startReq := &api.Start{
		HostID: hostID,
		Source: sourceID,
		Input:  inputs,
	}

	_, err := manager.Start(ctx, startReq)

	// Assertions
	require.NoError(t, err)
	assert.NotEmpty(t, managedHost.lastLaunch.PID.UniqID, "Manager should generate a UniqID when none is provided")
}

// Invalid host type for testing
type managerInvalidHost struct{}

func (h *managerInvalidHost) Send(_ *relay.Package) error {
	return nil
}

func (h *managerInvalidHost) Terminate(_ context.Context, _ relay.PID) error {
	return nil
}

func TestManager_Start_InvalidHostType(t *testing.T) {
	// Setup test dependencies
	nodeID := relay.NodeID("test-node")
	logger := zap.NewNop()

	hostLookup := newManagerHostLookup()
	factory := &managerProcessMock{}
	ctx, _ := contextWithManagerTopology()

	// Create an invalid host type
	hostID := "invalid-host"
	hostLookup.AddHost(hostID, &managerInvalidHost{})

	manager := NewProcessManager(hostLookup, factory, nodeID, logger)

	// Test Start with invalid host type
	start := &api.Start{
		HostID:  hostID,
		Source:  registry.ParseID("test-process"),
		Input:   payload.Payloads{},
		Options: attrs.NewBag(),
	}

	_, err := manager.Start(ctx, start)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid host type")
}

func TestManager_Start_ProcessCreationError(t *testing.T) {
	// Setup test dependencies
	nodeID := relay.NodeID("test-node")
	logger := zap.NewNop()

	hostLookup := newManagerHostLookup()
	factory := &managerProcessMock{
		createErr: errors.New("process creation failed"),
	}
	ctx, _ := contextWithManagerTopology()

	// Create the managed host mock
	managedHost := &managerManagedHost{}
	hostID := "managed-host"
	hostLookup.AddHost(hostID, managedHost)

	manager := NewProcessManager(hostLookup, factory, nodeID, logger)

	// Test Start with process creation error
	start := &api.Start{
		HostID:  hostID,
		Source:  registry.ParseID("test-process"),
		Input:   payload.Payloads{},
		Options: attrs.NewBag(),
	}

	_, err := manager.Start(ctx, start)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to init launch")
}
