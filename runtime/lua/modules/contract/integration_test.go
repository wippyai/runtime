// SPDX-License-Identifier: MPL-2.0

package contract_test

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	stdtime "time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	apicontract "github.com/wippyai/runtime/api/contract"
	"github.com/wippyai/runtime/api/dispatcher"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/internal/uniqid"
	"github.com/wippyai/runtime/runtime/lua/engine"
	contractmod "github.com/wippyai/runtime/runtime/lua/modules/contract"
	syscontract "github.com/wippyai/runtime/system/contract"
	"github.com/wippyai/runtime/system/eventbus"
	sysfunction "github.com/wippyai/runtime/system/function"
	"github.com/wippyai/runtime/system/function/interceptor"
	"github.com/wippyai/runtime/system/function/interceptor/retry"
	sysrelay "github.com/wippyai/runtime/system/relay"
	"github.com/wippyai/runtime/system/scheduler"
	"github.com/wippyai/runtime/system/scheduler/actor"
	"go.uber.org/zap"
)

type testScheduler struct {
	*actor.Scheduler
	contractDisp *syscontract.Dispatcher
	pending      map[string]chan *runtime.Result
	startHooks   map[string]func()
	stopHooks    map[string]func()
	mu           sync.Mutex
}

func (ts *testScheduler) Stop() {
	ts.Scheduler.Stop(context.Background())
}

// setLifecycleHooks registers per-PID callbacks fired on OnStart (after
// Process.Init) and at the start of OnComplete (before the result is delivered
// and before the worker tears the process down). A subscription sampler uses
// these to bound its observation window strictly inside the live phase.
func (ts *testScheduler) setLifecycleHooks(p pid.PID, onStart, onStop func()) {
	ts.mu.Lock()
	if ts.startHooks == nil {
		ts.startHooks = make(map[string]func())
	}
	if ts.stopHooks == nil {
		ts.stopHooks = make(map[string]func())
	}
	ts.startHooks[p.UniqID] = onStart
	ts.stopHooks[p.UniqID] = onStop
	ts.mu.Unlock()
}

func (ts *testScheduler) OnStart(_ context.Context, p pid.PID, _ process.Process) error {
	ts.mu.Lock()
	hook := ts.startHooks[p.UniqID]
	ts.mu.Unlock()
	if hook != nil {
		hook()
	}
	return nil
}

func (ts *testScheduler) OnComplete(_ context.Context, p pid.PID, result *runtime.Result) {
	ts.mu.Lock()
	ch, ok := ts.pending[p.UniqID]
	if ok {
		delete(ts.pending, p.UniqID)
	}
	stop := ts.stopHooks[p.UniqID]
	delete(ts.stopHooks, p.UniqID)
	delete(ts.startHooks, p.UniqID)
	ts.mu.Unlock()
	// Stop the sampler before the result is delivered: this OnComplete runs on
	// the worker goroutine ahead of Process.Close, so joining the sampler here
	// guarantees no sample races teardown.
	if stop != nil {
		stop()
	}
	if ok {
		ch <- result
	}
}

func (ts *testScheduler) Execute(ctx context.Context, p pid.PID, proc process.Process, method string, input payload.Payloads) (*runtime.Result, error) {
	resultCh := make(chan *runtime.Result, 1)

	ts.mu.Lock()
	ts.pending[p.UniqID] = resultCh
	ts.mu.Unlock()

	_, err := ts.Submit(ctx, p, proc, method, input)
	if err != nil {
		ts.mu.Lock()
		delete(ts.pending, p.UniqID)
		ts.mu.Unlock()
		return nil, err
	}

	select {
	case result := <-resultCh:
		return result, nil
	case <-ctx.Done():
		ts.mu.Lock()
		delete(ts.pending, p.UniqID)
		ts.mu.Unlock()
		return nil, ctx.Err()
	}
}

var testPIDCounter atomic.Int64

func uniqueTestPID() pid.PID {
	return pid.PID{UniqID: stdtime.Now().Format("20060102150405.000000000") + "-" + strconv.FormatInt(testPIDCounter.Add(1), 10)}
}

// extractInt64 extracts an int64 from various types (Lua or Go)
func extractInt64(v any) int64 {
	switch val := v.(type) {
	case int64:
		return val
	case int:
		return int64(val)
	case lua.LInteger:
		return int64(val)
	case lua.LNumber:
		return int64(val)
	}
	return 0
}

type integrationTestContext struct {
	ctx              context.Context
	bus              event.Bus
	contractRegistry *syscontract.Registry
	functionRegistry *sysfunction.Registry
	instantiator     apicontract.Instantiator
	scheduler        *testScheduler
	node             relay.Node
}

