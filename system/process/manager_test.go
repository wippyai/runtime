package process

import (
	"context"
	"errors"
	"testing"
	"time"

	toposystem "github.com/ponyruntime/pony/system/topology"

	ctxapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/pidgen"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/api/topology"
	"github.com/ponyruntime/pony/internal/uniqid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// Mock implementation for HostLookup interface (unique to manager tests)
type managerHostLookup struct {
	hosts map[string]process.Host
}

func newManagerHostLookup() *managerHostLookup {
	return &managerHostLookup{
		hosts: make(map[string]process.Host),
	}
}

func (m *managerHostLookup) AddHost(id string, host process.Host) {
	m.hosts[id] = host
}

func (m *managerHostLookup) GetHost(hostID string) (process.Host, bool) {
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
	lastLaunch   *process.Launch
	lastCancel   *pubsub.Package
}

func (m *managerManagedHost) Send(pkg *pubsub.Package) error {
	m.lastCancel = pkg
	return m.sendErr
}

func (m *managerManagedHost) Terminate(_ context.Context, _ pubsub.PID) error {
	m.terminated = true
	return m.terminateErr
}

func (m *managerManagedHost) Launch(_ context.Context, launch *process.Launch) (pubsub.PID, error) {
	if m.launchErr != nil {
		return pubsub.PID{}, m.launchErr
	}

	m.launched = true
	m.lastLaunch = launch

	// Return a Target with the provided values but a modified UniqID
	return pubsub.PID{
		Node:   launch.PID.Node,
		Host:   launch.PID.Host,
		ID:     launch.PID.ID,
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
	lastPID       pubsub.PID
	lastLifecycle process.Lifecycle
	lastInput     payload.Payloads
	lastCancel    *pubsub.Package
}

func (m *managerDelegatedHost) Send(pkg *pubsub.Package) error {
	m.lastCancel = pkg
	return m.sendErr
}

func (m *managerDelegatedHost) Terminate(_ context.Context, _ pubsub.PID) error {
	m.terminated = true
	return m.terminateErr
}

// Updated to match the Delegated interface with Lifecycle parameter
func (m *managerDelegatedHost) Launch(_ context.Context, pid pubsub.PID, lf process.Lifecycle, input payload.Payloads) (pubsub.PID, error) {
	if m.launchErr != nil {
		return pubsub.PID{}, m.launchErr
	}

	m.launched = true
	m.lastPID = pid
	m.lastLifecycle = lf
	m.lastInput = input

	// Return a Target with the provided values but a modified UniqID
	return pubsub.PID{
		Node:   "delegated-node",
		Host:   pid.Host,
		ID:     pid.ID,
		UniqID: "delegated-host-assigned-" + pid.UniqID,
	}, nil
}

// Manager-specific mock process
type managerProcessMock struct {
	createErr error
}

func (m *managerProcessMock) Create(_ registry.ID) (process.Process, error) {
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

func (m *mockProcess) Send(_ *pubsub.Package) error {
	return m.sendErr
}

func (m *mockProcess) Start(_ context.Context, _ pubsub.PID, _ payload.Payloads) error {
	return nil
}

func (m *mockProcess) Step() error {
	return m.stepErr
}

func (m *mockProcess) Ready() int {
	return 0
}

func (m *mockProcess) Terminate() {
	// No-op implementation for tests
}

// Manager-specific mock topology
type managerTopology struct {
	registerErr error
	waitErr     error
	linkErr     error

	registered []pubsub.PID
	waited     map[pubsub.PID]pubsub.PID
	linked     map[pubsub.PID]pubsub.PID
	notified   map[pubsub.PID]*runtime.Result
	removed    []pubsub.PID
}

func newManagerTopology() *managerTopology {
	return &managerTopology{
		waited:   make(map[pubsub.PID]pubsub.PID),
		linked:   make(map[pubsub.PID]pubsub.PID),
		notified: make(map[pubsub.PID]*runtime.Result),
	}
}

func (m *managerTopology) Register(pid pubsub.PID) error {
	if m.registerErr != nil {
		return m.registerErr
	}
	m.registered = append(m.registered, pid)
	return nil
}

func (m *managerTopology) Wait(from, to pubsub.PID) error {
	if m.waitErr != nil {
		return m.waitErr
	}
	m.waited[from] = to
	return nil
}

func (m *managerTopology) Link(from, to pubsub.PID) error {
	if m.linkErr != nil {
		return m.linkErr
	}
	m.linked[from] = to
	return nil
}

func (m *managerTopology) Notify(pid pubsub.PID, result *runtime.Result) {
	m.notified[pid] = result
}

func (m *managerTopology) Remove(pid pubsub.PID) {
	m.removed = append(m.removed, pid)
}

func (m *managerTopology) Unlink(from, _ pubsub.PID) error {
	delete(m.linked, from)
	return nil
}

func (m *managerTopology) GetLinks(_ pubsub.PID) []pubsub.PID {
	return nil
}

func (m *managerTopology) Release(caller, _ pubsub.PID) error {
	delete(m.waited, caller)
	return nil
}

// Helper to create a context with mock topology
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
	nodeID := pubsub.NodeID("test-node")
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
	parentPID := pubsub.PID{
		Node:   "parent-node",
		Host:   "parent-host",
		ID:     registry.ID{NS: "parent", Name: "process"},
		UniqID: "parent-uniq",
	}

	// Execute the test
	startReq := &process.Start{
		HostID: hostID,
		Source: sourceID,
		UniqID: uniqID,
		Input:  inputs,
		Lifecycle: process.Lifecycle{
			Parent:  parentPID,
			Monitor: true,
			Link:    true,
		},
	}

	resultPID, err := manager.Start(ctx, startReq)

	// Assertions
	require.NoError(t, err)
	assert.True(t, managedHost.launched, "Expected host Launch to be called")
	assert.Equal(t, "managed-host-assigned-test-uniq", resultPID.UniqID)
	assert.Equal(t, hostID, resultPID.Host)
	assert.Equal(t, sourceID, resultPID.ID)
	assert.Equal(t, nodeID, resultPID.Node)

	// Verify Launch parameters
	require.NotNil(t, managedHost.lastLaunch)
	assert.Equal(t, uniqID, managedHost.lastLaunch.PID.UniqID)
	assert.Equal(t, nodeID, managedHost.lastLaunch.PID.Node)
	assert.Equal(t, hostID, managedHost.lastLaunch.PID.Host)
	assert.Equal(t, sourceID, managedHost.lastLaunch.PID.ID)
	assert.Equal(t, inputs, managedHost.lastLaunch.Input)
	assert.Equal(t, parentPID, managedHost.lastLaunch.Lifecycle.Parent)
	assert.True(t, managedHost.lastLaunch.Lifecycle.Monitor)
	assert.True(t, managedHost.lastLaunch.Lifecycle.Link)
}

func TestManager_Start_DelegatedHost(t *testing.T) {
	// Setup test dependencies
	nodeID := pubsub.NodeID("test-node")
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
	lifecycle := process.Lifecycle{
		Monitor: true,
		Link:    true,
	}

	// Execute the test
	startReq := &process.Start{
		HostID:    hostID,
		Source:    sourceID,
		UniqID:    uniqID,
		Input:     inputs,
		Lifecycle: lifecycle,
	}

	resultPID, err := manager.Start(ctx, startReq)

	// Assertions
	require.NoError(t, err)
	assert.True(t, delegatedHost.launched, "Expected host Launch to be called")
	assert.Equal(t, "delegated-host-assigned-test-uniq", resultPID.UniqID)
	assert.Equal(t, hostID, resultPID.Host)
	assert.Equal(t, sourceID, resultPID.ID)
	assert.Equal(t, "delegated-node", resultPID.Node)

	// Verify Launch parameters
	assert.Equal(t, uniqID, delegatedHost.lastPID.UniqID)
	assert.Equal(t, hostID, delegatedHost.lastPID.Host)
	assert.Equal(t, sourceID, delegatedHost.lastPID.ID)
	assert.Equal(t, inputs, delegatedHost.lastInput)
	assert.Equal(t, lifecycle, delegatedHost.lastLifecycle)
}

func TestManager_Start_HostNotFound(t *testing.T) {
	// Setup test dependencies
	nodeID := pubsub.NodeID("test-node")
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
	startReq := &process.Start{
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
	nodeID := pubsub.NodeID("test-node")
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
	startReq := &process.Start{
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
	nodeID := pubsub.NodeID("test-node")
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
	fromPID := pubsub.PID{
		Node:   "from-node",
		Host:   "from-host",
		ID:     registry.ID{NS: "from", Name: "process"},
		UniqID: "from-uniq",
	}

	targetPID := pubsub.PID{
		Node:   "target-node",
		Host:   hostID,
		ID:     registry.ID{NS: "target", Name: "process"},
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
	nodeID := pubsub.NodeID("test-node")
	logger := zap.NewNop()

	hostLookup := newManagerHostLookup()
	factory := &managerProcessMock{}
	ctx := context.Background()

	// Create the manager
	manager := NewProcessManager(hostLookup, factory, nodeID, logger)

	// Test data
	fromPID := pubsub.PID{
		Node:   "from-node",
		Host:   "from-host",
		ID:     registry.ID{NS: "from", Name: "process"},
		UniqID: "from-uniq",
	}

	targetPID := pubsub.PID{
		Node:   "target-node",
		Host:   "nonexistent-host",
		ID:     registry.ID{NS: "target", Name: "process"},
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
	nodeID := pubsub.NodeID("test-node")
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
	pid := pubsub.PID{
		Node:   "node",
		Host:   hostID,
		ID:     registry.ID{NS: "test", Name: "process"},
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
	nodeID := pubsub.NodeID("test-node")
	logger := zap.NewNop()

	hostLookup := newManagerHostLookup()
	factory := &managerProcessMock{}
	ctx := context.Background()

	// Create the manager
	manager := NewProcessManager(hostLookup, factory, nodeID, logger)

	// Test data
	pid := pubsub.PID{
		Node:   "node",
		Host:   "nonexistent-host",
		ID:     registry.ID{NS: "test", Name: "process"},
		UniqID: "uniq",
	}

	// Execute the test
	err := manager.Terminate(ctx, pid)

	// Assertions
	require.Error(t, err)
	assert.Contains(t, err.Error(), "host not found")
}

func TestManager_AttachLifecycle(t *testing.T) {
	// Setup test dependencies
	nodeID := pubsub.NodeID("test-node")
	logger := zap.NewNop()

	hostLookup := newManagerHostLookup()
	factory := &managerProcessMock{}
	ctx, topo := contextWithManagerTopology()

	// Create the manager
	manager := NewProcessManager(hostLookup, factory, nodeID, logger)

	// Test data
	parentPID := pubsub.PID{
		Node:   "parent-node",
		Host:   "parent-host",
		ID:     registry.ID{NS: "parent", Name: "process"},
		UniqID: "parent-uniq",
	}

	lifecycle := process.Lifecycle{
		Parent:  parentPID,
		Monitor: true,
		Link:    true,
	}

	// Set up a process and Target for the callbacks
	proc := &mockProcess{}
	pid := pubsub.PID{
		Node:   "node",
		Host:   "host",
		ID:     registry.ID{NS: "test", Name: "process"},
		UniqID: "uniq",
	}

	// Attach lifecycle
	ctxWithLifecycle := manager.AttachLifecycle(ctx, lifecycle)

	// Get the callbacks
	onStart := process.GetOnStart(ctxWithLifecycle)
	onComplete := process.GetOnComplete(ctxWithLifecycle)

	// Assert callbacks are present
	require.NotNil(t, onStart)
	require.NotNil(t, onComplete)

	// Test OnStart callback
	onStart(pid, proc)

	// Verify registration, monitoring, and linking
	require.Len(t, topo.registered, 1)
	assert.Equal(t, pid, topo.registered[0])
	assert.Equal(t, pid, topo.waited[parentPID])
	assert.Equal(t, pid, topo.linked[parentPID])

	// Test OnComplete callback with success
	successResult := &runtime.Result{
		Value: payload.New("success"),
	}
	onComplete(pid, successResult)

	// Verify notification and removal
	assert.Equal(t, successResult, topo.notified[pid])
	require.Len(t, topo.removed, 1)
	assert.Equal(t, pid, topo.removed[0])

	// Test OnComplete callback with error
	errorResult := &runtime.Result{
		Error: errors.New("process failed"),
	}
	onComplete(pid, errorResult)

	// Verify error is preserved
	assert.Equal(t, errorResult, topo.notified[pid])
	assert.NotNil(t, topo.notified[pid].Error)

	// Test OnComplete callback with ErrExit - should be converted to normal exit
	exitResult := &runtime.Result{
		Error: supervisor.ErrExit,
	}
	onComplete(pid, exitResult)

	// Verify error is cleared
	assert.Equal(t, exitResult, topo.notified[pid])
	assert.Nil(t, topo.notified[pid].Error, "Exit error should be cleared to nil")
}

func TestManager_GeneratesUniqID(t *testing.T) {
	// Setup test dependencies
	nodeID := pubsub.NodeID("test-node")
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
	startReq := &process.Start{
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

func (h *managerInvalidHost) Send(_ *pubsub.Package) error {
	return nil
}

func (h *managerInvalidHost) Terminate(_ context.Context, _ pubsub.PID) error {
	return nil
}

func TestManager_Start_InvalidHostType(t *testing.T) {
	// Setup test dependencies
	nodeID := pubsub.NodeID("test-node")
	logger := zap.NewNop()

	hostLookup := newManagerHostLookup()
	factory := &managerProcessMock{}
	ctx, _ := contextWithManagerTopology()

	// Create an invalid host type
	hostID := "invalid-host"
	hostLookup.AddHost(hostID, &managerInvalidHost{})

	manager := NewProcessManager(hostLookup, factory, nodeID, logger)

	// Test Start with invalid host type
	start := &process.Start{
		HostID:    hostID,
		Source:    registry.ParseID("test-process"),
		Input:     payload.Payloads{},
		Lifecycle: process.Lifecycle{},
	}

	_, err := manager.Start(ctx, start)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid host type")
}

func TestManager_Start_ProcessCreationError(t *testing.T) {
	// Setup test dependencies
	nodeID := pubsub.NodeID("test-node")
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
	start := &process.Start{
		HostID:    hostID,
		Source:    registry.ParseID("test-process"),
		Input:     payload.Payloads{},
		Lifecycle: process.Lifecycle{},
	}

	_, err := manager.Start(ctx, start)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to init launch")
}

func TestManager_AttachLifecycle_MissingTopology(t *testing.T) {
	// Setup test dependencies
	nodeID := pubsub.NodeID("test-node")
	logger := zap.NewNop()
	hostLookup := newManagerHostLookup()
	factory := &managerProcessMock{}

	manager := NewProcessManager(hostLookup, factory, nodeID, logger)

	// Test AttachLifecycle with missing topology
	ctx := ctxapi.NewRootContext()

	lifecycle := process.Lifecycle{
		Monitor: true,
		Link:    true,
		Parent:  pubsub.PID{Node: "test-node", Host: "test-host", ID: registry.ParseID("test-id")},
	}

	ctx = manager.AttachLifecycle(ctx, lifecycle)

	// Verify that the callbacks are attached but handle missing topology gracefully
	onStart := process.GetOnStart(ctx)
	onComplete := process.GetOnComplete(ctx)

	assert.NotNil(t, onStart)
	assert.NotNil(t, onComplete)

	// Test the callbacks with missing topology
	pid := pubsub.PID{Node: "test-node", Host: "test-host", ID: registry.ParseID("test-id")}
	onStart(pid, &mockProcess{})
	onComplete(pid, &runtime.Result{})
}

func TestManager_AttachLifecycle_MissingPIDRegistry(t *testing.T) {
	// Setup test dependencies
	nodeID := pubsub.NodeID("test-node")
	logger := zap.NewNop()
	hostLookup := newManagerHostLookup()
	factory := &managerProcessMock{}

	manager := NewProcessManager(hostLookup, factory, nodeID, logger)

	// Test AttachLifecycle with missing PID registry
	ctx := ctxapi.NewRootContext()

	ctx = topology.WithTopology(ctx, newManagerTopology())
	lifecycle := process.Lifecycle{
		Monitor: true,
		Link:    true,
		Parent:  pubsub.PID{Node: "test-node", Host: "test-host", ID: registry.ParseID("test-id")},
	}

	ctx = manager.AttachLifecycle(ctx, lifecycle)

	// Verify that the callbacks are attached but handle missing PID registry gracefully
	onComplete := process.GetOnComplete(ctx)
	assert.NotNil(t, onComplete)

	// Test the callback with missing PID registry
	pid := pubsub.PID{Node: "test-node", Host: "test-host", ID: registry.ParseID("test-id")}
	onComplete(pid, &runtime.Result{})
}

func TestManager_AttachLifecycle_RegistrationError(t *testing.T) {
	// Setup test dependencies
	nodeID := pubsub.NodeID("test-node")
	logger := zap.NewNop()
	hostLookup := newManagerHostLookup()
	factory := &managerProcessMock{}

	manager := NewProcessManager(hostLookup, factory, nodeID, logger)

	// Create topology with registration error
	topo := newManagerTopology()
	topo.registerErr = errors.New("registration failed")

	ctx := ctxapi.NewRootContext()

	ctx = topology.WithTopology(ctx, topo)
	ctx = topology.WithRegistry(ctx, toposystem.NewPIDRegistry(toposystem.PIDRegistryConfig{}))
	lifecycle := process.Lifecycle{
		Monitor: true,
		Link:    true,
		Parent:  pubsub.PID{Node: "test-node", Host: "test-host", ID: registry.ParseID("test-id")},
	}

	ctx = manager.AttachLifecycle(ctx, lifecycle)

	// Test the callback with registration error
	onStart := process.GetOnStart(ctx)
	assert.NotNil(t, onStart)

	pid := pubsub.PID{Node: "test-node", Host: "test-host", ID: registry.ParseID("test-id")}
	onStart(pid, &mockProcess{})
}

func TestManager_AttachLifecycle_MonitoringError(t *testing.T) {
	// Setup test dependencies
	nodeID := pubsub.NodeID("test-node")
	logger := zap.NewNop()
	hostLookup := newManagerHostLookup()
	factory := &managerProcessMock{}

	manager := NewProcessManager(hostLookup, factory, nodeID, logger)

	// Create topology with monitoring error
	topo := newManagerTopology()
	topo.waitErr = errors.New("monitoring failed")

	ctx := ctxapi.NewRootContext()

	ctx = topology.WithTopology(ctx, topo)
	ctx = topology.WithRegistry(ctx, toposystem.NewPIDRegistry(toposystem.PIDRegistryConfig{}))
	lifecycle := process.Lifecycle{
		Monitor: true,
		Link:    true,
		Parent:  pubsub.PID{Node: "test-node", Host: "test-host", ID: registry.ParseID("test-id")},
	}

	ctx = manager.AttachLifecycle(ctx, lifecycle)

	// Test the callback with monitoring error
	onStart := process.GetOnStart(ctx)
	assert.NotNil(t, onStart)

	pid := pubsub.PID{Node: "test-node", Host: "test-host", ID: registry.ParseID("test-id")}
	onStart(pid, &mockProcess{})
}

func TestManager_AttachLifecycle_LinkingError(t *testing.T) {
	// Setup test dependencies
	nodeID := pubsub.NodeID("test-node")
	logger := zap.NewNop()
	hostLookup := newManagerHostLookup()
	factory := &managerProcessMock{}

	manager := NewProcessManager(hostLookup, factory, nodeID, logger)

	// Create topology with linking error
	topo := newManagerTopology()
	topo.linkErr = errors.New("linking failed")

	ctx := ctxapi.NewRootContext()

	ctx = topology.WithTopology(ctx, topo)
	ctx = topology.WithRegistry(ctx, toposystem.NewPIDRegistry(toposystem.PIDRegistryConfig{}))
	lifecycle := process.Lifecycle{
		Monitor: true,
		Link:    true,
		Parent:  pubsub.PID{Node: "test-node", Host: "test-host", ID: registry.ParseID("test-id")},
	}

	ctx = manager.AttachLifecycle(ctx, lifecycle)

	// Test the callback with linking error
	onStart := process.GetOnStart(ctx)
	assert.NotNil(t, onStart)

	pid := pubsub.PID{Node: "test-node", Host: "test-host", ID: registry.ParseID("test-id")}
	onStart(pid, &mockProcess{})
}
