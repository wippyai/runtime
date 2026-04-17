// SPDX-License-Identifier: MPL-2.0

package funcs

import (
	"github.com/google/uuid"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	contextapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/function"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	secapi "github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/lua/engine"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/modules/future"
	"github.com/wippyai/runtime/runtime/security"
)

const executorTypeName = "funcs.Executor"

var (
	moduleTable *lua.LTable
	yieldTypes  []luaapi.YieldType
)

func init() {
	value.RegisterTypeMethods(nil, executorTypeName, nil, map[string]lua.LGoFunc{
		"with_context": executorWithContext,
		"with_actor":   executorWithActor,
		"with_scope":   executorWithScope,
		"with_options": executorWithOptions,
		"call":         executorCall,
		"async":        executorAsync,
	})

	// Set cancel function for Future type
	future.CancelFunc = futureCancelImpl

	moduleTable = lua.CreateTable(0, 3)
	moduleTable.RawSetString("new", lua.LGoFunc(executorNew))
	moduleTable.RawSetString("call", lua.LGoFunc(call))
	moduleTable.RawSetString("async", lua.LGoFunc(async))
	moduleTable.Immutable = true

	yieldTypes = []luaapi.YieldType{
		{Sample: &CallYield{}, CmdID: function.Call},
		{Sample: &AsyncStartYield{}, CmdID: function.AsyncStart},
		{Sample: &AsyncCancelYield{}, CmdID: function.AsyncCancel},
	}
}

// Module is the funcs module definition.
var Module = &luaapi.ModuleDef{
	Name:        "funcs",
	Description: "Function calls and async execution",
	Class:       []string{luaapi.ClassWorkflow, luaapi.ClassNondeterministic},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		return moduleTable, yieldTypes
	},
	Types: ModuleTypes,
}

// Executor represents a function executor with context values.
type Executor struct {
	scope      secapi.Scope
	values     contextapi.Values
	options    attrs.Bag
	actor      secapi.Actor
	hasActor   bool
	hasScope   bool
	hasOptions bool
}

func executorNew(l *lua.LState) int {
	exec := &Executor{}
	value.PushTypedUserData(l, exec, executorTypeName)
	return 1
}

