package funcs

import (
	"sync"

	"github.com/wippyai/runtime/api/attrs"
	contextapi "github.com/wippyai/runtime/api/context"
	funcapi "github.com/wippyai/runtime/api/dispatcher/func"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	secapi "github.com/wippyai/runtime/api/security"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/security"
	lua "github.com/yuin/gopher-lua"
)

const (
	futureTypeName   = "funcs.Future"
	executorTypeName = "funcs.Executor"
)

var (
	moduleTable       *lua.LTable
	registration      *luaapi.Registration
	futureMetatable   *lua.LTable
	executorMetatable *lua.LTable
	initOnce          sync.Once
)

// Module is the funcs module definition.
var Module = &luaapi.ModuleDef{
	Name:        "funcs",
	Description: "Function calls and async execution",
	Class:       []string{luaapi.ClassWorkflow, luaapi.ClassNondeterministic},
	Build:       buildModule,
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
	initOnce.Do(doInit)

	return moduleTable, []luaapi.YieldType{
		{Sample: &CallYield{}, CmdID: funcapi.CmdCall},
		{Sample: &AsyncStartYield{}, CmdID: funcapi.CmdAsyncStart},
		{Sample: &AsyncAwaitYield{}, CmdID: funcapi.CmdAsyncAwait},
		{Sample: &AsyncCancelYield{}, CmdID: funcapi.CmdAsyncCancel},
	}
}

func doInit() {
	futureMetatable = value.RegisterTypeMethods(nil, futureTypeName, nil, futureMethods)
	executorMetatable = value.RegisterTypeMethods(nil, executorTypeName, nil, executorMethods)

	mod := lua.CreateTable(0, 3)
	mod.RawSetString("call", lua.LGoFunc(call))
	mod.RawSetString("async", lua.LGoFunc(async))
	mod.RawSetString("new", lua.LGoFunc(executorNew))
	mod.Immutable = true
	moduleTable = mod
}

// Future represents an async call that can be awaited.
type Future struct {
	ID uint64
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

// Executor represents a function executor with context values.
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
		err := lua.NewLuaError(l, "not allowed to call functions with custom context").
			WithKind(lua.KindPermissionDenied).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
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
		err := lua.NewLuaError(l, "not allowed to call functions with custom security context").
			WithKind(lua.KindPermissionDenied).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
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
		err := lua.NewLuaError(l, "not allowed to call functions with custom security context").
			WithKind(lua.KindPermissionDenied).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
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
		err := lua.NewLuaError(l, "function ID required").
			WithKind(lua.KindInvalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	regID := registry.ParseID(target)
	if regID.NS == "" {
		err := lua.NewLuaError(l, "namespace required in function ID").
			WithKind(lua.KindInvalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}
	if regID.Name == "" {
		err := lua.NewLuaError(l, "name required in function ID").
			WithKind(lua.KindInvalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	ctx := l.Context()
	if ctx == nil {
		err := lua.NewLuaError(l, "no context").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	if !security.IsAllowed(ctx, "funcs.call", target, nil) {
		err := lua.NewLuaError(l, "not allowed: "+target).
			WithKind(lua.KindPermissionDenied).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
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
		err := lua.NewLuaError(l, "function ID required").
			WithKind(lua.KindInvalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	regID := registry.ParseID(target)
	if regID.NS == "" {
		err := lua.NewLuaError(l, "namespace required in function ID").
			WithKind(lua.KindInvalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}
	if regID.Name == "" {
		err := lua.NewLuaError(l, "name required in function ID").
			WithKind(lua.KindInvalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	ctx := l.Context()
	if ctx == nil {
		err := lua.NewLuaError(l, "no context").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	if !security.IsAllowed(ctx, "funcs.call", target, nil) {
		err := lua.NewLuaError(l, "not allowed: "+target).
			WithKind(lua.KindPermissionDenied).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
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

// call is the simple funcs.call(target, ...) function.
func call(l *lua.LState) int {
	target := l.CheckString(1)
	if target == "" {
		err := lua.NewLuaError(l, "function ID required").
			WithKind(lua.KindInvalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	regID := registry.ParseID(target)
	if regID.NS == "" {
		err := lua.NewLuaError(l, "namespace required in function ID").
			WithKind(lua.KindInvalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}
	if regID.Name == "" {
		err := lua.NewLuaError(l, "name required in function ID").
			WithKind(lua.KindInvalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	ctx := l.Context()
	if ctx == nil {
		err := lua.NewLuaError(l, "no context").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	if !security.IsAllowed(ctx, "funcs.call", target, nil) {
		err := lua.NewLuaError(l, "not allowed: "+target).
			WithKind(lua.KindPermissionDenied).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
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

// async is the simple funcs.async(target, ...) function.
func async(l *lua.LState) int {
	target := l.CheckString(1)
	if target == "" {
		err := lua.NewLuaError(l, "function ID required").
			WithKind(lua.KindInvalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	regID := registry.ParseID(target)
	if regID.NS == "" {
		err := lua.NewLuaError(l, "namespace required in function ID").
			WithKind(lua.KindInvalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}
	if regID.Name == "" {
		err := lua.NewLuaError(l, "name required in function ID").
			WithKind(lua.KindInvalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	ctx := l.Context()
	if ctx == nil {
		err := lua.NewLuaError(l, "no context").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	if !security.IsAllowed(ctx, "funcs.call", target, nil) {
		err := lua.NewLuaError(l, "not allowed: "+target).
			WithKind(lua.KindPermissionDenied).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
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
	return -1
}

// createFuture creates a Future userdata from a call ID.
func createFuture(l *lua.LState, id uint64) *lua.LUserData {
	future := &Future{ID: id}
	ud := l.NewUserData()
	ud.Value = future
	ud.Metatable = futureMetatable
	return ud
}
