package env

import (
	"context"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	envapi "github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/coroutine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap/zaptest"
)

// mockResourceRegistry is a simple mock for the resource registry
type mockResourceRegistry struct {
	vars      map[string]string // map[key_name]value
	resources map[registry.ID]resource.Resource[any]
}

func (m *mockResourceRegistry) Get(_ context.Context, name string) (string, error) {
	value, ok := m.vars[name]
	if !ok {
		return "", envapi.ErrVariableNotFound
	}
	return value, nil
}

func (m *mockResourceRegistry) GetFromStorage(_ context.Context, name string) (string, error) {
	value, ok := m.vars[name]
	if !ok {
		return "", envapi.ErrVariableNotFound
	}
	return value, nil
}

func (m *mockResourceRegistry) Set(_ context.Context, name string, value string) error {
	m.vars[name] = value
	return nil
}

func (m *mockResourceRegistry) All(_ context.Context) (map[string]string, error) {
	// For testing purposes, we return the variables map
	return m.vars, nil
}

func (m *mockResourceRegistry) Acquire(
	_ context.Context,
	id registry.ID,
	_ resource.AccessMode,
) (resource.Resource[any], error) {
	res, ok := m.resources[id]
	if !ok {
		return nil, resource.ErrResourceNotFound
	}
	return res, nil
}

func (m *mockResourceRegistry) List() ([]registry.ID, error) {
	ids := make([]registry.ID, 0, len(m.resources))
	for id := range m.resources {
		ids = append(ids, id)
	}
	return ids, nil
}

func (m *mockResourceRegistry) Exists(id registry.ID) bool {
	_, ok := m.resources[id]
	return ok
}

// testScope implements security.Scope for testing
type testScope struct {
	allowed map[string]bool
}

func newTestScope() *testScope {
	return &testScope{
		allowed: make(map[string]bool),
	}
}

func (s *testScope) With(_ security.Policy) security.Scope {
	return s
}

func (s *testScope) Without(_ registry.ID) security.Scope {
	return s
}

func (s *testScope) Evaluate(_ security.Actor, action, resource string, _ registry.Metadata) security.Result {
	key := action + ":" + resource
	if s.allowed[key] {
		return security.Allow
	}
	return security.Deny
}

func (s *testScope) Contains(_ registry.ID) bool {
	return false
}

func (s *testScope) Policies() []security.Policy {
	return nil
}

func (s *testScope) Allow(action, resource string) {
	s.allowed[action+":"+resource] = true
}

// setupTestEnvironment creates a test environment with Env module and mock storage
func setupTestEnvironment(t *testing.T) (*engine.CoroutineVM, *engine.Runner, context.Context, *mockResourceRegistry) {
	logger := zaptest.NewLogger(t)

	// Create the Env module
	module := NewEnvModule()

	// Create a mock resource registry with our test storage
	mockRegistry := &mockResourceRegistry{
		vars:      make(map[string]string),
		resources: make(map[registry.ID]resource.Resource[any]),
	}

	// Create a VM with coroutine support
	vm, err := engine.NewCVM(logger)
	require.NoError(t, err)

	// Get the Lua state
	L := vm.State()

	// Register the Env module
	L.PreloadModule(module.Name(), module.Loader)

	// Create a runner with the coroutine layer
	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))

	// Create context properly: RootContext -> OpenFrameContext -> add resources
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	// Add the resource registry to the context
	ctx = envapi.WithRegistry(ctx, mockRegistry)

	// Add security context
	actor := security.Actor{ID: "test"}
	scope := newTestScope()
	_ = security.SetActor(ctx, actor)
	_ = security.SetScope(ctx, scope)

	return vm, runner, ctx, mockRegistry
}

