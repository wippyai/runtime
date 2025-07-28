package contract

import (
	"context"
	"errors"
	"testing"

	"github.com/ponyruntime/pony/api/contract"
	"github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	secapi "github.com/ponyruntime/pony/api/security"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	transcoder "github.com/ponyruntime/pony/system/payload"
	"github.com/ponyruntime/pony/system/payload/json"
	luapayload "github.com/ponyruntime/pony/system/payload/lua"
	"github.com/ponyruntime/pony/system/payload/yaml"
	systemsec "github.com/ponyruntime/pony/system/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

var (
	// ErrNotFound represents a not found error for testing
	ErrNotFound = errors.New("not found")
)

// TestPolicy allows all operations for testing
type TestPolicy struct {
	id registry.ID
}

func (p *TestPolicy) ID() registry.ID {
	return p.id
}

func (p *TestPolicy) Evaluate(_ secapi.Actor, _, _ string, _ registry.Metadata) secapi.Result {
	// Allow everything for testing
	return secapi.Allow
}

// Mock implementations for testing

// mockContract implements contract.Contract interface
type mockContract struct {
	id      registry.ID
	meta    registry.Metadata
	methods []contract.MethodDef
}

func (m *mockContract) ID() registry.ID {
	return m.id
}

func (m *mockContract) Meta() registry.Metadata {
	return m.meta
}

func (m *mockContract) Methods() []contract.MethodDef {
	return m.methods
}

func (m *mockContract) Method(name string) (*contract.MethodDef, error) {
	for i := range m.methods {
		if m.methods[i].Name == name {
			return &m.methods[i], nil
		}
	}
	return nil, ErrNotFound
}

// mockInstance implements contract.Instance interface
type mockInstance struct {
	id         registry.ID
	implements []contract.Contract
	scope      registry.Metadata
}

func (m *mockInstance) ID() registry.ID {
	return m.id
}

func (m *mockInstance) Implements() []contract.Contract {
	return m.implements
}

func (m *mockInstance) Args() registry.Metadata {
	return m.scope
}

func (m *mockInstance) Call(_ context.Context, method string, args payload.Payloads) (chan *runtime.Result, error) {
	resultChan := make(chan *runtime.Result, 1)
	go func() {
		defer close(resultChan)
		// Simple mock: return method name + first arg if available
		resultValue := method
		if len(args) > 0 {
			resultValue = method + "_called"
		}
		resultChan <- &runtime.Result{
			Value: payload.New(resultValue),
		}
	}()
	return resultChan, nil
}

// mockRegistry implements contract.Registry interface
type mockRegistry struct {
	contracts           map[registry.ID]contract.Contract
	bindings            map[registry.ID]*contract.Binding
	bindingsForContract map[registry.ID][]registry.ID
	defaultBindings     map[registry.ID]registry.ID
}

func (m *mockRegistry) GetContract(_ context.Context, id registry.ID) (contract.Contract, error) {
	if c, ok := m.contracts[id]; ok {
		return c, nil
	}
	return nil, ErrNotFound
}

func (m *mockRegistry) GetBinding(_ context.Context, id registry.ID) (*contract.Binding, error) {
	if b, ok := m.bindings[id]; ok {
		return b, nil
	}
	return nil, ErrNotFound
}

func (m *mockRegistry) GetBindingsForContract(_ context.Context, contractID registry.ID) ([]registry.ID, error) {
	if bindings, ok := m.bindingsForContract[contractID]; ok {
		return bindings, nil
	}
	return []registry.ID{}, nil
}

func (m *mockRegistry) GetDefaultBinding(_ context.Context, contractID registry.ID) (registry.ID, error) {
	if binding, ok := m.defaultBindings[contractID]; ok {
		return binding, nil
	}
	return registry.ID{}, ErrNotFound
}

// mockInstantiator implements contract.Instantiator interface
type mockInstantiator struct {
	instances map[registry.ID]contract.Instance
}

func (m *mockInstantiator) Instantiate(_ context.Context, bindingID registry.ID, _ registry.Metadata) (contract.Instance, error) {
	if instance, ok := m.instances[bindingID]; ok {
		return instance, nil
	}
	return nil, ErrNotFound
}

// Test helper functions

func createTestTranscoder() payload.Transcoder {
	tr := transcoder.NewTranscoder()
	json.Register(tr)
	yaml.Register(tr)
	luapayload.Register(tr)
	return tr
}

// Setup security module mock for testing
func securityModuleLoader(l *lua.LState) int {
	// Register actor metatable
	const ActorMetatable = "security.Actor"
	value.RegisterMethods(l, ActorMetatable, map[string]lua.LGFunction{
		"id": func(l *lua.LState) int {
			ud := l.CheckUserData(1)
			actor, ok := ud.Value.(secapi.Actor)
			if !ok {
				l.ArgError(1, "Actor expected")
				return 0
			}
			l.Push(lua.LString(actor.ID))
			return 1
		},
	})

	// Register scope metatable
	const ScopeMetatable = "security.Args"
	value.RegisterMethods(l, ScopeMetatable, map[string]lua.LGFunction{
		"policies": func(l *lua.LState) int {
			ud := l.CheckUserData(1)
			scope, ok := ud.Value.(secapi.Scope)
			if !ok {
				l.ArgError(1, "Args expected")
				return 0
			}
			policies := scope.Policies()
			policyTable := l.CreateTable(len(policies), 0)
			l.Push(policyTable)
			return 1
		},
	})

	// Create module table
	mod := l.CreateTable(0, 2)

	// Add new_actor function
	mod.RawSetString("new_actor", l.NewFunction(func(l *lua.LState) int {
		// Create mock actor
		id := l.CheckString(1)
		actor := secapi.Actor{
			ID:   id,
			Meta: registry.Metadata{},
		}

		// Create userdata for actor with metatable
		ud := l.NewUserData()
		ud.Value = actor
		ud.Metatable = value.GetTypeMetatable(l, ActorMetatable)
		l.Push(ud)
		return 1
	}))

	// Add new_scope function
	mod.RawSetString("new_scope", l.NewFunction(func(l *lua.LState) int {
		// Create mock scope with empty policies
		scope := systemsec.NewScope(nil)

		// Create userdata for scope with metatable
		ud := l.NewUserData()
		ud.Value = scope
		ud.Metatable = value.GetTypeMetatable(l, ScopeMetatable)
		l.Push(ud)
		return 1
	}))

	l.Push(mod)
	return 1
}

// setupContractTest sets up a Lua VM with contract module and mock dependencies
func setupContractTest(t *testing.T) (*engine.CoroutineVM, engine.UnitOfWork, context.Context, *mockRegistry, *mockInstantiator) {
	logger := zap.NewNop()

	// Create test contract
	testContract := &mockContract{
		id:   registry.ParseID("test:contract"),
		meta: registry.Metadata{"version": "1.0"},
		methods: []contract.MethodDef{
			{
				Name:        "test_method",
				Description: "A test method",
				InputSchemas: []contract.SchemaDefinition{
					{Format: "application/json", Definition: map[string]any{"type": "string"}},
				},
				OutputSchemas: []contract.SchemaDefinition{
					{Format: "application/json", Definition: map[string]any{"type": "string"}},
				},
			},
		},
	}

	// Create test instance
	testInstance := &mockInstance{
		id:         registry.ParseID("test:binding"),
		implements: []contract.Contract{testContract},
		scope:      registry.Metadata{"initialized": "true"},
	}

	// Create mock registry
	mockReg := &mockRegistry{
		contracts: map[registry.ID]contract.Contract{
			registry.ParseID("test:contract"): testContract,
		},
		bindings: map[registry.ID]*contract.Binding{
			registry.ParseID("test:binding"): {
				ID:   registry.ParseID("test:binding"),
				Meta: registry.Metadata{"version": "1.0"},
				Contracts: []contract.BoundContract{
					{
						Contract: registry.ParseID("test:contract"),
						Methods: map[string]registry.ID{
							"test_method": registry.ParseID("test:implementation"),
						},
						ContextRequired: []string{},
					},
				},
			},
		},
		bindingsForContract: map[registry.ID][]registry.ID{
			registry.ParseID("test:contract"): {registry.ParseID("test:binding")},
		},
		defaultBindings: map[registry.ID]registry.ID{},
	}

	// Create mock instantiator
	mockInst := &mockInstantiator{
		instances: map[registry.ID]contract.Instance{
			registry.ParseID("test:binding"): testInstance,
		},
	}

	// Create contract module
	mod := NewContractModule(logger)

	// Create VM with coroutine support
	vm, err := engine.NewCVM(logger)
	require.NoError(t, err)

	// Get the Lua state and register modules
	L := vm.State()
	L.PreloadModule(mod.Name(), mod.Loader)
	L.PreloadModule("security", securityModuleLoader)

	// Setup coroutine support
	wrapped := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))
	uw, ctx := wrapped.InitUnitOfWork(context.Background())

	// Inject mock dependencies
	ctx = contract.WithServices(ctx, mockReg, mockInst)

	// Add payload transcoder
	ctx = payload.WithTranscoder(ctx, createTestTranscoder())

	// Add logger to context (required for security)
	ctx = logs.WithLogger(ctx, logger)

	// For testing, use incomplete security context which will default to allow
	// since STRICT = false in runtime/lua/security/access.go
	// This is simpler than setting up complex policy evaluation

	// Optionally add just actor (incomplete context triggers allow-by-default)
	testActor := secapi.Actor{
		ID:   "test_actor",
		Meta: registry.Metadata{},
	}
	ctx = secapi.WithActor(ctx, testActor)
	// NOTE: Not setting scope, so security context is incomplete -> defaults to allow

	// Set context in VM
	L.SetContext(ctx)

	return vm, uw, ctx, mockReg, mockInst
}

