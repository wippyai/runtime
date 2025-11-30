package funcs

import (
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/attrs"
	contextapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua2api "github.com/wippyai/runtime/api/runtime/lua2"
	secapi "github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/security"
	luaconv "github.com/wippyai/runtime/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
)

var (
	moduleTable       *lua.LTable
	registration      *lua2api.Registration
	futureMetatable   *lua.LTable
	executorMetatable *lua.LTable
	initOnce          sync.Once
)

const (
	futureTypeName   = "funcs.Future"
	executorTypeName = "funcs.Executor"
)

// Module is the singleton funcs module instance.
var Module = &funcsModule{}

type funcsModule struct{}

func (m *funcsModule) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "funcs",
		Description: "Function calls and async execution",
		Class:       []string{luaapi.ClassWorkflow, luaapi.ClassNondeterministic},
	}
}

func (m *funcsModule) Register(l *lua.LState) *lua2api.Registration {
	initOnce.Do(func() {
		moduleTable = createModuleTable()
		futureMetatable = value.RegisterTypeMethods(nil, futureTypeName, nil, futureMethods)
		executorMetatable = value.RegisterTypeMethods(nil, executorTypeName, nil, executorMethods)
		registration = &lua2api.Registration{
			Table:      moduleTable,
			YieldTypes: nil,
		}
	})

	l.SetGlobal("__funcs_async_start_yield", lua.LGoFunc(asyncStartYield))
	l.SetGlobal("__funcs_future_new", lua.LGoFunc(futureNew))

	return registration
}

func (m *funcsModule) Loader(l *lua.LState) int {
	reg := m.Register(l)
	l.Push(reg.Table)
	return 1
}

type Future struct {
	ID uint64
}

// Executor represents a function executor with context values
type Executor struct {
	values     []contextapi.Pair
	actor      secapi.Actor
	hasActor   bool
	scope      secapi.Scope
	hasScope   bool
	options    attrs.Bag
	hasOptions bool
}

var executorMethods = map[string]lua.LGFunction{
	"with_context": executorWithContext,
	"with_actor":   executorWithActor,
	"with_scope":   executorWithScope,
	"with_options": executorWithOptions,
	"call":         executorCall,
	"async":        executorAsync,
}

func checkExecutor(l *lua.LState, idx int) *Executor {
	ud := l.CheckUserData(idx)
	if v, ok := ud.Value.(*Executor); ok {
		return v
	}
	l.ArgError(idx, "Executor expected")
	return nil
}

func executorNew(l *lua.LState) int {
	exec := &Executor{}
	ud := l.NewUserData()
	ud.Value = exec
	ud.Metatable = executorMetatable
	l.Push(ud)
	return 1
}

func executorWithContext(l *lua.LState) int {
	exec := checkExecutor(l, 1)
	if exec == nil {
		return 0
	}

	ctx := l.Context()
	if !security.IsAllowed(ctx, "funcs.context", "context", nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("not allowed to call functions with custom context"))
		return 2
	}

	ctxTable := l.CheckTable(2)

	newValues := make([]contextapi.Pair, len(exec.values))
	copy(newValues, exec.values)

	ctxTable.ForEach(func(k, v lua.LValue) {
		if key, ok := k.(lua.LString); ok {
			newValues = append(newValues, contextapi.Pair{
				Key:   string(key),
				Value: value.ToGoAny(v),
			})
		}
	})

	newExec := &Executor{
		values:     newValues,
		actor:      exec.actor,
		hasActor:   exec.hasActor,
		scope:      exec.scope,
		hasScope:   exec.hasScope,
		options:    exec.options,
		hasOptions: exec.hasOptions,
	}

	ud := l.NewUserData()
	ud.Value = newExec
	ud.Metatable = executorMetatable
	l.Push(ud)
	return 1
}

func executorWithActor(l *lua.LState) int {
	exec := checkExecutor(l, 1)
	if exec == nil {
		return 0
	}

	ctx := l.Context()
	if !security.IsAllowed(ctx, "funcs.security", "security", nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("not allowed to call functions with custom security context"))
		return 2
	}

	if l.Get(2).Type() == lua.LTNil {
		l.ArgError(2, "actor cannot be nil")
		return 0
	}

	actorUD := l.CheckUserData(2)
	actor, ok := actorUD.Value.(secapi.Actor)
	if !ok {
		l.ArgError(2, "Actor expected")
		return 0
	}

	newValues := make([]contextapi.Pair, len(exec.values))
	copy(newValues, exec.values)

	newExec := &Executor{
		values:     newValues,
		actor:      actor,
		hasActor:   true,
		scope:      exec.scope,
		hasScope:   exec.hasScope,
		options:    exec.options,
		hasOptions: exec.hasOptions,
	}

	ud := l.NewUserData()
	ud.Value = newExec
	ud.Metatable = executorMetatable
	l.Push(ud)
	return 1
}