func setupIntegrationTest(t *testing.T, numWorkers int) *integrationTestContext {
	// Configure error metadata extractor so Lua errors preserve kind/retryable from apierror
	lua.SetErrorMetadataExtractor(func(err error) *lua.ErrorMetadata {
		chain := apierror.BuildChain(err)
		if chain == nil {
			return nil
		}
		root := chain.Root()
		if root == nil {
			return nil
		}
		meta := &lua.ErrorMetadata{}
		if root.Kind != "" {
			meta.Kind = lua.Kind(root.Kind)
		}
		if root.Retryable != nil {
			b := *root.Retryable
			meta.Retryable = &b
		}
		if len(root.Details) > 0 {
			meta.Details = make(map[string]any, len(root.Details))
			for k, v := range root.Details {
				meta.Details[k] = v
			}
		}
		if meta.Kind == "" && meta.Retryable == nil && meta.Details == nil {
			return nil
		}
		return meta
	})

	logger := zap.NewNop()
	bus := eventbus.NewBus()

	node := sysrelay.NewNode("test")

	contractRegistry := syscontract.NewContractRegistry(bus, logger)
	functionRegistry := sysfunction.NewFunctionRegistry(bus, logger)
	instantiator := syscontract.NewContractInstantiator(contractRegistry, functionRegistry)

	interceptorRegistry := interceptor.NewRegistry(logger)
	require.NoError(t, interceptorRegistry.Register("retry", retry.New(), 20))

	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	ctx = relay.WithNode(ctx, node)
	ctx = apicontract.WithContracts(ctx, contractRegistry, instantiator)
	ctx = function.WithRegistry(ctx, functionRegistry)
	ctx = function.WithInterceptorRegistry(ctx, interceptorRegistry)

	// Set up PID generator for function calls
	uniqGen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(uniqGen, "test")
	ctx = process.WithPIDGenerator(ctx, pidGen)

	require.NoError(t, contractRegistry.Start(ctx))
	require.NoError(t, functionRegistry.Start(ctx))

	reg := scheduler.NewRegistry()

	contractDisp := syscontract.NewDispatcher(node, nil)
	contractDisp.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		reg.Register(id, h)
	})

	funcDisp := sysfunction.NewDispatcher(node, zap.NewNop())
	funcDisp.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		reg.Register(id, h)
	})

	ts := &testScheduler{
		pending:      make(map[string]chan *runtime.Result),
		startHooks:   make(map[string]func()),
		stopHooks:    make(map[string]func()),
		contractDisp: contractDisp,
	}

	opts := []actor.Option{
		actor.WithWorkers(numWorkers),
		actor.WithLifecycle(ts),
	}
	ts.Scheduler = actor.NewScheduler(reg, opts...)
	ts.Start()
	ts.EnableStats()

	return &integrationTestContext{
		ctx:              ctx,
		bus:              bus,
		contractRegistry: contractRegistry,
		functionRegistry: functionRegistry,
		instantiator:     instantiator,
		scheduler:        ts,
		node:             node,
	}
}

func (tc *integrationTestContext) Close(t *testing.T) {
	tc.scheduler.Stop()
	require.NoError(t, tc.contractRegistry.Stop())
	require.NoError(t, tc.functionRegistry.Stop())
}

func (tc *integrationTestContext) registerFunction(t *testing.T, funcID registry.ID, handler function.Func) {
	var wg sync.WaitGroup
	sub, err := eventbus.NewSubscriber(tc.ctx, tc.bus, function.System, "function.*", func(evt event.Event) {
		if evt.Kind == function.FunctionAccept && evt.Path == funcID.String() {
			wg.Done()
		}
	})
	require.NoError(t, err)
	defer sub.Close()

	wg.Add(1)
	tc.bus.Send(tc.ctx, event.Event{
		System: function.System,
		Kind:   function.FunctionRegister,
		Path:   funcID.String(),
		Data: &function.FuncEntry{
			Handler: handler,
			Options: nil,
		},
	})
	wg.Wait()
}

func (tc *integrationTestContext) registerContract(t *testing.T, contractID registry.ID, def *apicontract.Definition) {
	var wg sync.WaitGroup
	sub, err := eventbus.NewSubscriber(tc.ctx, tc.bus, apicontract.System, "contract.*", func(evt event.Event) {
		if evt.Kind == apicontract.ContractAccept && evt.Path == contractID.String() {
			wg.Done()
		}
	})
	require.NoError(t, err)
	defer sub.Close()

	wg.Add(1)
	tc.bus.Send(tc.ctx, event.Event{
		System: apicontract.System,
		Kind:   apicontract.RegisterDefinition,
		Path:   contractID.String(),
		Data:   def,
	})
	wg.Wait()
}

func (tc *integrationTestContext) registerBinding(t *testing.T, bindingID registry.ID, binding *apicontract.Binding) {
	var wg sync.WaitGroup
	sub, err := eventbus.NewSubscriber(tc.ctx, tc.bus, apicontract.System, "contract.*", func(evt event.Event) {
		if evt.Kind == apicontract.ContractAccept && evt.Path == bindingID.String() {
			wg.Done()
		}
	})
	require.NoError(t, err)
	defer sub.Close()

	wg.Add(1)
	tc.bus.Send(tc.ctx, event.Event{
		System: apicontract.System,
		Kind:   apicontract.RegisterBinding,
		Path:   bindingID.String(),
		Data:   binding,
	})
	wg.Wait()
}