// TestContractModuleBasicLoading tests basic module loading and contract retrieval
func TestContractModuleBasicLoading(t *testing.T) {
	vm, uw, ctx, _, _ := setupContractTest(t)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Import test function that loads module and gets a contract
	err := vm.Import(`
		function test_basic_contract_loading()
			local contract = require("contract")
			
			-- Verify module structure
			assert(contract ~= nil, "contract module should not be nil")
			assert(contract.get ~= nil, "contract.get should exist")
			assert(contract.find_implementations ~= nil, "contract.find_implementations should exist")
			assert(contract.is ~= nil, "contract.is should exist")
			
			-- Get a test contract
			local c, err = contract.get("test:contract")
			if err then 
				error("Error getting contract: " .. err)
			end
			assert(c ~= nil, "contract should not be nil")
			
			-- Check contract ID
			local id = c:id()
			assert(id == "test:contract", "contract ID should match")
			
			-- Check contract methods
			local methods = c:methods()
			assert(methods ~= nil, "methods should not be nil")
			assert(#methods == 1, "should have 1 method")
			assert(methods[1].name == "test_method", "method name should match")
			
			return {
				contract_id = id,
				method_count = #methods,
				method_name = methods[1].name
			}
		end
	`, "test", "test_basic_contract_loading")
	require.NoError(t, err, "Failed to import test function")

	// Execute the test function
	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))
	result, err := runner.Execute(ctx, "test_basic_contract_loading")
	require.NoError(t, err, "Lua execution failed")

	// Verify the results
	resultTable, ok := result.(*lua.LTable)
	require.True(t, ok, "Result should be a Lua table")

	contractID := resultTable.RawGetString("contract_id").(lua.LString)
	methodCount := resultTable.RawGetString("method_count").(lua.LNumber)
	methodName := resultTable.RawGetString("method_name").(lua.LString)

	assert.Equal(t, "test:contract", string(contractID), "Contract ID should match")
	assert.Equal(t, 1, int(methodCount), "Should have 1 method")
	assert.Equal(t, "test_method", string(methodName), "Method name should match")
}

