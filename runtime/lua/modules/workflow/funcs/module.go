package funcs

import (
	"sync"

	contextapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	secapi "github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	payloadmod "github.com/wippyai/runtime/runtime/lua/modules/payload"
	"github.com/wippyai/runtime/runtime/lua/modules/upstream"
	"github.com/wippyai/runtime/runtime/lua/security"
	luaconv "github.com/wippyai/runtime/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
)

// Module represents the workflow function module
type Module struct {
	moduleTable *lua.LTable
	once        sync.Once
}

// Executor represents a workflow function executor with context values
type Executor struct {
	values contextapi.Values

	actor    secapi.Actor
	hasActor bool
	scope    secapi.Scope
	hasScope bool

	options    runtime.Bag
	hasOptions bool
}

// NewFuncsModule creates a new workflow function module
func NewFuncsModule() *Module {
	return &Module{}
}

// Name returns the module name
func (m *Module) Name() string {
	return "funcs"
}

// Loader registers the module functions
func (m *Module) Loader(l *lua.LState) int {
	m.once.Do(func() {
		m.initModuleTable(l)
	})

	l.Push(m.moduleTable)
	return 1
}

func (m *Module) initModuleTable(l *lua.LState) {
	value.RegisterTypeMethods(l, "workflow.funcs.Executor", nil, map[string]lua.LGFunction{
		"with_context": m.withContext,
		"with_actor":   m.withActor,
		"with_scope":   m.withScope,
		"with_options": m.withOptions,
		"call":         m.call,
		"async":        m.async,
	})

	t := l.CreateTable(0, 1)
	t.RawSetString("new", l.NewFunction(m.new))
	t.Immutable = true

	m.moduleTable = t
}

func (m *Module) new(l *lua.LState) int {
	values := contextapi.GetValues(l.Context())
	if values != nil {
		values = values.Clone().(contextapi.Values)
	} else {
		values = contextapi.NewValues()
	}

	executor := &Executor{
		values:     values,
		hasActor:   false,
		hasScope:   false,
		hasOptions: false,
	}

	ud := l.NewUserData()
	ud.Value = executor
	ud.Metatable = value.GetTypeMetatable(l, "workflow.funcs.Executor")
	l.Push(ud)
	return 1
}

func (m *Module) withContext(l *lua.LState) int {
	ud := l.CheckUserData(1)
	executor, ok := ud.Value.(*Executor)
	if !ok {
		l.ArgError(1, "workflow funcs executor expected")
		return 0
	}

	if !security.IsAllowed(l.Context(), "funcs.context", "context", nil) {
		l.RaiseError("not allowed to call functions with custom context")
		return 0
	}

	ctxTable := l.CheckTable(2)

	newValues := contextapi.NewValues()
	if executor.values != nil {
		executor.values.Iterate(func(key string, val any) {
			newValues.Set(key, val)
		})
	}

	ctxTable.ForEach(func(k, v lua.LValue) {
		key, ok := k.(lua.LString)
		if !ok {
			l.ArgError(2, "context keys must be strings")
			return
		}
		newValues.Set(string(key), value.ToGoAny(v))
	})

	newExecutor := &Executor{
		values:     newValues,
		actor:      executor.actor,
		hasActor:   executor.hasActor,
		scope:      executor.scope,
		hasScope:   executor.hasScope,
		options:    executor.options,
		hasOptions: executor.hasOptions,
	}

	newUd := l.NewUserData()
	newUd.Value = newExecutor
	newUd.Metatable = value.GetTypeMetatable(l, "workflow.funcs.Executor")
	l.Push(newUd)
	return 1
}