func bindContractModule(l *lua.LState) error {
	tbl, _ := contractmod.Module.Build()
	l.SetGlobal(contractmod.Module.Name, tbl)
	return nil
}

func newLuaProcess(t *testing.T, script string) *engine.Process {
	t.Helper()
	proto, _ := lua.CompileString(script, "test.lua")
	proc, err := engine.NewProcess(
		engine.WithProto(proto),
		engine.WithModuleBinder(func(l *lua.LState) error {
			engine.LoadModuleDef(l, engine.ChannelModule)
			return nil
		}),
		engine.WithModuleBinder(bindContractModule),
	)
	if err != nil {
		t.Fatalf("NewProcess failed: %v", err)
	}
	return proc
}

func TestIntegration_OpenBinding(t *testing.T) {
	tc := setupIntegrationTest(t, 4)
	defer tc.Close(t)

	funcID := registry.NewID("test", "greet")
	tc.registerFunction(t, funcID, func(_ context.Context, task runtime.Task) (*runtime.Result, error) {
		name := "World"
		if len(task.Payloads) > 0 {
			if s, ok := task.Payloads[0].Data().(string); ok {
				name = s
			}
		}
		return &runtime.Result{Value: payload.New("Hello, " + name)}, nil
	})

	contractID := registry.NewID("test", "greeter")
	tc.registerContract(t, contractID, &apicontract.Definition{
		Methods: []apicontract.MethodDef{{Name: "greet", Description: "Greet someone"}},
	})

	bindingID := registry.NewID("test", "default_greeter")
	tc.registerBinding(t, bindingID, &apicontract.Binding{
		Contracts: []apicontract.BoundContract{
			{
				Contract: contractID,
				Methods:  map[string]registry.ID{"greet": funcID},
				Default:  true,
			},
		},
	})

	script := `
		local instance, err = contract.open("test:default_greeter")
		if err then
			return nil, tostring(err)
		end
		return contract.is(instance, "test:greeter")
	`

	frameCtx, _ := ctxapi.OpenFrameContext(tc.ctx)
	proc := newLuaProcess(t, script)

	result, err := tc.scheduler.Execute(frameCtx, uniqueTestPID(), proc, "", nil)
	require.NoError(t, err)
	require.Nil(t, result.Error)
	require.NotNil(t, result.Value)
	assert.Equal(t, lua.LTrue, result.Value.Data())
}

func TestIntegration_CallMethod(t *testing.T) {
	tc := setupIntegrationTest(t, 4)
	defer tc.Close(t)

	funcID := registry.NewID("test", "add")
	tc.registerFunction(t, funcID, func(_ context.Context, task runtime.Task) (*runtime.Result, error) {
		var a, b int64
		if len(task.Payloads) > 0 {
			a = extractInt64(task.Payloads[0].Data())
		}
		if len(task.Payloads) > 1 {
			b = extractInt64(task.Payloads[1].Data())
		}
		return &runtime.Result{Value: payload.New(a + b)}, nil
	})

	contractID := registry.NewID("test", "calculator")
	tc.registerContract(t, contractID, &apicontract.Definition{
		Methods: []apicontract.MethodDef{{Name: "add", Description: "Add two numbers"}},
	})

	bindingID := registry.NewID("test", "calc_impl")
	tc.registerBinding(t, bindingID, &apicontract.Binding{
		Contracts: []apicontract.BoundContract{
			{
				Contract: contractID,
				Methods:  map[string]registry.ID{"add": funcID},
				Default:  true,
			},
		},
	})

	script := `
		local instance, err = contract.open("test:calc_impl")
		if err then
			return nil, tostring(err)
		end
		local result, err = instance:add(10, 32)
		if err then
			return nil, tostring(err)
		end
		return result
	`

	frameCtx, _ := ctxapi.OpenFrameContext(tc.ctx)
	proc := newLuaProcess(t, script)

	result, err := tc.scheduler.Execute(frameCtx, uniqueTestPID(), proc, "", nil)
	require.NoError(t, err)
	require.Nil(t, result.Error)
	require.NotNil(t, result.Value)
	assert.Equal(t, lua.LInteger(42), result.Value.Data())
}