// TestContractContextChaining tests the with_context functionality
func TestContractContextChaining(t *testing.T) {
	vm, uw, ctx, _, _ := setupContractTest(t)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	err := vm.Import(`
		function test_with_context()
			local contract = require("contract")
			
			-- Get a contract
			local c, err = contract.get("test:contract")
			if err then error("Error getting contract: " .. err) end
			
			-- Chain context
			local c_with_ctx = c:with_context({key = "value", test = 123})
			
			-- Verify it returns a new contract object
			assert(c_with_ctx ~= nil, "with_context should return contract")
			assert(c_with_ctx:id() == "test:contract", "chained contract should have same ID")
			
			return "success"
		end
	`, "test", "test_with_context")
	require.NoError(t, err, "Failed to import test function")

	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))
	result, err := runner.Execute(ctx, "test_with_context")
	require.NoError(t, err, "Lua execution failed")
	assert.Equal(t, "success", result.String())
}

// TestContractSecurityChaining tests with_actor and with_scope functionality
func TestContractSecurityChaining(t *testing.T) {
	vm, uw, ctx, _, _ := setupContractTest(t)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	err := vm.Import(`
		function test_security_chaining()
			local contract = require("contract")
			local security = require("security")
			
			-- Get a contract
			local c, err = contract.get("test:contract")
			if err then error("Error getting contract: " .. err) end
			
			-- Create actor and scope
			local actor = security.new_actor("test_user")
			local scope = security.new_scope()
			
			-- Chain security context
			local c_with_actor = c:with_actor(actor)
			local c_with_scope = c_with_actor:with_scope(scope)
			
			-- Verify chaining works
			assert(c_with_actor ~= nil, "with_actor should return contract")
			assert(c_with_scope ~= nil, "with_scope should return contract")
			assert(c_with_scope:id() == "test:contract", "chained contract should have same ID")
			
			return "success"
		end
	`, "test", "test_security_chaining")
	require.NoError(t, err, "Failed to import test function")

	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))
	result, err := runner.Execute(ctx, "test_security_chaining")
	require.NoError(t, err, "Lua execution failed")
	assert.Equal(t, "success", result.String())
}

// TestContractOpening tests opening contracts to create instances
func TestContractOpening(t *testing.T) {
	vm, uw, ctx, _, _ := setupContractTest(t)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	err := vm.Import(`
		function test_contract_opening()
			local contract = require("contract")
			
			-- Get a contract
			local c, err = contract.get("test:contract")
			if err then error("Error getting contract: " .. err) end
			
			-- Open the contract to create an instance
			local instance, err = c:open("test:binding")
			if err then error("Error opening contract: " .. err) end
			
			-- Verify instance exists
			assert(instance ~= nil, "instance should not be nil")
			
			-- Check if instance implements our contract using the new API
			local implements_contract = contract.is(instance, "test:contract")
			assert(implements_contract == true, "instance should implement test:contract")
			
			-- Check if instance doesn't implement non-existent contract
			local implements_other = contract.is(instance, "other:contract")
			assert(implements_other == false, "instance should not implement other:contract")
			
			return "success"
		end
	`, "test", "test_contract_opening")
	require.NoError(t, err, "Failed to import test function")

	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))
	result, err := runner.Execute(ctx, "test_contract_opening")
	require.NoError(t, err, "Lua execution failed")
	assert.Equal(t, "success", result.String())
}

// TestContractOpenWithDefaultBinding tests opening contracts using default bindings
func TestContractOpenWithDefaultBinding(t *testing.T) {
	vm, uw, ctx, mockReg, mockInst := setupContractTest(t)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Add a default binding to the mock registry
	defaultBindingID := registry.ParseID("test:default_binding")
	defaultBinding := &contract.Binding{
		ID:   defaultBindingID,
		Meta: registry.Metadata{"version": "1.0", "default": "true"},
		Contracts: []contract.BoundContract{
			{
				Contract: registry.ParseID("test:contract"),
				Methods: map[string]registry.ID{
					"test_method": registry.ParseID("test:implementation"),
				},
				ContextRequired: []string{}, // No context required
				Default:         true,       // Mark as default
			},
		},
	}

	// Add to mocks
	mockReg.bindings[defaultBindingID] = defaultBinding
	mockReg.defaultBindings[registry.ParseID("test:contract")] = defaultBindingID

	// Create instance for default binding
	defaultInstance := &mockInstance{
		id:         defaultBindingID,
		implements: []contract.Contract{mockReg.contracts[registry.ParseID("test:contract")]},
		scope:      registry.Metadata{"from_default": "true"},
	}
	mockInst.instances[defaultBindingID] = defaultInstance

	err := vm.Import(`
		function test_default_binding()
			local contract = require("contract")
			
			-- Get a contract
			local c, err = contract.get("test:contract")
			if err then error("Error getting contract: " .. err) end
			
			-- Open using default binding (no arguments)
			local instance, err = c:open()
			if err then error("Error opening default binding: " .. err) end
			
			-- Verify instance exists
			assert(instance ~= nil, "instance should not be nil")
			
			-- Check if instance implements our contract
			local implements_contract = contract.is(instance, "test:contract")
			assert(implements_contract == true, "instance should implement test:contract")
			
			return "success"
		end
	`, "test", "test_default_binding")
	require.NoError(t, err, "Failed to import test function")

	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))
	result, err := runner.Execute(ctx, "test_default_binding")
	require.NoError(t, err, "Lua execution failed")
	assert.Equal(t, "success", result.String())
}