func executorWithScope(l *lua.LState) int {
	exec := checkExecutor(l, 1)
	if exec == nil {
		return 0
	}

	ctx := l.Context()
	if !security.IsAllowed(ctx, "funcs.security", "security", nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("not allowed to call functions with custom security context"))
		return 2
	}

	if l.Get(2).Type() == lua.LTNil {
		l.ArgError(2, "scope cannot be nil")
		return 0
	}

	scopeUD := l.CheckUserData(2)
	scope, ok := scopeUD.Value.(secapi.Scope)
	if !ok {
		l.ArgError(2, "Scope expected")
		return 0
	}

	newValues := make([]contextapi.Pair, len(exec.values))
	copy(newValues, exec.values)

	newExec := &Executor{
		values:     newValues,
		actor:      exec.actor,
		hasActor:   exec.hasActor,
		scope:      scope,
		hasScope:   true,
		options:    exec.options,
		hasOptions: exec.hasOptions,
	}

	ud := l.NewUserData()
	ud.Value = newExec
	ud.Metatable = executorMetatable
	l.Push(ud)
	return 1
}

func executorWithOptions(l *lua.LState) int {
	exec := checkExecutor(l, 1)
	if exec == nil {
		return 0
	}

	optionsTable := l.CheckTable(2)
	options := attrs.NewBag()
	optionsTable.ForEach(func(k, v lua.LValue) {
		if key, ok := k.(lua.LString); ok {
			options.Set(string(key), value.ToGoAny(v))
		}
	})

	newValues := make([]contextapi.Pair, len(exec.values))
	copy(newValues, exec.values)

	newExec := &Executor{
		values:     newValues,
		actor:      exec.actor,
		hasActor:   exec.hasActor,
		scope:      exec.scope,
		hasScope:   exec.hasScope,
		options:    options,
		hasOptions: true,
	}

	ud := l.NewUserData()
	ud.Value = newExec
	ud.Metatable = executorMetatable
	l.Push(ud)
	return 1
}

func executorCall(l *lua.LState) int {
	exec := checkExecutor(l, 1)
	if exec == nil {
		return 0
	}

	target := l.CheckString(2)
	if target == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("function ID required"))
		return 2
	}

	regID := registry.ParseID(target)
	if regID.NS == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("namespace required in function ID"))
		return 2
	}
	if regID.Name == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("name required in function ID"))
		return 2
	}

	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context"))
		return 2
	}

	if !security.IsAllowed(ctx, "funcs.call", target, nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("not allowed: " + target))
		return 2
	}

	var payloads []payload.Payload
	for i := 3; i <= l.GetTop(); i++ {
		val := l.Get(i)
		payloads = append(payloads, luaconv.ExportPayload(val))
	}

	yield := AcquireCallYield()
	yield.Task = runtime.Task{
		ID:       regID,
		Payloads: payloads,
	}

	// Add context pairs
	if exec.hasActor {
		yield.Task.Context = append(yield.Task.Context, secapi.ActorPair(exec.actor))
	}
	if exec.hasScope {
		yield.Task.Context = append(yield.Task.Context, secapi.ScopePair(exec.scope))
	}
	yield.Task.Context = append(yield.Task.Context, exec.values...)

	if exec.hasOptions {
		yield.Task.Options = exec.options
	}

	l.Push(yield)
	return -1
}