func TestIntegration_ScopeContext(t *testing.T) {
	tc := setupIntegrationTest(t, 4)
	defer tc.Close(t)

	funcID := registry.NewID("test", "get_user")
	tc.registerFunction(t, funcID, func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
		values := ctxapi.GetValues(ctx)
		if values == nil {
			return &runtime.Result{Value: payload.New("no context")}, nil
		}
		userID, ok := values.Get("user_id")
		if !ok {
			return &runtime.Result{Value: payload.New("no user_id")}, nil
		}
		return &runtime.Result{Value: payload.New("user:" + userID.(string))}, nil
	})

	contractID := registry.NewID("test", "user_service")
	tc.registerContract(t, contractID, &apicontract.Definition{
		Methods: []apicontract.MethodDef{{Name: "get_user"}},
	})

	bindingID := registry.NewID("test", "user_impl")
	tc.registerBinding(t, bindingID, &apicontract.Binding{
		Contracts: []apicontract.BoundContract{
			{
				Contract:        contractID,
				Methods:         map[string]registry.ID{"get_user": funcID},
				ContextRequired: []string{"user_id"},
			},
		},
	})

	script := `
		local instance, err = contract.open("test:user_impl", {user_id = "12345"})
		if err then
			return nil, tostring(err)
		end
		local result, err = instance:get_user()
		if err then
			return nil, tostring(err)
		end
		return result
	`

	frameCtx, _ := ctxapi.OpenFrameContext(tc.ctx)
	proc := newLuaProcess(t, script)

	result, err := tc.scheduler.Execute(frameCtx, uniqueTestPID(), proc, "", nil)
	require.NoError(t, err)
	require.Nil(t, result.Error)
	require.NotNil(t, result.Value)
	assert.Equal(t, "user:12345", string(result.Value.Data().(lua.LString)))
}

func TestIntegration_MethodNotFound(t *testing.T) {
	tc := setupIntegrationTest(t, 4)
	defer tc.Close(t)

	funcID := registry.NewID("test", "noop")
	tc.registerFunction(t, funcID, func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		return &runtime.Result{Value: payload.New("ok")}, nil
	})

	contractID := registry.NewID("test", "simple")
	tc.registerContract(t, contractID, &apicontract.Definition{
		Methods: []apicontract.MethodDef{{Name: "doit"}},
	})

	bindingID := registry.NewID("test", "simple_impl")
	tc.registerBinding(t, bindingID, &apicontract.Binding{
		Contracts: []apicontract.BoundContract{
			{
				Contract: contractID,
				Methods:  map[string]registry.ID{"doit": funcID},
			},
		},
	})

	script := `
		local instance, err = contract.open("test:simple_impl")
		if err then
			return nil, tostring(err)
		end
		-- Access a method that doesn't exist returns nil
		local method = instance.nonexistent
		if method == nil then
			return "method_not_found"
		end
		return "should_not_reach"
	`

	frameCtx, _ := ctxapi.OpenFrameContext(tc.ctx)
	proc := newLuaProcess(t, script)

	result, err := tc.scheduler.Execute(frameCtx, uniqueTestPID(), proc, "", nil)
	require.NoError(t, err)
	require.Nil(t, result.Error)
	require.NotNil(t, result.Value)
	assert.Equal(t, "method_not_found", string(result.Value.Data().(lua.LString)))
}

func TestIntegration_MultipleMethodCalls(t *testing.T) {
	tc := setupIntegrationTest(t, 4)
	defer tc.Close(t)

	addID := registry.NewID("test", "math_add")
	mulID := registry.NewID("test", "math_mul")

	tc.registerFunction(t, addID, func(_ context.Context, task runtime.Task) (*runtime.Result, error) {
		var a, b int64
		if len(task.Payloads) > 0 {
			a = extractInt64(task.Payloads[0].Data())
		}
		if len(task.Payloads) > 1 {
			b = extractInt64(task.Payloads[1].Data())
		}
		return &runtime.Result{Value: payload.New(a + b)}, nil
	})

	tc.registerFunction(t, mulID, func(_ context.Context, task runtime.Task) (*runtime.Result, error) {
		var a, b int64
		if len(task.Payloads) > 0 {
			a = extractInt64(task.Payloads[0].Data())
		}
		if len(task.Payloads) > 1 {
			b = extractInt64(task.Payloads[1].Data())
		}
		return &runtime.Result{Value: payload.New(a * b)}, nil
	})

	contractID := registry.NewID("test", "math")
	tc.registerContract(t, contractID, &apicontract.Definition{
		Methods: []apicontract.MethodDef{
			{Name: "add"},
			{Name: "mul"},
		},
	})

	bindingID := registry.NewID("test", "math_impl")
	tc.registerBinding(t, bindingID, &apicontract.Binding{
		Contracts: []apicontract.BoundContract{
			{
				Contract: contractID,
				Methods: map[string]registry.ID{
					"add": addID,
					"mul": mulID,
				},
			},
		},
	})

	script := `
		local m, err = contract.open("test:math_impl")
		if err then return nil, tostring(err) end

		local a, err = m:add(2, 3)
		if err then return nil, tostring(err) end

		local b, err = m:mul(a, 4)
		if err then return nil, tostring(err) end

		return b  -- (2+3)*4 = 20
	`

	frameCtx, _ := ctxapi.OpenFrameContext(tc.ctx)
	proc := newLuaProcess(t, script)

	result, err := tc.scheduler.Execute(frameCtx, uniqueTestPID(), proc, "", nil)
	require.NoError(t, err)
	require.Nil(t, result.Error)
	require.NotNil(t, result.Value)
	assert.Equal(t, lua.LInteger(20), result.Value.Data())
}