// TestContractOpenWithDefaultBindingAndContext tests default binding with context
func TestContractOpenWithDefaultBindingAndContext(t *testing.T) {
	vm, uw, ctx, mockReg, mockInst := setupContractTest(t)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Add a default binding that requires context
	defaultBindingID := registry.ParseID("test:context_default")
	defaultBinding := &contract.Binding{
		ID:   defaultBindingID,
		Meta: registry.Metadata{"version": "1.0"},
		Contracts: []contract.BoundContract{
			{
				Contract: registry.ParseID("test:contract"),
				Methods: map[string]registry.ID{
					"test_method": registry.ParseID("test:implementation"),
				},
				ContextRequired: []string{"database", "timeout"}, // Requires context
				Default:         true,
			},
		},
	}

	// Add to mocks
	mockReg.bindings[defaultBindingID] = defaultBinding
	mockReg.defaultBindings[registry.ParseID("test:contract")] = defaultBindingID

	// Create instance for default binding
	defaultInstance := &mockInstance{
		id:         defaultBindingID,
		implements: []contract.Contract{mockReg.contracts[registry.ParseID("test:contract")]},
		scope:      registry.Metadata{"database": "prod", "timeout": "30"},
	}
	mockInst.instances[defaultBindingID] = defaultInstance

	err := vm.Import(`
		function test_default_binding_with_context()
			local contract = require("contract")
			local security = require("security")
			
			-- Get a contract
			local c, err = contract.get("test:contract")
			if err then error("Error getting contract: " .. err) end
			
			-- Chain context that provides required values
			local c_with_ctx = c:with_context({
				database = "prod",
				timeout = 30
			})
			
			-- Open using default binding with context
			local instance, err = c_with_ctx:open()
			if err then error("Error opening default binding with context: " .. err) end
			
			-- Verify instance exists
			assert(instance ~= nil, "instance should not be nil")
			
			-- Check if instance implements our contract
			local implements_contract = contract.is(instance, "test:contract")
			assert(implements_contract == true, "instance should implement test:contract")
			
			-- Test opening with additional context via open() parameter
			local instance2, err = c_with_ctx:open(nil, {priority = "high"})
			if err then error("Error opening default binding with additional context: " .. err) end
			
			assert(instance2 ~= nil, "instance2 should not be nil")
			
			return "success"
		end
	`, "test", "test_default_binding_with_context")
	require.NoError(t, err, "Failed to import test function")

	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))
	result, err := runner.Execute(ctx, "test_default_binding_with_context")
	require.NoError(t, err, "Lua execution failed")
	assert.Equal(t, "success", result.String())
}

// TestContractOpenDefaultBindingNotFound tests failure when no default binding exists
func TestContractOpenDefaultBindingNotFound(t *testing.T) {
	vm, uw, ctx, _, _ := setupContractTest(t)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	err := vm.Import(`
		function test_default_binding_not_found()
			local contract = require("contract")
			
			-- Get a contract (no default binding configured)
			local c, err = contract.get("test:contract")
			if err then error("Error getting contract: " .. err) end
			
			-- Try to open using default binding (should fail)
			local instance, err = c:open()
			assert(instance == nil, "instance should be nil when no default binding")
			assert(err ~= nil, "should return error when no default binding")
			assert(string.match(err, "no default binding"), "error should mention no default binding")
			
			return "success"
		end
	`, "test", "test_default_binding_not_found")
	require.NoError(t, err, "Failed to import test function")

	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))
	result, err := runner.Execute(ctx, "test_default_binding_not_found")
	require.NoError(t, err, "Lua execution failed")
	assert.Equal(t, "success", result.String())
}

// TestContractIntrospection tests contract inspection methods
func TestContractIntrospection(t *testing.T) {
	vm, uw, ctx, _, _ := setupContractTest(t)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	err := vm.Import(`
		function test_contract_introspection()
			local contract = require("contract")
			
			-- Get a contract
			local c, err = contract.get("test:contract")
			if err then error("Error getting contract: " .. err) end
			
			-- Test method() function for specific method
			local method, err = c:method("test_method")
			if err then error("Error getting method: " .. err) end
			
			assert(method ~= nil, "method should not be nil")
			assert(method.name == "test_method", "method name should match")
			assert(method.description == "A test method", "method description should match")
			assert(method.input_schemas ~= nil, "should have input schemas")
			assert(method.output_schemas ~= nil, "should have output schemas")
			
			-- Test implementations() function
			local implementations, err = c:implementations()
			if err then error("Error getting implementations: " .. err) end
			
			assert(implementations ~= nil, "implementations should not be nil")
			assert(#implementations == 1, "should have 1 implementation")
			assert(implementations[1] == "test:binding", "implementation should match")
			
			return "success"
		end
	`, "test", "test_contract_introspection")
	require.NoError(t, err, "Failed to import test function")

	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))
	result, err := runner.Execute(ctx, "test_contract_introspection")
	require.NoError(t, err, "Lua execution failed")
	assert.Equal(t, "success", result.String())
}

// TestContractErrorHandling tests error conditions
func TestContractErrorHandling(t *testing.T) {
	vm, uw, ctx, _, _ := setupContractTest(t)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	err := vm.Import(`
		function test_error_handling()
			local contract = require("contract")
			
			-- Test non-existent contract
			local c, err = contract.get("nonexistent:contract")
			assert(c == nil, "should return nil for non-existent contract")
			assert(err ~= nil, "should return error for non-existent contract")
			
			-- Test non-existent method
			local c, err = contract.get("test:contract")
			if err then error("Error getting contract: " .. err) end
			
			local method, err = c:method("nonexistent_method")
			assert(method == nil, "should return nil for non-existent method")
			assert(err ~= nil, "should return error for non-existent method")
			
			-- Test opening non-existent binding
			local instance, err = c:open("nonexistent:binding")
			assert(instance == nil, "should return nil for non-existent binding")
			assert(err ~= nil, "should return error for non-existent binding")
			
			return "success"
		end
	`, "test", "test_error_handling")
	require.NoError(t, err, "Failed to import test function")

	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))
	result, err := runner.Execute(ctx, "test_error_handling")
	require.NoError(t, err, "Lua execution failed")
	assert.Equal(t, "success", result.String())
}

