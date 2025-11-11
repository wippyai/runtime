package security

import (
	"context"
	ctxapi "github.com/ponyruntime/pony/api/context"
	"testing"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	secapi "github.com/ponyruntime/pony/api/security"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/system/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// mockTokenStore implements secapi.TokenStore for testing
type mockTokenStore struct {
	tokens map[string]interface{}
}

func newMockTokenStore() *mockTokenStore {
	return &mockTokenStore{
		tokens: make(map[string]interface{}),
	}
}

func (m *mockTokenStore) Get(key string) (interface{}, error) {
	if token, exists := m.tokens[key]; exists {
		return token, nil
	}
	return nil, secapi.ErrTokenNotFound
}

func (m *mockTokenStore) Set(key string, value interface{}) error {
	m.tokens[key] = value
	return nil
}

func (m *mockTokenStore) Delete(key string) error {
	delete(m.tokens, key)
	return nil
}

// mockResource implements resource.Resource for testing
type mockResource struct {
	id    registry.ID
	store *mockTokenStore
}

func newMockResource(id registry.ID, store *mockTokenStore) *mockResource {
	return &mockResource{
		id:    id,
		store: store,
	}
}

func (m *mockResource) ID() registry.ID {
	return m.id
}

func (m *mockResource) Data() interface{} {
	return m.store
}

func (m *mockResource) Release() {
	// No-op for testing
}

func (m *mockResource) Get() (any, error) {
	return m.store, nil
}

// mockResourceRegistry implements resource.Registry for testing
type mockResourceRegistry struct {
	resources map[registry.ID]resource.Resource[any]
}

func newMockResourceRegistry() *mockResourceRegistry {
	return &mockResourceRegistry{
		resources: make(map[registry.ID]resource.Resource[any]),
	}
}

func (m *mockResourceRegistry) Acquire(_ context.Context, id registry.ID, _ resource.AccessMode) (resource.Resource[any], error) {
	if res, exists := m.resources[id]; exists {
		return res, nil
	}
	return nil, resource.ErrResourceNotFound
}

func (m *mockResourceRegistry) List() ([]registry.ID, error) {
	ids := make([]registry.ID, 0, len(m.resources))
	for id := range m.resources {
		ids = append(ids, id)
	}
	return ids, nil
}

func (m *mockResourceRegistry) Exists(id registry.ID) bool {
	_, exists := m.resources[id]
	return exists
}

func (m *mockResourceRegistry) Add(id registry.ID, res resource.Resource[any]) {
	m.resources[id] = res
}

// setupTestEnvironment creates a test environment with Security module
func setupTestEnvironment(t *testing.T) (*engine.CoroutineVM, *lua.LState, engine.UnitOfWork, *engine.Runner, *mockResourceRegistry) {
	logger := zap.NewNop()

	// Create the Security module
	module := NewSecurityModule(logger)

	// Create a VM with coroutine support
	vm, err := engine.NewCVM(logger)
	require.NoError(t, err)

	// Get the Lua state
	L := vm.State()

	// Register the Security module
	L.PreloadModule(module.Name(), module.Loader)

	// Create a runner
	runner := engine.NewRunner(vm)

	// Create a UOW
	uw, ctx := runner.InitUnitOfWork(ctxapi.NewRootContext())

	// Add security context with a test actor and scope
	actor := secapi.Actor{ID: "test-actor"}
	scope := security.NewScope(nil)
	ctx = secapi.WithActor(ctx, actor)
	ctx = secapi.WithScope(ctx, scope)

	// Add resource registry
	mockRegistry := newMockResourceRegistry()
	ctx = resource.WithRegistry(ctx, mockRegistry)

	// Set the context in the Lua state
	L.SetContext(ctx)

	// Load the security module in the Lua state
	err = L.DoString(`require("security")`)
	require.NoError(t, err)

	return vm, L, uw, runner, mockRegistry
}

func TestSecurityModule(t *testing.T) {
	t.Run("module loader registers functions", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewSecurityModule(logger)

		vm, err := engine.NewVM(logger, engine.WithLoader(module.Name(), module.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Check that the module name is correct
		assert.Equal(t, "security", module.Name())

		// Load the module and check that functions are registered
		err = vm.DoString(context.Background(), `
			local security = require("security")
			
			-- Check that core functions exist
			assert(type(security.actor) == "function", "security.actor should be a function")
			assert(type(security.scope) == "function", "security.scope should be a function")
			assert(type(security.can) == "function", "security.can should be a function")
			assert(type(security.policy) == "function", "security.policy should be a function")
			assert(type(security.named_scope) == "function", "security.named_scope should be a function")
			assert(type(security.new_scope) == "function", "security.new_scope should be a function")
			assert(type(security.new_actor) == "function", "security.new_actor should be a function")
			assert(type(security.token_store) == "function", "security.token_store should be a function")
		`, "test_loader")
		require.NoError(t, err)
	})

	t.Run("actor returns current actor", func(t *testing.T) {
		vm, L, uw, runner, _ := setupTestEnvironment(t)
		defer vm.Close()
		defer func() {
			err := uw.Close()
			assert.NoError(t, err, "Unit of work cleanup failed")
		}()

		// Import test function
		err := vm.Import(`
			function test_actor()
				local security = require("security")
				local actor = security.actor()
				return actor
			end
		`, "test", "test_actor")
		require.NoError(t, err)

		// Execute the function
		result, err := runner.Execute(L.Context(), "test_actor")
		require.NoError(t, err)

		// Check that we got a userdata (Actor object)
		assert.Equal(t, lua.LTUserData, result.Type())
	})

	t.Run("scope returns current scope", func(t *testing.T) {
		vm, L, uw, runner, _ := setupTestEnvironment(t)
		defer vm.Close()
		defer func() {
			err := uw.Close()
			assert.NoError(t, err, "Unit of work cleanup failed")
		}()

		// Import test function
		err := vm.Import(`
			function test_scope()
				local security = require("security")
				local scope = security.scope()
				return scope
			end
		`, "test", "test_scope")
		require.NoError(t, err)

		// Execute the function
		result, err := runner.Execute(L.Context(), "test_scope")
		require.NoError(t, err)

		// Check that we got a userdata (Scope object)
		assert.Equal(t, lua.LTUserData, result.Type())
	})

	t.Run("can checks permissions", func(t *testing.T) {
		vm, L, uw, runner, _ := setupTestEnvironment(t)
		defer vm.Close()
		defer func() {
			err := uw.Close()
			assert.NoError(t, err, "Unit of work cleanup failed")
		}()

		// Import test function
		err := vm.Import(`
			function test_can()
				local security = require("security")
				local allowed = security.can("read", "test-resource")
				return allowed
			end
		`, "test", "test_can")
		require.NoError(t, err)

		// Execute the function
		result, err := runner.Execute(L.Context(), "test_can")
		require.NoError(t, err)

		// Check that we got a boolean
		assert.Equal(t, lua.LTBool, result.Type())
	})

	t.Run("new_actor creates actor", func(t *testing.T) {
		vm, L, uw, runner, _ := setupTestEnvironment(t)
		defer vm.Close()
		defer func() {
			err := uw.Close()
			assert.NoError(t, err, "Unit of work cleanup failed")
		}()

		// Import test function
		err := vm.Import(`
			function test_new_actor()
				local security = require("security")
				local actor, err = security.new_actor("new-actor")
				if err then
					error(err)
				end
				return actor
			end
		`, "test", "test_new_actor")
		require.NoError(t, err)

		// Execute the function - this should fail because we can't create custom actors in test mode
		_, err = runner.Execute(L.Context(), "test_new_actor")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not allowed to create actor")
	})

	t.Run("new_scope creates scope", func(t *testing.T) {
		vm, L, uw, runner, _ := setupTestEnvironment(t)
		defer vm.Close()
		defer func() {
			err := uw.Close()
			assert.NoError(t, err, "Unit of work cleanup failed")
		}()

		// Import test function
		err := vm.Import(`
			function test_new_scope()
				local security = require("security")
				local scope, err = security.new_scope()
				if err then
					error(err)
				end
				return scope
			end
		`, "test", "test_new_scope")
		require.NoError(t, err)

		// Execute the function - this should fail because we can't create custom scopes in test mode
		_, err = runner.Execute(L.Context(), "test_new_scope")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not allowed to create custom scopes")
	})

	t.Run("token_store gets token store", func(t *testing.T) {
		vm, L, uw, runner, mockRegistry := setupTestEnvironment(t)
		defer vm.Close()
		defer func() {
			err := uw.Close()
			assert.NoError(t, err, "Unit of work cleanup failed")
		}()

		// Add a mock token store to the registry
		tokenStore := newMockTokenStore()
		resource := newMockResource(registry.ID{NS: "test", Name: "tokenstore"}, tokenStore)
		mockRegistry.Add(registry.ID{NS: "test", Name: "tokenstore"}, resource)

		// Import test function
		err := vm.Import(`
			function test_token_store()
				local security = require("security")
				local store, err = security.token_store("test:tokenstore")
				if err then
					error(err)
				end
				return store
			end
		`, "test", "test_token_store")
		require.NoError(t, err)

		// Execute the function - this should fail because we need a transcoder
		_, err = runner.Execute(L.Context(), "test_token_store")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not allowed to get token store")
	})
}

func TestSecurityModuleErrorHandling(t *testing.T) {
	t.Run("actor with no context", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewSecurityModule(logger)

		vm, err := engine.NewVM(logger, engine.WithLoader(module.Name(), module.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Test without proper context setup
		err = vm.DoString(context.Background(), `
			local security = require("security")
			local actor = security.actor()
		`, "test_no_context")
		require.NoError(t, err) // This should return nil, not error
	})

	t.Run("scope with no context", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewSecurityModule(logger)

		vm, err := engine.NewVM(logger, engine.WithLoader(module.Name(), module.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Test without proper context setup
		err = vm.DoString(context.Background(), `
			local security = require("security")
			local scope = security.scope()
		`, "test_no_context_scope")
		require.NoError(t, err) // This should return nil, not error
	})

	t.Run("can with no context", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewSecurityModule(logger)

		vm, err := engine.NewVM(logger, engine.WithLoader(module.Name(), module.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Test without proper context setup
		err = vm.DoString(context.Background(), `
			local security = require("security")
			local allowed = security.can("read", "test")
		`, "test_no_context_can")
		require.NoError(t, err) // This should return false, not error
	})

	t.Run("token_store with no resource registry", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewSecurityModule(logger)

		vm, err := engine.NewVM(logger, engine.WithLoader(module.Name(), module.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Test without proper context setup
		err = vm.DoString(context.Background(), `
			local security = require("security")
			local store, err = security.token_store("test:store")
		`, "test_no_resource_registry")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no resource registry found in context")
	})
}

func TestActorType(t *testing.T) {
	t.Run("actor id method", func(t *testing.T) {
		vm, L, uw, runner, _ := setupTestEnvironment(t)
		defer vm.Close()
		defer func() {
			err := uw.Close()
			assert.NoError(t, err, "Unit of work cleanup failed")
		}()

		// Import test function
		err := vm.Import(`
			function test_actor_id()
				local security = require("security")
				local actor = security.actor()
				local id = actor:id()
				return id
			end
		`, "test", "test_actor_id")
		require.NoError(t, err)

		// Execute the function
		result, err := runner.Execute(L.Context(), "test_actor_id")
		require.NoError(t, err)

		// Check that we got the actor ID
		assert.Equal(t, lua.LTString, result.Type())
		assert.Equal(t, "test-actor", result.String())
	})

	t.Run("actor meta method", func(t *testing.T) {
		vm, L, uw, runner, _ := setupTestEnvironment(t)
		defer vm.Close()
		defer func() {
			err := uw.Close()
			assert.NoError(t, err, "Unit of work cleanup failed")
		}()

		// Import test function
		err := vm.Import(`
			function test_actor_meta()
				local security = require("security")
				local actor = security.actor()
				if actor == nil then
					return nil
				end
				local meta = actor:meta()
				return meta
			end
		`, "test", "test_actor_meta")
		require.NoError(t, err)

		// Execute the function - this should fail because no transcoder is set up
		_, err = runner.Execute(L.Context(), "test_actor_meta")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no transcoder registered")
	})
}