func TestIntegration_InstanceImplements(t *testing.T) {
	tc := setupIntegrationTest(t, 4)
	defer tc.Close(t)

	funcID := registry.NewID("test", "noop")
	tc.registerFunction(t, funcID, func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		return &runtime.Result{Value: payload.New("ok")}, nil
	})

	contract1 := registry.NewID("test", "service_a")
	contract2 := registry.NewID("test", "service_b")

	tc.registerContract(t, contract1, &apicontract.Definition{
		Methods: []apicontract.MethodDef{{Name: "method_a"}},
	})
	tc.registerContract(t, contract2, &apicontract.Definition{
		Methods: []apicontract.MethodDef{{Name: "method_b"}},
	})

	bindingID := registry.NewID("test", "multi_impl")
	tc.registerBinding(t, bindingID, &apicontract.Binding{
		Contracts: []apicontract.BoundContract{
			{Contract: contract1, Methods: map[string]registry.ID{"method_a": funcID}},
			{Contract: contract2, Methods: map[string]registry.ID{"method_b": funcID}},
		},
	})

	script := `
		local instance, err = contract.open("test:multi_impl")
		if err then return nil, tostring(err) end

		local impl1 = contract.is(instance, "test:service_a")
		local impl2 = contract.is(instance, "test:service_b")
		return impl1 and impl2
	`

	frameCtx, _ := ctxapi.OpenFrameContext(tc.ctx)
	proc := newLuaProcess(t, script)

	result, err := tc.scheduler.Execute(frameCtx, uniqueTestPID(), proc, "", nil)
	require.NoError(t, err)
	require.Nil(t, result.Error)
	require.NotNil(t, result.Value)
	assert.Equal(t, lua.LTrue, result.Value.Data())
}

func TestIntegration_IsContract(t *testing.T) {
	tc := setupIntegrationTest(t, 4)
	defer tc.Close(t)

	funcID := registry.NewID("test", "noop")
	tc.registerFunction(t, funcID, func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		return &runtime.Result{Value: payload.New("ok")}, nil
	})

	contractID := registry.NewID("test", "checker")
	tc.registerContract(t, contractID, &apicontract.Definition{
		Methods: []apicontract.MethodDef{{Name: "check"}},
	})

	bindingID := registry.NewID("test", "checker_impl")
	tc.registerBinding(t, bindingID, &apicontract.Binding{
		Contracts: []apicontract.BoundContract{
			{Contract: contractID, Methods: map[string]registry.ID{"check": funcID}},
		},
	})

	script := `
		local instance, err = contract.open("test:checker_impl")
		if err then return nil, tostring(err) end

		local isChecker = contract.is(instance, "test:checker")
		local isOther = contract.is(instance, "test:other")

		if isChecker and not isOther then
			return "correct"
		end
		return "incorrect"
	`

	frameCtx, _ := ctxapi.OpenFrameContext(tc.ctx)
	proc := newLuaProcess(t, script)

	result, err := tc.scheduler.Execute(frameCtx, uniqueTestPID(), proc, "", nil)
	require.NoError(t, err)
	require.Nil(t, result.Error)
	require.NotNil(t, result.Value)
	assert.Equal(t, "correct", string(result.Value.Data().(lua.LString)))
}

func TestIntegration_Concurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrent test in short mode")
	}
	tc := setupIntegrationTest(t, 8)
	defer tc.Close(t)

	funcID := registry.NewID("test", "add_one")
	tc.registerFunction(t, funcID, func(_ context.Context, task runtime.Task) (*runtime.Result, error) {
		var v int64
		if len(task.Payloads) > 0 {
			v = extractInt64(task.Payloads[0].Data())
		}
		return &runtime.Result{Value: payload.New(v + 1)}, nil
	})

	contractID := registry.NewID("test", "adder")
	tc.registerContract(t, contractID, &apicontract.Definition{
		Methods: []apicontract.MethodDef{{Name: "add_one"}},
	})

	bindingID := registry.NewID("test", "adder_impl")
	tc.registerBinding(t, bindingID, &apicontract.Binding{
		Contracts: []apicontract.BoundContract{
			{Contract: contractID, Methods: map[string]registry.ID{"add_one": funcID}},
		},
	})

	const numGoroutines = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(n int) {
			defer wg.Done()

			// Embed value directly in script
			script := fmt.Sprintf(`
				local instance, err = contract.open("test:adder_impl")
				if err then return nil, tostring(err) end
				local result, err = instance:add_one(%d)
				if err then return nil, tostring(err) end
				return result
			`, n)

			frameCtx, _ := ctxapi.OpenFrameContext(tc.ctx)
			proc := newLuaProcess(t, script)

			result, err := tc.scheduler.Execute(frameCtx, uniqueTestPID(), proc, "", nil)
			require.NoError(t, err)
			require.Nil(t, result.Error)
			require.NotNil(t, result.Value)
			expected := int64(n + 1)
			assert.Equal(t, expected, extractInt64(result.Value.Data()))
		}(i)
	}

	wg.Wait()
}