// TestContractSecurityValidation tests security restrictions
func TestContractSecurityValidation(t *testing.T) {
	vm, uw, ctx, _, _ := setupContractTest(t)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	err := vm.Import(`
		function test_security_validation()
			local contract = require("contract")
			local security = require("security")
			
			-- Get a contract
			local c, err = contract.get("test:contract")
			if err then error("Error getting contract: " .. err) end
			
			-- Test that nil actor/scope are rejected
			local success, err = pcall(function()
				local c_with_nil_actor = c:with_actor(nil)
			end)
			assert(not success, "should reject nil actor")
			assert(string.match(err, "actor cannot be nil"), "error should mention nil actor")
			
			local success, err = pcall(function()
				local c_with_nil_scope = c:with_scope(nil)
			end)
			assert(not success, "should reject nil scope")
			assert(string.match(err, "scope cannot be nil"), "error should mention nil scope")
			
			return "success"
		end
	`, "test", "test_security_validation")
	require.NoError(t, err, "Failed to import test function")

	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))
	result, err := runner.Execute(ctx, "test_security_validation")
	require.NoError(t, err, "Lua execution failed")
	assert.Equal(t, "success", result.String())
}

// TestContractFindImplementations tests the module-level find_implementations function
func TestContractFindImplementations(t *testing.T) {
	vm, uw, ctx, _, _ := setupContractTest(t)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	err := vm.Import(`
		function test_find_implementations()
			local contract = require("contract")
			
			-- Test finding implementations for existing contract
			local implementations, err = contract.find_implementations("test:contract")
			if err then error("Error finding implementations: " .. err) end
			
			assert(implementations ~= nil, "implementations should not be nil")
			assert(#implementations == 1, "should have 1 implementation")
			assert(implementations[1] == "test:binding", "implementation should match")
			
			-- Test finding implementations for non-existent contract
			local implementations, err = contract.find_implementations("nonexistent:contract")
			if err then error("Error finding implementations: " .. err) end
			
			assert(implementations ~= nil, "should return empty array for non-existent contract")
			assert(#implementations == 0, "should have 0 implementations for non-existent contract")
			
			return "success"
		end
	`, "test", "test_find_implementations")
	require.NoError(t, err, "Failed to import test function")

	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))
	result, err := runner.Execute(ctx, "test_find_implementations")
	require.NoError(t, err, "Lua execution failed")
	assert.Equal(t, "success", result.String())
}

// TestContractOpenWithContext tests opening contracts with additional context
func TestContractOpenWithContext(t *testing.T) {
	vm, uw, ctx, _, _ := setupContractTest(t)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	err := vm.Import(`
		function test_open_with_context()
			local contract = require("contract")
			local security = require("security")
			
			-- Get a contract and add context
			local c, err = contract.get("test:contract")
			if err then error("Error getting contract: " .. err) end
			
			-- Chain multiple context types
			local actor = security.new_actor("test_user")
			local c_with_ctx = c:with_context({env = "test", version = "1.0"})
			local c_with_actor = c_with_ctx:with_actor(actor)
			
			-- Open with additional context values
			local instance, err = c_with_actor:open("test:binding", {
				session_id = "abc123",
				priority = "high"
			})
			if err then error("Error opening contract: " .. err) end
			
			-- Verify instance was created successfully
			assert(instance ~= nil, "instance should not be nil")
			
			-- Verify instance implements the contract
			local implements_contract = contract.is(instance, "test:contract")
			assert(implements_contract == true, "instance should implement test:contract")
			
			return "success"
		end
	`, "test", "test_open_with_context")
	require.NoError(t, err, "Failed to import test function")

	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))
	result, err := runner.Execute(ctx, "test_open_with_context")
	require.NoError(t, err, "Lua execution failed")
	assert.Equal(t, "success", result.String())
}

// TestContractContextInheritance tests that security context is properly inherited through chains
func TestContractContextInheritance(t *testing.T) {
	vm, uw, ctx, _, _ := setupContractTest(t)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	err := vm.Import(`
		function test_context_inheritance()
			local contract = require("contract")
			local security = require("security")
			
			-- Get contract
			local c, err = contract.get("test:contract")
			if err then error("Error getting contract: " .. err) end
			
			-- Chain multiple context modifications
			local actor = security.new_actor("service_user")
			local scope = security.new_scope()
			
			local c1 = c:with_context({env = "production"})
			local c2 = c1:with_actor(actor)
			local c3 = c2:with_scope(scope)
			local c4 = c3:with_context({version = "2.0"})
			
			-- Each step should return a valid contract object
			assert(c1:id() == "test:contract", "c1 should have correct ID")
			assert(c2:id() == "test:contract", "c2 should have correct ID")
			assert(c3:id() == "test:contract", "c3 should have correct ID")
			assert(c4:id() == "test:contract", "c4 should have correct ID")
			
			-- Opening should work with the fully chained context
			local instance, err = c4:open("test:binding")
			if err then error("Error opening with chained context: " .. err) end
			
			assert(instance ~= nil, "instance should be created")
			
			-- Verify instance implements the contract
			local implements_contract = contract.is(instance, "test:contract")
			assert(implements_contract == true, "instance should implement test:contract")
			
			return "success"
		end
	`, "test", "test_context_inheritance")
	require.NoError(t, err, "Failed to import test function")

	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))
	result, err := runner.Execute(ctx, "test_context_inheritance")
	require.NoError(t, err, "Lua execution failed")
	assert.Equal(t, "success", result.String())
}