func (m *Module) withActor(l *lua.LState) int {
	ud := l.CheckUserData(1)
	executor, ok := ud.Value.(*Executor)
	if !ok {
		l.ArgError(1, "workflow funcs executor expected")
		return 0
	}

	if !security.IsAllowed(l.Context(), "funcs.security", "security", nil) {
		l.RaiseError("not allowed to call functions with custom security context")
		return 0
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

	newExecutor := &Executor{
		values:     executor.values.Clone().(contextapi.Values),
		actor:      actor,
		hasActor:   true,
		scope:      executor.scope,
		hasScope:   executor.hasScope,
		options:    executor.options,
		hasOptions: executor.hasOptions,
	}

	newUd := l.NewUserData()
	newUd.Value = newExecutor
	newUd.Metatable = value.GetTypeMetatable(l, "workflow.funcs.Executor")
	l.Push(newUd)
	return 1
}

func (m *Module) withScope(l *lua.LState) int {
	ud := l.CheckUserData(1)
	executor, ok := ud.Value.(*Executor)
	if !ok {
		l.ArgError(1, "workflow funcs executor expected")
		return 0
	}

	if !security.IsAllowed(l.Context(), "funcs.security", "security", nil) {
		l.RaiseError("not allowed to call functions with custom security context")
		return 0
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

	newExecutor := &Executor{
		values:     executor.values.Clone().(contextapi.Values),
		actor:      executor.actor,
		hasActor:   executor.hasActor,
		scope:      scope,
		hasScope:   true,
		options:    executor.options,
		hasOptions: executor.hasOptions,
	}

	newUd := l.NewUserData()
	newUd.Value = newExecutor
	newUd.Metatable = value.GetTypeMetatable(l, "workflow.funcs.Executor")
	l.Push(newUd)
	return 1
}

func (m *Module) withOptions(l *lua.LState) int {
	ud := l.CheckUserData(1)
	executor, ok := ud.Value.(*Executor)
	if !ok {
		l.ArgError(1, "workflow funcs executor expected")
		return 0
	}

	optionsTable := l.CheckTable(2)
	optionsData := value.ToGoAny(optionsTable)
	var options runtime.Bag
	if dataMap, ok := optionsData.(map[string]any); ok {
		options = runtime.Bag(dataMap)
	} else {
		options = runtime.Bag{}
	}

	newExecutor := &Executor{
		values:     executor.values.Clone().(contextapi.Values),
		actor:      executor.actor,
		hasActor:   executor.hasActor,
		scope:      executor.scope,
		hasScope:   executor.hasScope,
		options:    options,
		hasOptions: true,
	}

	newUd := l.NewUserData()
	newUd.Value = newExecutor
	newUd.Metatable = value.GetTypeMetatable(l, "workflow.funcs.Executor")
	l.Push(newUd)
	return 1
}

// call sends a funcs.call command and yields waiting for response
func (m *Module) call(l *lua.LState) int {
	ud := l.CheckUserData(1)
	executor, ok := ud.Value.(*Executor)
	if !ok {
		l.ArgError(1, "workflow funcs executor expected")
		return 0
	}

	targetIndex := 2 // after self
	target := l.CheckString(targetIndex)
	if target == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("target name is required"))
		return 2
	}

	regID := registry.ParseID(target)
	if regID.NS == "" || regID.Name == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("invalid registry ID: namespace and name required"))
		return 2
	}

	if !security.IsAllowed(l.Context(), "funcs.call", target, nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("not allowed to call function: " + target))
		return 2
	}

	// Build payloads from arguments
	payloads := executor.buildPayloads(l, targetIndex+1)

	// Create request with funcs.call command type
	req := upstream.NewRequest(l, "funcs.call", nil, payloads...)

	// Send and yield waiting for response
	return upstream.SendAndYield(l, req)
}

// async sends a funcs.call command and returns immediately with Request
func (m *Module) async(l *lua.LState) int {
	ud := l.CheckUserData(1)
	executor, ok := ud.Value.(*Executor)
	if !ok {
		l.ArgError(1, "workflow funcs executor expected")
		return 0
	}

	targetIndex := 2 // after self
	target := l.CheckString(targetIndex)
	if target == "" {
		l.RaiseError("target name is required")
		return 0
	}

	regID := registry.ParseID(target)
	if regID.NS == "" || regID.Name == "" {
		l.RaiseError("invalid registry ID: namespace and name required")
		return 0
	}

	if !security.IsAllowed(l.Context(), "funcs.call", target, nil) {
		l.RaiseError("not allowed to call function: %s", target)
		return 0
	}

	// Build payloads from arguments
	payloads := executor.buildPayloads(l, targetIndex+1)

	// Create request with funcs.call command type
	req := upstream.NewRequest(l, "funcs.call", nil, payloads...)

	// Send command to upstream (non-blocking)
	up, ok := runtime.GetUpstream(l.Context())
	if !ok {
		l.RaiseError("no upstream handler found in context")
		return 0
	}

	if err := up.SendRequest(req); err != nil {
		l.RaiseError("failed to send request: %s", err.Error())
		return 0
	}

	// Return the request - caller can await on its channel later
	l.Push(upstream.WrapRequest(l, req))
	return 1
}

// buildPayloads extracts payloads from Lua arguments starting at given index
func (e *Executor) buildPayloads(l *lua.LState, startIndex int) []payload.Payload {
	var payloads []payload.Payload

	for i := startIndex; i <= l.GetTop(); i++ {
		val := l.Get(i)

		// Check if already a payload wrapper
		if ud, ok := val.(*lua.LUserData); ok {
			if pw, ok := ud.Value.(*payloadmod.Wrapper); ok {
				payloads = append(payloads, pw.Payload)
				continue
			}
		}

		// Create new payload from Lua value
		payloads = append(payloads, luaconv.ExportPayload(val))
	}

	// Prepend context information if present
	if e.hasActor || e.hasScope || (e.values != nil && e.values.Len() > 0) || e.hasOptions {
		ctx := make(map[string]any)
		if e.hasActor {
			ctx["actor"] = e.actor
		}
		if e.hasScope {
			ctx["scope"] = e.scope
		}
		if e.values != nil && e.values.Len() > 0 {
			vals := make(map[string]any)
			e.values.Iterate(func(k string, v any) {
				vals[k] = v
			})
			ctx["values"] = vals
		}
		if e.hasOptions {
			ctx["options"] = e.options
		}
		// Context is sent as first payload
		payloads = append([]payload.Payload{payload.New(ctx)}, payloads...)
	}

	return payloads
}