func TestIntegration_WithOptions_ConfigPermutations(t *testing.T) {
	tc := setupIntegrationTest(t, 4)
	defer tc.Close(t)

	var capturedOptions runtime.Options
	funcID := registry.NewID("test", "capture_opts")
	tc.registerFunction(t, funcID, func(_ context.Context, task runtime.Task) (*runtime.Result, error) {
		capturedOptions = task.Options
		if capturedOptions != nil {
			return &runtime.Result{Value: payload.New("has_options")}, nil
		}
		return &runtime.Result{Value: payload.New("no_options")}, nil
	})

	contractID := registry.NewID("test", "opts_check_contract")
	tc.registerContract(t, contractID, &apicontract.Definition{
		Methods: []apicontract.MethodDef{{Name: "check"}},
	})

	bindingID := registry.NewID("test", "opts_check_binding")
	tc.registerBinding(t, bindingID, &apicontract.Binding{
		Contracts: []apicontract.BoundContract{{
			Contract: contractID,
			Methods:  map[string]registry.ID{"check": funcID},
		}},
	})

	run := func(t *testing.T, script string) (string, runtime.Options) {
		t.Helper()
		capturedOptions = nil
		frameCtx, _ := ctxapi.OpenFrameContext(tc.ctx)
		proc := newLuaProcess(t, script)
		result, err := tc.scheduler.Execute(frameCtx, uniqueTestPID(), proc, "", nil)
		require.NoError(t, err, "scheduler error")
		require.NotNil(t, result, "result is nil")
		if result.Error != nil {
			t.Fatalf("result.Error: %v", result.Error)
		}
		if result.Value == nil {
			t.Fatal("result.Value is nil (Lua returned nothing)")
		}
		s, ok := result.Value.Data().(lua.LString)
		require.True(t, ok, "expected LString, got %T: %v", result.Value.Data(), result.Value.Data())
		return string(s), capturedOptions
	}

	hasRetry := func(t *testing.T, opts runtime.Options) {
		t.Helper()
		require.NotNil(t, opts, "options should not be nil")
		bag, ok := opts.(runtime.Bag)
		require.True(t, ok)
		_, exists := bag["retry"]
		assert.True(t, exists, "retry key should exist")
	}

	t.Run("wrapper with_options then open", func(t *testing.T) {
		val, opts := run(t, `
			local c, err = contract.get("test:opts_check_contract")
			if err then error("get: " .. tostring(err)) end
			local inst, err = c:with_options({retry = {max_attempts = 3}}):open("test:opts_check_binding")
			if err then error("open: " .. tostring(err)) end
			local result, err = inst:check()
			if err then error("check: " .. tostring(err)) end
			return result
		`)
		assert.Equal(t, "has_options", val)
		hasRetry(t, opts)
	})

	t.Run("contract.open with empty scope and options", func(t *testing.T) {
		val, opts := run(t, `
			local inst, err = contract.open("test:opts_check_binding", {}, {retry = {max_attempts = 3}})
			if err then error(tostring(err)) end
			local r, err = inst:check()
			if err then error("check: " .. tostring(err)) end
			return r
		`)
		assert.Equal(t, "has_options", val)
		hasRetry(t, opts)
	})

	t.Run("contract.open with nil scope and options", func(t *testing.T) {
		val, opts := run(t, `
			local inst, err = contract.open("test:opts_check_binding", nil, {retry = {max_attempts = 3}})
			if err then error(tostring(err)) end
			local r, err = inst:check()
			if err then error("check: " .. tostring(err)) end
			return r
		`)
		assert.Equal(t, "has_options", val)
		hasRetry(t, opts)
	})

	t.Run("contract.open with scope and options", func(t *testing.T) {
		val, opts := run(t, `
			local inst, err = contract.open("test:opts_check_binding", {key = "val"}, {retry = {max_attempts = 3}})
			if err then error(tostring(err)) end
			local r, err = inst:check()
			if err then error("check: " .. tostring(err)) end
			return r
		`)
		assert.Equal(t, "has_options", val)
		hasRetry(t, opts)
	})

	t.Run("contract.open without options", func(t *testing.T) {
		val, opts := run(t, `
			local inst, err = contract.open("test:opts_check_binding")
			if err then error(tostring(err)) end
			local r, err = inst:check()
			if err then error("check: " .. tostring(err)) end
			return r
		`)
		assert.Equal(t, "no_options", val)
		assert.Nil(t, opts)
	})

	t.Run("contract.open with scope only", func(t *testing.T) {
		val, opts := run(t, `
			local inst, err = contract.open("test:opts_check_binding", {key = "val"})
			if err then error(tostring(err)) end
			local r, err = inst:check()
			if err then error("check: " .. tostring(err)) end
			return r
		`)
		assert.Equal(t, "no_options", val)
		assert.Nil(t, opts)
	})

	t.Run("wrapper with_options chains with with_context", func(t *testing.T) {
		val, opts := run(t, `
			local c, err = contract.get("test:opts_check_contract")
			if err then error(tostring(err)) end
			local inst, err = c:with_context({k = "v"}):with_options({retry = {max_attempts = 3}}):open("test:opts_check_binding")
			if err then error(tostring(err)) end
			local r, err = inst:check()
			if err then error("check: " .. tostring(err)) end
			return r
		`)
		assert.Equal(t, "has_options", val)
		hasRetry(t, opts)
	})

	t.Run("wrapper with_options then with_context preserves options", func(t *testing.T) {
		val, opts := run(t, `
			local c, err = contract.get("test:opts_check_contract")
			if err then error(tostring(err)) end
			local inst, err = c:with_options({retry = {max_attempts = 3}}):with_context({k = "v"}):open("test:opts_check_binding")
			if err then error(tostring(err)) end
			local r, err = inst:check()
			if err then error("check: " .. tostring(err)) end
			return r
		`)
		assert.Equal(t, "has_options", val)
		hasRetry(t, opts)
	})

	t.Run("wrapper without options", func(t *testing.T) {
		val, opts := run(t, `
			local c, err = contract.get("test:opts_check_contract")
			if err then error(tostring(err)) end
			local inst, err = c:open("test:opts_check_binding")
			if err then error(tostring(err)) end
			local r, err = inst:check()
			if err then error("check: " .. tostring(err)) end
			return r
		`)
		assert.Equal(t, "no_options", val)
		assert.Nil(t, opts)
	})

	t.Run("wrapper with empty options", func(t *testing.T) {
		val, opts := run(t, `
			local c, err = contract.get("test:opts_check_contract")
			if err then error(tostring(err)) end
			local inst, err = c:with_options({}):open("test:opts_check_binding")
			if err then error(tostring(err)) end
			local r, err = inst:check()
			if err then error("check: " .. tostring(err)) end
			return r
		`)
		assert.Equal(t, "has_options", val)
		require.NotNil(t, opts)
	})
}