// TestContractMultipleContracts tests working with multiple contracts simultaneously
func TestContractMultipleContracts(t *testing.T) {
	vm, uw, ctx, mockReg, mockInst := setupContractTest(t)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Add another test contract to the registry
	secondContract := &mockContract{
		id:   registry.ParseID("test:contract2"),
		meta: registry.Metadata{"version": "2.0"},
		methods: []contract.MethodDef{
			{
				Name:        "another_method",
				Description: "Another test method",
			},
		},
	}

	secondInstance := &mockInstance{
		id:         registry.ParseID("test:binding2"),
		implements: []contract.Contract{secondContract},
		scope:      registry.Metadata{"type": "secondary"},
	}

	// Add to mocks
	mockReg.contracts[registry.ParseID("test:contract2")] = secondContract
	mockReg.bindings[registry.ParseID("test:binding2")] = &contract.Binding{
		ID:   registry.ParseID("test:binding2"),
		Meta: registry.Metadata{"version": "2.0"},
	}
	mockReg.bindingsForContract[registry.ParseID("test:contract2")] = []registry.ID{registry.ParseID("test:binding2")}
	mockInst.instances[registry.ParseID("test:binding2")] = secondInstance

	err := vm.Import(`
		function test_multiple_contracts()
			local contract = require("contract")
			
			-- Get both contracts
			local c1, err = contract.get("test:contract")
			if err then error("Error getting contract1: " .. err) end
			
			local c2, err = contract.get("test:contract2")
			if err then error("Error getting contract2: " .. err) end
			
			-- Verify they have different IDs and methods
			assert(c1:id() == "test:contract", "c1 should have correct ID")
			assert(c2:id() == "test:contract2", "c2 should have correct ID")
			
			local methods1 = c1:methods()
			local methods2 = c2:methods()
			
			assert(methods1[1].name == "test_method", "c1 should have test_method")
			assert(methods2[1].name == "another_method", "c2 should have another_method")
			
			-- Open instances from both contracts
			local instance1, err = c1:open("test:binding")
			if err then error("Error opening instance1: " .. err) end
			
			local instance2, err = c2:open("test:binding2")
			if err then error("Error opening instance2: " .. err) end
			
			-- Verify instances using contract.is()
			assert(contract.is(instance1, "test:contract") == true, "instance1 should implement test:contract")
			assert(contract.is(instance1, "test:contract2") == false, "instance1 should not implement test:contract2")
			
			assert(contract.is(instance2, "test:contract2") == true, "instance2 should implement test:contract2")
			assert(contract.is(instance2, "test:contract") == false, "instance2 should not implement test:contract")
			
			return "success"
		end
	`, "test", "test_multiple_contracts")
	require.NoError(t, err, "Failed to import test function")

	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))
	result, err := runner.Execute(ctx, "test_multiple_contracts")
	require.NoError(t, err, "Lua execution failed")
	assert.Equal(t, "success", result.String())
}

// TestContractEdgeCases tests edge cases and boundary conditions
func TestContractEdgeCases(t *testing.T) {
	vm, uw, ctx, _, _ := setupContractTest(t)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	err := vm.Import(`
		function test_edge_cases()
			local contract = require("contract")
			local security = require("security")
			
			-- Test empty string IDs (should fail)
			local c, err = contract.get("")
			assert(c == nil, "should fail for empty contract ID")
			assert(err ~= nil, "should return error for empty contract ID")
			
			-- Test malformed IDs
			local c, err = contract.get("malformed")
			assert(c == nil, "should fail for malformed contract ID")
			assert(err ~= nil, "should return error for malformed contract ID")
			
			-- Test multiple calls to same contract (should work)
			local c1, err = contract.get("test:contract")
			if err then error("Error getting contract: " .. err) end
			
			local c2, err = contract.get("test:contract")
			if err then error("Error getting contract: " .. err) end
			
			assert(c1:id() == c2:id(), "multiple calls should return equivalent contracts")
			
			-- Test chaining on the same contract multiple times
			local actor = security.new_actor("test")
			local c_with_actor1 = c1:with_actor(actor)
			local c_with_actor2 = c1:with_actor(actor)
			
			assert(c_with_actor1:id() == c_with_actor2:id(), "multiple chaining should work")
			
			-- Test opening non-existent binding
			local instance, err = c1:open("nonexistent:binding")
			assert(instance == nil, "should fail for non-existent binding")
			assert(err ~= nil, "should return error for non-existent binding")
			
			return "success"
		end
	`, "test", "test_edge_cases")
	require.NoError(t, err, "Failed to import test function")

	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))
	result, err := runner.Execute(ctx, "test_edge_cases")
	require.NoError(t, err, "Lua execution failed")
	assert.Equal(t, "success", result.String())
}

