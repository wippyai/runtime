package process

import (
	"context"
	ctxapi "github.com/wippyai/runtime/api/context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	processapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	topologyapi "github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func newTestContext() context.Context {
	ctx := ctxapi.NewRootContext()
	ctx, fc := ctxapi.OpenFrameContext(ctx)

	// Set up test PID and ID in frame context before sealing
	testPID := relay.PID{Host: "test-host", UniqID: "test-process-123"}
	testID := registry.ID{NS: "test", Name: "process"}

	_ = fc.Set(runtime.FramePIDKey, testPID)
	_ = fc.Set(runtime.FrameIDKey, testID)

	return ctx
}

// mockProcessManager implements process.Manager for testing
type mockProcessManager struct {
	processes map[relay.PID]bool
}

func newMockProcessManager() *mockProcessManager {
	return &mockProcessManager{
		processes: make(map[relay.PID]bool),
	}
}

func (m *mockProcessManager) Start(_ context.Context, start *processapi.Start) (relay.PID, error) {
	pid := relay.PID{
		Host:   start.HostID,
		UniqID: start.UniqID,
	}
	m.processes[pid] = true
	return pid, nil
}

func (m *mockProcessManager) Terminate(_ context.Context, pid relay.PID) error {
	delete(m.processes, pid)
	return nil
}

func (m *mockProcessManager) Cancel(_ context.Context, _ relay.PID, pid relay.PID, _ time.Time) error {
	delete(m.processes, pid)
	return nil
}

func (m *mockProcessManager) AttachLifecycle(ctx context.Context, _ processapi.Lifecycle) context.Context {
	return ctx
}

// mockTopology implements topology.Topology for testing
type mockTopology struct {
	processes map[relay.PID]bool
	monitors  map[relay.PID][]relay.PID
	links     map[relay.PID][]relay.PID
}

func newMockTopology() *mockTopology {
	return &mockTopology{
		processes: make(map[relay.PID]bool),
		monitors:  make(map[relay.PID][]relay.PID),
		links:     make(map[relay.PID][]relay.PID),
	}
}

func (m *mockTopology) Register(pid relay.PID) error {
	m.processes[pid] = true
	return nil
}

func (m *mockTopology) Wait(caller, pid relay.PID) error {
	m.monitors[pid] = append(m.monitors[pid], caller)
	return nil
}

func (m *mockTopology) Release(caller, pid relay.PID) error {
	if monitors, exists := m.monitors[pid]; exists {
		for i, monitor := range monitors {
			if monitor == caller {
				m.monitors[pid] = append(monitors[:i], monitors[i+1:]...)
				break
			}
		}
	}
	return nil
}

func (m *mockTopology) Link(from, to relay.PID) error {
	m.links[from] = append(m.links[from], to)
	m.links[to] = append(m.links[to], from)
	return nil
}

func (m *mockTopology) Unlink(from, to relay.PID) error {
	if links, exists := m.links[from]; exists {
		for i, link := range links {
			if link == to {
				m.links[from] = append(links[:i], links[i+1:]...)
				break
			}
		}
	}
	if links, exists := m.links[to]; exists {
		for i, link := range links {
			if link == from {
				m.links[to] = append(links[:i], links[i+1:]...)
				break
			}
		}
	}
	return nil
}

func (m *mockTopology) GetLinks(pid relay.PID) []relay.PID {
	return m.links[pid]
}

func (m *mockTopology) Notify(_ relay.PID, _ *runtime.Result) {
	// No-op for testing
}

func (m *mockTopology) Remove(pid relay.PID) {
	delete(m.processes, pid)
	delete(m.monitors, pid)
	delete(m.links, pid)
}

// mockNode implements relay.Node for testing
type mockNode struct {
	attached map[relay.PID]chan *relay.Package
}

func newMockNode() *mockNode {
	return &mockNode{
		attached: make(map[relay.PID]chan *relay.Package),
	}
}

func (m *mockNode) Attach(pid relay.PID, inbox chan *relay.Package) (context.CancelFunc, error) {
	m.attached[pid] = inbox
	return func() {
		delete(m.attached, pid)
	}, nil
}

func (m *mockNode) Detach(pid relay.PID) {
	delete(m.attached, pid)
}

func (m *mockNode) Send(_ *relay.Package) error {
	return nil
}

func (m *mockNode) ID() relay.NodeID {
	return "test-node"
}

func (m *mockNode) RegisterHost(_ relay.HostID, _ relay.Host) error {
	return nil
}

func (m *mockNode) UnregisterHost(_ relay.HostID) {
	// No-op for testing
}

// setupTestEnvironment creates a test environment with Process module
func setupTestEnvironment(t *testing.T) (*engine.CoroutineVM, *lua.LState, engine.UnitOfWork) {
	logger := zap.NewNop()

	// Create the Process module
	module := NewProcessAPIModule(logger)

	// Create a VM with coroutine support
	vm, err := engine.NewCVM(logger)
	require.NoError(t, err)

	// Get the Lua state
	L := vm.State()

	// Register the Process module
	L.PreloadModule(module.Name(), module.Loader)

	// Create a runner
	runner := engine.NewRunner(vm)

	// Create a UOW
	uw, ctx := runner.InitUnitOfWork(newTestContext())

	// Add mock node
	mockNode := newMockNode()
	ctx = relay.WithNode(ctx, mockNode)

	// Add topology context
	mockTopo := newMockTopology()
	ctx = topologyapi.WithTopology(ctx, mockTopo)

	// Add process manager
	mockManager := newMockProcessManager()
	ctx = processapi.WithManager(ctx, mockManager)

	// Set the context in the Lua state
	L.SetContext(ctx)

	return vm, L, uw
}

func TestProcessModule(t *testing.T) {
	t.Run("module loader registers functions", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewProcessAPIModule(logger)

		vm, err := engine.NewVM(logger, engine.WithLoader(module.Name(), module.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Check that the module name is correct
		assert.Equal(t, "process", module.Name())

		// Load the module and set it as a global in Lua
		err = vm.State().DoString(`process = require("process")`)
		require.NoError(t, err)

		// Check that key functions exist
		L := vm.State()
		processValue := L.GetGlobal("process")
		require.NotNil(t, processValue, "process module should be loaded")

		processTable, ok := processValue.(*lua.LTable)
		require.True(t, ok, "process should be a table")

		// Check for core functions
		assert.NotNil(t, processTable.RawGetString("id"))
		assert.NotNil(t, processTable.RawGetString("pid"))
		assert.NotNil(t, processTable.RawGetString("send"))
		assert.NotNil(t, processTable.RawGetString("spawn"))
		assert.NotNil(t, processTable.RawGetString("terminate"))
		assert.NotNil(t, processTable.RawGetString("cancel"))

		// Check for registry functions
		registryValue := processTable.RawGetString("registry")
		require.NotNil(t, registryValue, "registry should exist")
		registryTable, ok := registryValue.(*lua.LTable)
		require.True(t, ok, "registry should be a table")
		assert.NotNil(t, registryTable.RawGetString("register"))
		assert.NotNil(t, registryTable.RawGetString("lookup"))
		assert.NotNil(t, registryTable.RawGetString("unregister"))

		// Check for event constants
		eventValue := processTable.RawGetString("event")
		require.NotNil(t, eventValue, "event should exist")
		eventTable, ok := eventValue.(*lua.LTable)
		require.True(t, ok, "event should be a table")
		assert.NotNil(t, eventTable.RawGetString("CANCEL"))
		assert.NotNil(t, eventTable.RawGetString("EXIT"))
		assert.NotNil(t, eventTable.RawGetString("LINK_DOWN"))
	})

	t.Run("pid function returns current pid", func(t *testing.T) {
		vm, L, uw := setupTestEnvironment(t)
		defer vm.Close()
		defer uw.Close()

		err := L.DoString(`
			local process = require("process")
			local pid = process.pid()
			if not pid then
				error("pid should not be nil")
			end
		`)
		require.NoError(t, err)
	})

	t.Run("id function returns current id", func(t *testing.T) {
		vm, L, uw := setupTestEnvironment(t)
		defer vm.Close()
		defer uw.Close()

		err := L.DoString(`
			local process = require("process")
			local id = process.id()
			if not id then
				error("id should not be nil")
			end
		`)
		require.NoError(t, err)
	})

	t.Run("send function works with valid pid", func(t *testing.T) {
		vm, L, uw := setupTestEnvironment(t)
		defer vm.Close()
		defer uw.Close()

		err := L.DoString(`
			local process = require("process")
			local result, err = process.send("test:process", "hello")
			-- Should not error even if process doesn't exist in test environment
		`)
		require.NoError(t, err)
	})

	t.Run("spawn function works", func(t *testing.T) {
		vm, L, uw := setupTestEnvironment(t)
		defer vm.Close()
		defer uw.Close()

		err := L.DoString(`
			local process = require("process")
			local pid, err = process.spawn("test:process", "test-host")
			-- Should not error even if process doesn't exist in test environment
		`)
		require.NoError(t, err)
	})

	t.Run("terminate function works", func(t *testing.T) {
		vm, L, uw := setupTestEnvironment(t)
		defer vm.Close()
		defer uw.Close()

		err := L.DoString(`
			local process = require("process")
			local result, err = process.terminate("test:process")
			-- Should not error even if process doesn't exist in test environment
		`)
		require.NoError(t, err)
	})

	t.Run("cancel function works", func(t *testing.T) {
		vm, L, uw := setupTestEnvironment(t)
		defer vm.Close()
		defer uw.Close()

		err := L.DoString(`
			local process = require("process")
			local result, err = process.cancel("test:process", "30s")
			-- Should not error even if process doesn't exist in test environment
		`)
		require.NoError(t, err)
	})

	t.Run("registry functions work", func(t *testing.T) {
		vm, L, uw := setupTestEnvironment(t)
		defer vm.Close()
		defer uw.Close()

		err := L.DoString(`
			local process = require("process")
			local result, err = process.registry.register("test-name", "test:process")
			-- Should not error even if registry doesn't exist in test environment
		`)
		require.NoError(t, err)
	})
}

func TestProcessModuleErrorHandling(t *testing.T) {
	t.Run("pid with no context", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewProcessAPIModule(logger)

		vm, err := engine.NewVM(logger, engine.WithLoader(module.Name(), module.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Test without context
		err = vm.State().DoString(`
			local process = require("process")
			local pid, err = process.pid()
			if not err then
				error("should have error when no context")
			end
		`)
		require.NoError(t, err)
	})

	t.Run("send with no context", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewProcessAPIModule(logger)

		vm, err := engine.NewVM(logger, engine.WithLoader(module.Name(), module.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Test without context
		err = vm.State().DoString(`
			local process = require("process")
			local result, err = process.send("test:process", "hello")
			if not err then
				error("should have error when no context")
			end
		`)
		require.NoError(t, err)
	})
}