func TestIntegration_WithOptions_RetryOnFailure(t *testing.T) {
	tc := setupIntegrationTest(t, 4)
	defer tc.Close(t)

	var callCount atomic.Int64
	funcID := registry.NewID("test", "flaky_func")
	tc.registerFunction(t, funcID, func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		n := callCount.Add(1)
		if n < 3 {
			return nil, apierror.New(apierror.Unavailable, "temporary failure").WithRetryable(apierror.True)
		}
		return &runtime.Result{Value: payload.New("success_after_retries")}, nil
	})

	contractID := registry.NewID("test", "retry_contract")
	tc.registerContract(t, contractID, &apicontract.Definition{
		Methods: []apicontract.MethodDef{{Name: "call"}},
	})

	bindingID := registry.NewID("test", "retry_binding")
	tc.registerBinding(t, bindingID, &apicontract.Binding{
		Contracts: []apicontract.BoundContract{{
			Contract: contractID,
			Methods:  map[string]registry.ID{"call": funcID},
		}},
	})

	script := `
		local c, err = contract.get("test:retry_contract")
		if err then return nil, tostring(err) end

		local instance, err = c
			:with_options({retry = {max_attempts = 5, initial_delay = 1}})
			:open("test:retry_binding")
		if err then return nil, tostring(err) end

		local result, err = instance:call()
		if err then return nil, tostring(err) end
		return result
	`

	frameCtx, _ := ctxapi.OpenFrameContext(tc.ctx)
	proc := newLuaProcess(t, script)

	result, err := tc.scheduler.Execute(frameCtx, uniqueTestPID(), proc, "", nil)
	require.NoError(t, err)
	require.Nil(t, result.Error)
	require.NotNil(t, result.Value)
	assert.Equal(t, "success_after_retries", string(result.Value.Data().(lua.LString)))
	assert.Equal(t, int64(3), callCount.Load())
}