// TestContractIs tests the new contract.is() functionality specifically
func TestContractIs(t *testing.T) {
	vm, uw, ctx, _, _ := setupContractTest(t)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	err := vm.Import(`
		function test_contract_is()
			local contract = require("contract")
			
			-- Get a contract
			local c, err = contract.get("test:contract")
			if err then error("Error getting contract: " .. err) end
			
			-- Open the contract to create an instance
			local instance, err = c:open("test:binding")
			if err then error("Error opening contract: " .. err) end
			
			-- Test valid contract.is() scenarios
			
			-- Should return true for implemented contract
			local result1 = contract.is(instance, "test:contract")
			assert(result1 == true, "should return true for implemented contract")
			
			-- Should return false for non-implemented contract  
			local result2 = contract.is(instance, "other:contract")
			assert(result2 == false, "should return false for non-implemented contract")
			
			-- Should return false for malformed contract ID
			local result3 = contract.is(instance, "malformed")
			assert(result3 == false, "should return false for malformed contract ID")
			
			-- Should return false for empty contract ID
			local result4 = contract.is(instance, "")
			assert(result4 == false, "should return false for empty contract ID")
			
			-- Test with contract wrapper object (should return false)
			assert(contract.is(c, "test:contract") == false, "contract wrapper should return false")
			
			return "success"
		end
	`, "test", "test_contract_is")
	require.NoError(t, err, "Failed to import test function")

	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))
	result, err := runner.Execute(ctx, "test_contract_is")
	require.NoError(t, err, "Lua execution failed")
	assert.Equal(t, "success", result.String())
}

// TestContractMethodCalling tests that instances can still call methods dynamically
func TestContractMethodCalling(t *testing.T) {
	vm, uw, ctx, _, _ := setupContractTest(t)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	err := vm.Import(`
		function test_method_calling()
			local contract = require("contract")
			
			-- Get a contract and open instance
			local c, err = contract.get("test:contract")
			if err then error("Error getting contract: " .. err) end
			
			local instance, err = c:open("test:binding")
			if err then error("Error opening contract: " .. err) end
			
			-- Verify instance implements the contract
			assert(contract.is(instance, "test:contract") == true, "instance should implement test:contract")
			
			-- Test method calling (should still work)
			local result, err = instance:test_method("test_arg")
			if err then error("Error calling method: " .. err) end
			
			-- Mock should return "test_method_called"
			assert(result == "test_method_called", "method should return expected result")
			
			return "success"
		end
	`, "test", "test_method_calling")
	require.NoError(t, err, "Failed to import test function")

	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))
	result, err := runner.Execute(ctx, "test_method_calling")
	require.NoError(t, err, "Lua execution failed")
	assert.Equal(t, "success", result.String())
}

// TestContractIsEdgeCases tests edge cases for the contract.is() function
func TestContractIsEdgeCases(t *testing.T) {
	vm, uw, ctx, _, _ := setupContractTest(t)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	err := vm.Import(`
		function test_contract_is_edge_cases()
			local contract = require("contract")
			
			-- Get a contract and create instance for valid tests
			local c, err = contract.get("test:contract")
			if err then error("Error getting contract: " .. err) end
			
			local instance, err = c:open("test:binding")
			if err then error("Error opening contract: " .. err) end
			
			-- Test various edge cases with valid userdata
			
			-- Valid case first
			assert(contract.is(instance, "test:contract") == true, "valid case should work")
			
			-- Test with contract wrapper object (should return false)
			assert(contract.is(c, "test:contract") == false, "contract wrapper should return false")
			
			-- Test with different case contract ID
			assert(contract.is(instance, "TEST:CONTRACT") == false, "case sensitive contract ID should return false")
			
			-- Test with contract ID that has extra parts
			assert(contract.is(instance, "test:contract:extra") == false, "extra parts in contract ID should return false")
			
			-- Test with partial contract ID
			assert(contract.is(instance, "test") == false, "partial contract ID should return false")
			
			-- Test with namespace-only contract ID
			assert(contract.is(instance, "test:") == false, "namespace-only contract ID should return false")
			
			-- Test with colon-only contract ID
			assert(contract.is(instance, ":") == false, "colon-only contract ID should return false")
			
			return "success"
		end
	`, "test", "test_contract_is_edge_cases")
	require.NoError(t, err, "Failed to import test function")

	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))
	result, err := runner.Execute(ctx, "test_contract_is_edge_cases")
	require.NoError(t, err, "Lua execution failed")
	assert.Equal(t, "success", result.String())
}

// TestContractContextMergingAndIsolation tests context inheritance, merging, and isolation
func TestContractContextMergingAndIsolation(t *testing.T) {
	vm, uw, ctx, _, _ := setupContractTest(t)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	err := vm.Import(`
		function test_context_merging_and_isolation()
			local contract = require("contract")
			local security = require("security")
			
			-- Get base contract
			local base_contract, err = contract.get("test:contract")
			if err then error("Error getting contract: " .. err) end
			
			-- Step 1: Create contract with initial context
			local contract_v1 = base_contract:with_context({
				environment = "dev",
				version = "1.0",
				feature_flag = "old_feature"
			})
			
			-- Step 2: Open first instance with this context
			local instance1, err = contract_v1:open("test:binding")
			if err then error("Error opening instance1: " .. err) end
			
			-- Step 3: Modify context on contract (should not affect instance1)
			local contract_v2 = contract_v1:with_context({
				environment = "prod",  -- Override existing value
				version = "2.0",       -- Override existing value  
				new_feature = "enabled" -- Add new value
				-- feature_flag should remain "old_feature" from v1
			})
			
			-- Step 4: Open second instance with modified context
			local instance2, err = contract_v2:open("test:binding")
			if err then error("Error opening instance2: " .. err) end
			
			-- Step 5: Test context merging priorities with query params and open params
			local contract_v3 = base_contract:with_context({
				base_value = "from_chain",
				override_test = "chain_value",
				query_test = "chain_value"
			})
			
			-- Query params should override chained context
			-- Open params should override both chained and query
			local instance3, err = contract_v3:open("test:binding?override_test=query_value&query_test=query_value&query_only=query_param", {
				override_test = "open_value",  -- Should win (highest priority)
				open_only = "open_param"       -- Should be present
				-- base_value should remain "from_chain"
				-- query_test should be "query_value" 
				-- query_only should be "query_param"
			})
			if err then error("Error opening instance3: " .. err) end
			
			-- Verify all instances implement the contract
			assert(contract.is(instance1, "test:contract") == true, "instance1 should implement test:contract")
			assert(contract.is(instance2, "test:contract") == true, "instance2 should implement test:contract") 
			assert(contract.is(instance3, "test:contract") == true, "instance3 should implement test:contract")
			
			-- Step 6: Test that original contract is unchanged
			local instance_original, err = base_contract:open("test:binding")
			if err then error("Error opening original instance: " .. err) end
			assert(contract.is(instance_original, "test:contract") == true, "original instance should implement test:contract")
			
			-- Step 7: Test security context isolation
			local actor1 = security.new_actor("user1")
			local actor2 = security.new_actor("user2")
			
			local secured_contract1 = base_contract:with_actor(actor1):with_context({user_type = "admin"})
			local secured_contract2 = base_contract:with_actor(actor2):with_context({user_type = "regular"})
			
			local secured_instance1, err = secured_contract1:open("test:binding")
			if err then error("Error opening secured_instance1: " .. err) end
			
			local secured_instance2, err = secured_contract2:open("test:binding")  
			if err then error("Error opening secured_instance2: " .. err) end
			
			-- Both should implement the contract but have different inherited contexts
			assert(contract.is(secured_instance1, "test:contract") == true, "secured_instance1 should implement test:contract")
			assert(contract.is(secured_instance2, "test:contract") == true, "secured_instance2 should implement test:contract")
			
			return "success"
		end
	`, "test", "test_context_merging_and_isolation")
	require.NoError(t, err, "Failed to import test function")

	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))
	result, err := runner.Execute(ctx, "test_context_merging_and_isolation")
	require.NoError(t, err, "Lua execution failed")
	assert.Equal(t, "success", result.String())
}