func TestEnvModule(t *testing.T) {
	t.Run("get environment variable", func(t *testing.T) {
		vm, runner, ctx, registry := setupTestEnvironment(t)
		defer vm.Close()

		// Allow access to the variable
		scope, _ := security.GetScope(ctx)
		scope.(*testScope).Allow("env.get", "test_var")

		err := registry.Set(context.Background(), "test_var", "test_value")
		require.NoError(t, err, "failed to set new value")

		// Import our test function
		err = vm.Import(`
			function test_env_get()
				local env = require("env")
				local value, err = env.get("test_var")
				if err then
					error(err)
				end
				return value
			end
		`, "test", "test_env_get")
		require.NoError(t, err, "Failed to import test function")

		// Execute the function
		result, err := runner.Execute(ctx, "test_env_get")
		require.NoError(t, err, "Lua execution failed")

		assert.Equal(t, lua.LString("test_value"), result, "Expected 'test_value'")
	})

	t.Run("get non-existent variable", func(t *testing.T) {
		vm, runner, ctx, registry := setupTestEnvironment(t)
		defer vm.Close()

		_ = registry

		// Allow access to the variable
		scope, _ := security.GetScope(ctx)
		scope.(*testScope).Allow("env.get", "NON_EXISTENT")

		// Import our test function
		err := vm.Import(`
			function test_env_get_nonexistent()
				local env = require("env")
				local value, err = env.get("NON_EXISTENT")
				return {value = value, err = err}
			end
		`, "test", "test_env_get_nonexistent")
		require.NoError(t, err, "Failed to import test function")

		// Execute the function
		result, err := runner.Execute(ctx, "test_env_get_nonexistent")
		require.NoError(t, err, "Lua execution failed")

		resultTable := result.(*lua.LTable)
		value := resultTable.RawGetString("value")
		errVal := resultTable.RawGetString("environment variable not found")

		assert.Equal(t, lua.LNil, value, "Expected nil value")
		assert.Equal(t, lua.LNil, errVal, "Expected nil error")
	})

	t.Run("get with empty key", func(t *testing.T) {
		vm, runner, ctx, registry := setupTestEnvironment(t)
		defer vm.Close()
		_ = registry

		// Import our test function
		err := vm.Import(`
			function test_env_get_empty_key()
				local env = require("env")
				local ok, err = pcall(function()
					env.get("")
				end)
				return {success = ok, error = not ok}
			end
		`, "test", "test_env_get_empty_key")
		require.NoError(t, err, "Failed to import test function")

		// Execute the function
		result, err := runner.Execute(ctx, "test_env_get_empty_key")
		require.NoError(t, err, "Lua execution failed")

		resultTable := result.(*lua.LTable)
		success := resultTable.RawGetString("success")
		hasError := resultTable.RawGetString("error")

		assert.Equal(t, lua.LBool(false), success, "Function should fail")
		assert.Equal(t, lua.LBool(true), hasError, "Error should be returned")
	})

	t.Run("get with no context", func(t *testing.T) {
		vm, runner, ctx, registry := setupTestEnvironment(t)
		defer vm.Close()
		_ = registry

		// Clear the context
		// Context already prepared in setup

		// Import our test function
		err := vm.Import(`
			function test_env_get_no_context()
				local env = require("env")
				local ok, err = pcall(function()
					env.get("TEST_VAR")
				end)
				return {success = ok, error = not ok}
			end
		`, "test", "test_env_get_no_context")
		require.NoError(t, err, "Failed to import test function")

		// Execute the function
		result, err := runner.Execute(ctx, "test_env_get_no_context")
		require.NoError(t, err, "Lua execution failed")

		resultTable := result.(*lua.LTable)
		success := resultTable.RawGetString("success")
		hasError := resultTable.RawGetString("error")

		assert.Equal(t, lua.LBool(false), success, "Function should fail")
		assert.Equal(t, lua.LBool(true), hasError, "Error should be returned")
	})

	t.Run("set and get environment variable", func(t *testing.T) {
		vm, runner, ctx, registry := setupTestEnvironment(t)
		defer vm.Close()

		// Allow access to the variable
		scope, _ := security.GetScope(ctx)
		scope.(*testScope).Allow("env.set", "test_var")
		scope.(*testScope).Allow("env.get", "test_var")

		// Set default value in registry
		err := registry.Set(context.Background(), "test_var", "default_value")
		require.NoError(t, err, "Failed to set default value in registry")

		// Import our test function
		err = vm.Import(`
			function test_env_set_get()
				local env = require("env")
				local success, err = env.set("test_var", "new_value")
				if not success then
					return {success = false, error = err}
				end
				local value, err = env.get("test_var")
				if err then
					return {success = false, error = err}
				end
				return {success = true, value = value}
			end
		`, "test", "test_env_set_get")
		require.NoError(t, err, "Failed to import test function")

		// Execute the function
		result, err := runner.Execute(ctx, "test_env_set_get")
		require.NoError(t, err, "Lua execution failed")

		resultTable := result.(*lua.LTable)
		success := resultTable.RawGetString("success")
		value := resultTable.RawGetString("value")

		assert.Equal(t, lua.LBool(true), success, "Operation should succeed")
		assert.Equal(t, lua.LString("new_value"), value, "Expected 'new_value'")

		// Verify the value was actually set in the registry
		actualValue, err := registry.Get(context.Background(), "test_var")
		require.NoError(t, err, "Failed to get value from registry")
		assert.Equal(t, "new_value", actualValue, "Registry value should match")
	})
}