func TestIntegration_WithOptions_DirectOpen(t *testing.T) {
	tc := setupIntegrationTest(t, 4)
	defer tc.Close(t)

	var callCount atomic.Int64
	funcID := registry.NewID("test", "flaky_direct")
	tc.registerFunction(t, funcID, func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		n := callCount.Add(1)
		if n < 2 {
			return nil, apierror.New(apierror.Unavailable, "temporary").WithRetryable(apierror.True)
		}
		return &runtime.Result{Value: payload.New("ok")}, nil
	})

	contractID := registry.NewID("test", "direct_retry_contract")
	tc.registerContract(t, contractID, &apicontract.Definition{
		Methods: []apicontract.MethodDef{{Name: "run"}},
	})

	bindingID := registry.NewID("test", "direct_retry_binding")
	tc.registerBinding(t, bindingID, &apicontract.Binding{
		Contracts: []apicontract.BoundContract{{
			Contract: contractID,
			Methods:  map[string]registry.ID{"run": funcID},
		}},
	})

	script := `
		local instance, err = contract.open("test:direct_retry_binding", {}, {
			retry = {max_attempts = 3, initial_delay = 1}
		})
		if err then return nil, tostring(err) end

		local result, err = instance:run()
		if err then return nil, tostring(err) end
		return result
	`

	frameCtx, _ := ctxapi.OpenFrameContext(tc.ctx)
	proc := newLuaProcess(t, script)

	result, err := tc.scheduler.Execute(frameCtx, uniqueTestPID(), proc, "", nil)
	require.NoError(t, err)
	require.Nil(t, result.Error)
	require.NotNil(t, result.Value)
	assert.Equal(t, "ok", string(result.Value.Data().(lua.LString)))
	assert.Equal(t, int64(2), callCount.Load())
}

func TestIntegration_WithOptions_NonRetryableError(t *testing.T) {
	tc := setupIntegrationTest(t, 4)
	defer tc.Close(t)

	var callCount atomic.Int64
	funcID := registry.NewID("test", "permanent_fail")
	tc.registerFunction(t, funcID, func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		callCount.Add(1)
		return nil, apierror.New(apierror.PermissionDenied, "not allowed").WithRetryable(apierror.False)
	})

	contractID := registry.NewID("test", "noretry_contract")
	tc.registerContract(t, contractID, &apicontract.Definition{
		Methods: []apicontract.MethodDef{{Name: "forbidden"}},
	})

	bindingID := registry.NewID("test", "noretry_binding")
	tc.registerBinding(t, bindingID, &apicontract.Binding{
		Contracts: []apicontract.BoundContract{{
			Contract: contractID,
			Methods:  map[string]registry.ID{"forbidden": funcID},
		}},
	})

	script := `
		local c, err = contract.get("test:noretry_contract")
		if err then return nil, tostring(err) end

		local instance, err = c
			:with_options({retry = {max_attempts = 5, initial_delay = 1}})
			:open("test:noretry_binding")
		if err then return nil, tostring(err) end

		local result, err = instance:forbidden()
		if err then
			return err:kind()
		end
		return "should_not_reach"
	`

	frameCtx, _ := ctxapi.OpenFrameContext(tc.ctx)
	proc := newLuaProcess(t, script)

	result, err := tc.scheduler.Execute(frameCtx, uniqueTestPID(), proc, "", nil)
	require.NoError(t, err)
	require.Nil(t, result.Error)
	require.NotNil(t, result.Value)
	assert.Equal(t, "PermissionDenied", string(result.Value.Data().(lua.LString)))
	assert.Equal(t, int64(1), callCount.Load())
}

func TestIntegration_WithOptions_NoRetryWithoutOptions(t *testing.T) {
	tc := setupIntegrationTest(t, 4)
	defer tc.Close(t)

	var callCount atomic.Int64
	funcID := registry.NewID("test", "always_fail")
	tc.registerFunction(t, funcID, func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		callCount.Add(1)
		return nil, apierror.New(apierror.Unavailable, "fail").WithRetryable(apierror.True)
	})

	contractID := registry.NewID("test", "noopts_contract")
	tc.registerContract(t, contractID, &apicontract.Definition{
		Methods: []apicontract.MethodDef{{Name: "fail"}},
	})

	bindingID := registry.NewID("test", "noopts_binding")
	tc.registerBinding(t, bindingID, &apicontract.Binding{
		Contracts: []apicontract.BoundContract{{
			Contract: contractID,
			Methods:  map[string]registry.ID{"fail": funcID},
		}},
	})

	script := `
		local instance, err = contract.open("test:noopts_binding")
		if err then return nil, tostring(err) end

		local result, err = instance:fail()
		if err then
			return "failed:" .. err:kind()
		end
		return "should_not_reach"
	`

	frameCtx, _ := ctxapi.OpenFrameContext(tc.ctx)
	proc := newLuaProcess(t, script)

	result, err := tc.scheduler.Execute(frameCtx, uniqueTestPID(), proc, "", nil)
	require.NoError(t, err)
	require.Nil(t, result.Error)
	require.NotNil(t, result.Value)
	assert.Equal(t, "failed:Unavailable", string(result.Value.Data().(lua.LString)))
	assert.Equal(t, int64(1), callCount.Load())
}