// TestContractContextMergingBehavior tests the specific merging behavior and priorities
func TestContractContextMergingBehavior(t *testing.T) {
	vm, uw, ctx, _, _ := setupContractTest(t)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	err := vm.Import(`
		function test_context_merging_behavior()
			local contract = require("contract")
			
			-- Get base contract
			local base_contract, err = contract.get("test:contract")
			if err then error("Error getting contract: " .. err) end
			
			-- Test 1: Chained context only
			local chained_contract = base_contract
				:with_context({step1 = "value1", shared = "from_step1"})
				:with_context({step2 = "value2", shared = "from_step2"}) -- Should override shared
			
			local instance1, err = chained_contract:open("test:binding")
			if err then error("Error opening instance1: " .. err) end
			
			-- Test 2: Query parameters override chained context
			local instance2, err = chained_contract:open("test:binding?shared=from_query&query_only=query_value")
			if err then error("Error opening instance2: " .. err) end
			
			-- Test 3: Open parameters override both chained and query
			local instance3, err = chained_contract:open("test:binding?shared=from_query&query_only=query_value", {
				shared = "from_open",
				open_only = "open_value"
			})
			if err then error("Error opening instance3: " .. err) end
			
			-- Test 4: Complex merging with all three sources
			local complex_contract = base_contract:with_context({
				chain_only = "chain_value",
				chain_query = "chain_value", 
				chain_open = "chain_value",
				all_three = "chain_value"
			})
			
			local instance4, err = complex_contract:open("test:binding?chain_query=query_value&query_open=query_value&all_three=query_value", {
				chain_open = "open_value",
				query_open = "open_value", 
				all_three = "open_value",
				open_only = "open_value"
			})
			if err then error("Error opening instance4: " .. err) end
			
			-- Test 5: Type conversion in query parameters
			local instance5, err = base_contract:open("test:binding?bool_param=true&int_param=42&float_param=3.14&string_param=hello")
			if err then error("Error opening instance5: " .. err) end
			
			-- All instances should implement the contract
			assert(contract.is(instance1, "test:contract") == true, "instance1 should implement test:contract")
			assert(contract.is(instance2, "test:contract") == true, "instance2 should implement test:contract")
			assert(contract.is(instance3, "test:contract") == true, "instance3 should implement test:contract")
			assert(contract.is(instance4, "test:contract") == true, "instance4 should implement test:contract")
			assert(contract.is(instance5, "test:contract") == true, "instance5 should implement test:contract")
			
			return "success"
		end
	`, "test", "test_context_merging_behavior")
	require.NoError(t, err, "Failed to import test function")

	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))
	result, err := runner.Execute(ctx, "test_context_merging_behavior")
	require.NoError(t, err, "Lua execution failed")
	assert.Equal(t, "success", result.String())
}

// TestContractIsErrorCases tests that contract.is() properly errors on invalid arguments
func TestContractIsErrorCases(t *testing.T) {
	vm, uw, ctx, _, _ := setupContractTest(t)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	err := vm.Import(`
		function test_contract_is_errors()
			local contract = require("contract")
			
			-- Test that invalid argument types properly error
			
			-- Should error when first argument is string
			local success, err = pcall(function()
				contract.is("not an instance", "test:contract")
			end)
			assert(not success, "should error for string argument")
			assert(string.match(err, "userdata expected"), "error should mention userdata expected")
			
			-- Should error when first argument is nil
			local success, err = pcall(function()
				contract.is(nil, "test:contract")
			end)
			assert(not success, "should error for nil argument")
			assert(string.match(err, "userdata expected"), "error should mention userdata expected")
			
			-- Should error when first argument is number
			local success, err = pcall(function()
				contract.is(123, "test:contract")
			end)
			assert(not success, "should error for number argument")
			assert(string.match(err, "userdata expected"), "error should mention userdata expected")
			
			return "success"
		end
	`, "test", "test_contract_is_errors")
	require.NoError(t, err, "Failed to import test function")

	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))
	result, err := runner.Execute(ctx, "test_contract_is_errors")
	require.NoError(t, err, "Lua execution failed")
	assert.Equal(t, "success", result.String())
}
