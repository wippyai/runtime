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
	ctxapi "github.com/wippyai/runtime/api/context"
	apicontract "github.com/wippyai/runtime/api/contract"
	"github.com/wippyai/runtime/api/dispatcher"
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
	sysrelay "github.com/wippyai/runtime/system/relay"
	"github.com/wippyai/runtime/system/scheduler"
	"github.com/wippyai/runtime/system/scheduler/actor"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

type testScheduler struct {
	*actor.Scheduler
	contractDisp *syscontract.Dispatcher
	mu           sync.Mutex
	pending      map[string]chan *runtime.Result
}

func (ts *testScheduler) Stop() {
	ts.Scheduler.Stop(context.Background())
}

func (ts *testScheduler) OnStart(_ context.Context, _ pid.PID, _ process.Process) {}

func (ts *testScheduler) OnComplete(_ context.Context, p pid.PID, result *runtime.Result) {
	ts.mu.Lock()
	ch, ok := ts.pending[p.UniqID]
	if ok {
		delete(ts.pending, p.UniqID)
	}
	ts.mu.Unlock()
	if ok {
		ch <- result
	}
}

func (ts *testScheduler) Execute(ctx context.Context, p pid.PID, proc process.Process, method string, input payload.Payloads) (*runtime.Result, error) {
	resultCh := make(chan *runtime.Result, 1)

	ts.mu.Lock()
	ts.pending[p.UniqID] = resultCh
	ts.mu.Unlock()

	_, err := ts.Scheduler.Submit(ctx, p, proc, method, input)
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
	logger := zap.NewNop()
	bus := eventbus.NewBus()

	node := sysrelay.NewNode("test")

	contractRegistry := syscontract.NewContractRegistry(bus, logger)
	functionRegistry := sysfunction.NewFunctionRegistry(bus, logger)
	instantiator := syscontract.NewContractInstantiator(contractRegistry, functionRegistry)

	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	ctx = relay.WithNode(ctx, node)
	ctx = apicontract.WithContracts(ctx, contractRegistry, instantiator)
	ctx = function.WithRegistry(ctx, functionRegistry)

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
		contractDisp: contractDisp,
	}

	opts := []actor.Option{
		actor.WithWorkers(numWorkers),
		actor.WithLifecycle(ts),
	}
	ts.Scheduler = actor.NewScheduler(reg, opts...)
	ts.Start()

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
		if evt.Kind == function.Accept && evt.Path == funcID.String() {
			wg.Done()
		}
	})
	require.NoError(t, err)
	defer sub.Close()

	wg.Add(1)
	tc.bus.Send(tc.ctx, event.Event{
		System: function.System,
		Kind:   function.Register,
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
		if evt.Kind == apicontract.Accept && evt.Path == contractID.String() {
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
		if evt.Kind == apicontract.Accept && evt.Path == bindingID.String() {
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

func bindContractModule(l *lua.LState) {
	contractmod.Module.Load(l)
}

func newLuaProcess(script string) *engine.Process {
	proto, _ := lua.CompileString(script, "test.lua")
	return engine.NewProcess(
		engine.WithProto(proto),
		engine.WithModuleBinder(func(l *lua.LState) { engine.ChannelModule.Load(l) }),
		engine.WithModuleBinder(bindContractModule),
	)
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
	proc := newLuaProcess(script)

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
	proc := newLuaProcess(script)

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
	proc := newLuaProcess(script)

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
	proc := newLuaProcess(script)

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
	proc := newLuaProcess(script)

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
	proc := newLuaProcess(script)

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
	proc := newLuaProcess(script)

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
			proc := newLuaProcess(script)

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