func executorWithContext(l *lua.LState) int {
	ud := l.CheckUserData(1)
	exec, ok := ud.Value.(*Executor)
	if !ok {
		l.ArgError(1, "Executor expected")
		return 0
	}

	ctx := l.Context()
	if !security.IsAllowed(ctx, "funcs.context", "context", nil) {
		err := lua.NewLuaError(l, "not allowed to call functions with custom context").
			WithKind(lua.PermissionDenied).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	ctxTable := l.CheckTable(2)

	// Create new values bag, copying existing values
	newValues := contextapi.NewValues()
	if exec.values != nil {
		exec.values.Iterate(func(key string, val any) {
			newValues.Set(key, val)
		})
	}

	// Add new values from table
	ctxTable.ForEach(func(k, v lua.LValue) {
		if key, ok := k.(lua.LString); ok {
			newValues.Set(string(key), value.ToGoAny(v))
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

	value.PushTypedUserData(l, newExec, executorTypeName)
	return 1
}

func executorWithActor(l *lua.LState) int {
	ud := l.CheckUserData(1)
	exec, ok := ud.Value.(*Executor)
	if !ok {
		l.ArgError(1, "Executor expected")
		return 0
	}

	ctx := l.Context()
	if !security.IsAllowed(ctx, "funcs.security", "security", nil) {
		err := lua.NewLuaError(l, "not allowed to call functions with custom security context").
			WithKind(lua.PermissionDenied).
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

	newExec := &Executor{
		values:     exec.values,
		actor:      actor,
		hasActor:   true,
		scope:      exec.scope,
		hasScope:   exec.hasScope,
		options:    exec.options,
		hasOptions: exec.hasOptions,
	}

	value.PushTypedUserData(l, newExec, executorTypeName)
	return 1
}

func executorWithScope(l *lua.LState) int {
	ud := l.CheckUserData(1)
	exec, ok := ud.Value.(*Executor)
	if !ok {
		l.ArgError(1, "Executor expected")
		return 0
	}

	ctx := l.Context()
	if !security.IsAllowed(ctx, "funcs.security", "security", nil) {
		err := lua.NewLuaError(l, "not allowed to call functions with custom security context").
			WithKind(lua.PermissionDenied).
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

	newExec := &Executor{
		values:     exec.values,
		actor:      exec.actor,
		hasActor:   exec.hasActor,
		scope:      scope,
		hasScope:   true,
		options:    exec.options,
		hasOptions: exec.hasOptions,
	}

	value.PushTypedUserData(l, newExec, executorTypeName)
	return 1
}

func executorWithOptions(l *lua.LState) int {
	ud := l.CheckUserData(1)
	exec, ok := ud.Value.(*Executor)
	if !ok {
		l.ArgError(1, "Executor expected")
		return 0
	}

	optionsTable := l.CheckTable(2)
	options := attrs.NewBag()
	optionsTable.ForEach(func(k, v lua.LValue) {
		if key, ok := k.(lua.LString); ok {
			options.Set(string(key), value.ToGoAny(v))
		}
	})

	if net := options.GetString(netapi.OptionKeyNetwork, ""); net != "" {
		if !security.IsAllowed(l.Context(), "network.select", net, nil) {
			luaErr := lua.NewLuaError(l, "not allowed: network "+net).
				WithKind(lua.PermissionDenied).
				WithRetryable(false)
			l.Push(lua.LNil)
			l.Push(luaErr)
			return 2
		}
	}

	newExec := &Executor{
		values:     exec.values,
		actor:      exec.actor,
		hasActor:   exec.hasActor,
		scope:      exec.scope,
		hasScope:   exec.hasScope,
		options:    options,
		hasOptions: true,
	}

	value.PushTypedUserData(l, newExec, executorTypeName)
	return 1
}

func executorCall(l *lua.LState) int {
	ud := l.CheckUserData(1)
	exec, ok := ud.Value.(*Executor)
	if !ok {
		l.ArgError(1, "Executor expected")
		return 0
	}

	target := l.CheckString(2)
	regID, retCount := validateTarget(l, target)
	if retCount != 0 {
		return retCount
	}

	ctx := l.Context()
	if !security.IsAllowed(ctx, "funcs.call", target, nil) {
		luaErr := lua.NewLuaError(l, "not allowed: "+target).
			WithKind(lua.PermissionDenied).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	var payloads []payload.Payload
	for i := 3; i <= l.GetTop(); i++ {
		payloads = append(payloads, luaconv.ExportPayload(l.Get(i)))
	}

	yield := AcquireCallYield()
	yield.Task = runtime.Task{
		ID:       regID,
		Payloads: payloads,
	}

	// Build context with merged values (frame values + explicit values in one ValuesPair)
	yield.Task.Context = buildMergedContextPairs(l, exec.values)

	// Overlay with explicit actor/scope if set (these take precedence over inherited)
	if exec.hasActor {
		yield.Task.Context = append(yield.Task.Context, secapi.ActorPair(exec.actor))
	}
	if exec.hasScope {
		yield.Task.Context = append(yield.Task.Context, secapi.ScopePair(exec.scope))
	}

	if exec.hasOptions {
		yield.Task.Options = exec.options
	}

	l.Push(yield)
	return -1
}

func executorAsync(l *lua.LState) int {
	ud := l.CheckUserData(1)
	exec, ok := ud.Value.(*Executor)
	if !ok {
		l.ArgError(1, "Executor expected")
		return 0
	}

	target := l.CheckString(2)
	regID, retCount := validateTarget(l, target)
	if retCount != 0 {
		return retCount
	}

	ctx := l.Context()
	if !security.IsAllowed(ctx, "funcs.call", target, nil) {
		luaErr := lua.NewLuaError(l, "not allowed: "+target).
			WithKind(lua.PermissionDenied).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	var payloads []payload.Payload
	for i := 3; i <= l.GetTop(); i++ {
		payloads = append(payloads, luaconv.ExportPayload(l.Get(i)))
	}

	yield, retCount := setupAsyncYieldWithValues(l, regID, payloads, exec.values)
	if retCount != 0 {
		return retCount
	}

	// Overlay with explicit actor/scope if set (these take precedence over inherited)
	if exec.hasActor {
		yield.Task.Context = append(yield.Task.Context, secapi.ActorPair(exec.actor))
	}
	if exec.hasScope {
		yield.Task.Context = append(yield.Task.Context, secapi.ScopePair(exec.scope))
	}

	if exec.hasOptions {
		yield.Task.Options = exec.options
	}

	l.Push(yield)
	return -1
}

// call is a shortcut for funcs.call(target, ...).
func call(l *lua.LState) int {
	target := l.CheckString(1)
	regID, retCount := validateTarget(l, target)
	if retCount != 0 {
		return retCount
	}

	ctx := l.Context()
	if !security.IsAllowed(ctx, "funcs.call", target, nil) {
		luaErr := lua.NewLuaError(l, "not allowed: "+target).
			WithKind(lua.PermissionDenied).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	var payloads []payload.Payload
	for i := 2; i <= l.GetTop(); i++ {
		payloads = append(payloads, luaconv.ExportPayload(l.Get(i)))
	}

	yield := AcquireCallYield()
	yield.Task = runtime.Task{
		ID:       regID,
		Payloads: payloads,
		Context:  buildContextPairs(l),
	}

	l.Push(yield)
	return -1
}

// async is a shortcut for funcs.async(target, ...).
func async(l *lua.LState) int {
	target := l.CheckString(1)
	regID, retCount := validateTarget(l, target)
	if retCount != 0 {
		return retCount
	}

	ctx := l.Context()
	if !security.IsAllowed(ctx, "funcs.call", target, nil) {
		luaErr := lua.NewLuaError(l, "not allowed: "+target).
			WithKind(lua.PermissionDenied).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	var payloads []payload.Payload
	for i := 2; i <= l.GetTop(); i++ {
		payloads = append(payloads, luaconv.ExportPayload(l.Get(i)))
	}

	yield, retCount := setupAsyncYield(l, regID, payloads)
	if retCount != 0 {
		return retCount
	}

	l.Push(yield)
	return -1
}

// setupAsyncYield creates the async yield with topic, channel, and future.
func setupAsyncYield(l *lua.LState, regID registry.ID, payloads []payload.Payload) (*AsyncStartYield, int) {
	return setupAsyncYieldWithValues(l, regID, payloads, nil)
}

// setupAsyncYieldWithValues creates the async yield with merged context values.
func setupAsyncYieldWithValues(l *lua.LState, regID registry.ID, payloads []payload.Payload, explicitValues contextapi.Values) (*AsyncStartYield, int) {
	proc := engine.GetProcess(l)
	if proc == nil {
		luaErr := lua.NewLuaError(l, "no process context").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return nil, 2
	}

	topic := "@future:" + uuid.New().String()
	ch, subErr := proc.Subscribe(topic, 1)
	if subErr != nil {
		luaErr := lua.WrapErrorWithLua(l, subErr, "subscribe failed").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return nil, 2
	}

	f := future.New(topic, ch)
	proc.SetTopicHandler(topic, f.CreateHandler())

	yield := AcquireAsyncStartYield()
	yield.Task = runtime.Task{
		ID:       regID,
		Payloads: payloads,
		Context:  buildMergedContextPairs(l, explicitValues),
	}
	yield.Topic = topic
	yield.Future = f

	return yield, 0
}

func validateTarget(l *lua.LState, target string) (registry.ID, int) {
	if target == "" {
		err := lua.NewLuaError(l, "function ID required").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return registry.ID{}, 2
	}

	regID := registry.ParseID(target)
	if regID.NS == "" {
		err := lua.NewLuaError(l, "namespace required in function ID").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return registry.ID{}, 2
	}
	if regID.Name == "" {
		err := lua.NewLuaError(l, "name required in function ID").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return registry.ID{}, 2
	}

	return regID, 0
}

// futureCancelImpl yields an async cancel command for the future's topic.
func futureCancelImpl(l *lua.LState) int {
	ud := l.CheckUserData(1)
	f, ok := ud.Value.(*future.Future)
	if !ok {
		l.ArgError(1, "Future expected")
		return 0
	}

	f.MarkCanceled()
	yield := AcquireAsyncCancelYield()
	yield.Topic = f.Topic
	l.Push(yield)
	return -1
}

// buildContextPairs extracts context pairs from the Lua state's frame context
// to be passed to called functions for context inheritance.
func buildContextPairs(l *lua.LState) []contextapi.Pair {
	ctx := l.Context()
	if ctx == nil {
		return nil
	}

	var pairs []contextapi.Pair
	if actor, ok := secapi.GetActor(ctx); ok {
		pairs = append(pairs, secapi.ActorPair(actor))
	}
	if scope, ok := secapi.GetScope(ctx); ok {
		pairs = append(pairs, secapi.ScopePair(scope))
	}
	if values := contextapi.GetValues(ctx); values != nil && values.Len() > 0 {
		pairs = append(pairs, contextapi.ValuesPair(values))
	}
	return pairs
}

// buildMergedContextPairs extracts context pairs from the Lua state's frame context
// and merges them with explicit values, ensuring only ONE ValuesPair with merged values.
// This prevents the second ValuesPair from overwriting the first when context pairs are applied.
func buildMergedContextPairs(l *lua.LState, explicitValues contextapi.Values) []contextapi.Pair {
	ctx := l.Context()
	if ctx == nil && (explicitValues == nil || explicitValues.Len() == 0) {
		return nil
	}

	var pairs []contextapi.Pair

	// Add actor and scope from frame context
	if ctx != nil {
		if actor, ok := secapi.GetActor(ctx); ok {
			pairs = append(pairs, secapi.ActorPair(actor))
		}
		if scope, ok := secapi.GetScope(ctx); ok {
			pairs = append(pairs, secapi.ScopePair(scope))
		}
	}

	// Merge values: frame values first, then explicit values (explicit takes precedence)
	var mergedValues contextapi.Values
	frameValues := contextapi.GetValues(ctx)

	if (frameValues != nil && frameValues.Len() > 0) || (explicitValues != nil && explicitValues.Len() > 0) {
		mergedValues = contextapi.NewValues()

		// First, copy all frame values
		if frameValues != nil {
			frameValues.Iterate(func(key string, val any) {
				mergedValues.Set(key, val)
			})
		}

		// Then overlay explicit values (these take precedence for conflicts)
		if explicitValues != nil {
			explicitValues.Iterate(func(key string, val any) {
				mergedValues.Set(key, val)
			})
		}

		pairs = append(pairs, contextapi.ValuesPair(mergedValues))
	}

	return pairs
}