func executorAsync(l *lua.LState) int {
	exec := checkExecutor(l, 1)
	if exec == nil {
		return 0
	}

	target := l.CheckString(2)
	if target == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("function ID required"))
		return 2
	}

	regID := registry.ParseID(target)
	if regID.NS == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("namespace required in function ID"))
		return 2
	}
	if regID.Name == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("name required in function ID"))
		return 2
	}

	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context"))
		return 2
	}

	if !security.IsAllowed(ctx, "funcs.call", target, nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("not allowed: " + target))
		return 2
	}

	var payloads []payload.Payload
	for i := 3; i <= l.GetTop(); i++ {
		val := l.Get(i)
		payloads = append(payloads, luaconv.ExportPayload(val))
	}

	yield := AcquireAsyncStartYield()
	yield.Task = runtime.Task{
		ID:       regID,
		Payloads: payloads,
	}

	// Add context pairs
	if exec.hasActor {
		yield.Task.Context = append(yield.Task.Context, secapi.ActorPair(exec.actor))
	}
	if exec.hasScope {
		yield.Task.Context = append(yield.Task.Context, secapi.ScopePair(exec.scope))
	}
	yield.Task.Context = append(yield.Task.Context, exec.values...)

	if exec.hasOptions {
		yield.Task.Options = exec.options
	}

	l.Push(yield)
	return 1
}

// Bind is deprecated. Use lua2api.LoadModule(l, Module) instead.
func Bind(l *lua.LState) {
	lua2api.LoadModule(l, Module)
}

func createModuleTable() *lua.LTable {
	mod := lua.CreateTable(0, 2)
	mod.RawSetString("call", lua.LGoFunc(call))
	mod.RawSetString("new", lua.LGoFunc(executorNew))
	mod.Immutable = true
	return mod
}

var futureMethods = map[string]lua.LGFunction{
	"await":  futureAwait,
	"cancel": futureCancel,
}

func checkFuture(l *lua.LState, idx int) *Future {
	ud := l.CheckUserData(idx)
	if v, ok := ud.Value.(*Future); ok {
		return v
	}
	l.ArgError(idx, "Future expected")
	return nil
}

func futureAwait(l *lua.LState) int {
	future := checkFuture(l, 1)
	yield := AcquireAsyncAwaitYield()
	yield.CallID = future.ID
	l.Push(yield)
	return -1
}

func futureCancel(l *lua.LState) int {
	future := checkFuture(l, 1)
	yield := AcquireAsyncCancelYield()
	yield.CallID = future.ID
	l.Push(yield)
	return -1
}

func asyncStartYield(l *lua.LState) int {
	target := l.CheckString(1)
	if target == "" {
		l.ArgError(1, "function ID required")
		return 0
	}

	regID := registry.ParseID(target)
	if regID.NS == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("namespace required in function ID"))
		return 2
	}
	if regID.Name == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("name required in function ID"))
		return 2
	}

	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context"))
		return 2
	}

	if !security.IsAllowed(ctx, "funcs.call", target, nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("not allowed: " + target))
		return 2
	}

	var payloads []payload.Payload
	for i := 2; i <= l.GetTop(); i++ {
		val := l.Get(i)
		payloads = append(payloads, luaconv.ExportPayload(val))
	}

	yield := AcquireAsyncStartYield()
	yield.Task = runtime.Task{
		ID:       regID,
		Payloads: payloads,
	}

	l.Push(yield)
	return 1
}

func futureNew(l *lua.LState) int {
	id := uint64(l.CheckNumber(1))
	future := &Future{ID: id}
	ud := l.NewUserData()
	ud.Value = future
	ud.Metatable = futureMetatable
	l.Push(ud)
	return 1
}

func call(l *lua.LState) int {
	target := l.CheckString(1)
	if target == "" {
		l.ArgError(1, "function ID required")
		return 0
	}

	regID := registry.ParseID(target)
	if regID.NS == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("namespace required in function ID"))
		return 2
	}
	if regID.Name == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("name required in function ID"))
		return 2
	}

	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context"))
		return 2
	}

	if !security.IsAllowed(ctx, "funcs.call", target, nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("not allowed: " + target))
		return 2
	}

	var payloads []payload.Payload
	for i := 2; i <= l.GetTop(); i++ {
		val := l.Get(i)
		payloads = append(payloads, luaconv.ExportPayload(val))
	}

	yield := AcquireCallYield()
	yield.Task = runtime.Task{
		ID:       regID,
		Payloads: payloads,
	}

	l.Push(yield)
	return -1
}

func BindAsync(l *lua.LState) {
	err := l.DoString(`
		function funcs.async(target, ...)
			local yield = __funcs_async_start_yield(target, ...)
			if yield == nil then
				return nil, "failed to create async call"
			end
			local resp = coroutine.yield(yield)
			if resp.Error then
				return nil, tostring(resp.Error)
			end
			return __funcs_future_new(resp.CallID)
		end
	`)
	if err != nil {
		panic(fmt.Sprintf("failed to load funcs.async: %v", err))
	}
}
