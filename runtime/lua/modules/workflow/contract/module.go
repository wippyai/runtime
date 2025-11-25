package contract

import (
	"sync"

	contextapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	secapi "github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/api/workflow/std"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	payloadmod "github.com/wippyai/runtime/runtime/lua/modules/payload"
	"github.com/wippyai/runtime/runtime/lua/modules/upstream"
	"github.com/wippyai/runtime/runtime/lua/security"
	luaconv "github.com/wippyai/runtime/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
)

const (
	TypeNameProxy = "workflow.contract.Proxy"
)

// Module provides workflow-safe contract proxy
type Module struct {
	moduleTable *lua.LTable
	once        sync.Once
}

// Proxy represents a contract binding proxy with context
type Proxy struct {
	bindingID registry.ID
	values    contextapi.Values
	actor     secapi.Actor
	hasActor  bool
	scope     secapi.Scope
	hasScope  bool
	options   runtime.Bag
}

// NewContractModule creates a new workflow contract module
func NewContractModule() *Module {
	return &Module{}
}

// Info returns module metadata
func (m *Module) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "contract",
		Description: "Workflow-safe contract proxy",
		Class:       []string{luaapi.ClassWorkflow, luaapi.ClassNondeterministic},
	}
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
	value.RegisterTypeMethods(l, TypeNameProxy, nil, map[string]lua.LGFunction{
		"with_context": m.withContext,
		"with_actor":   m.withActor,
		"with_scope":   m.withScope,
		"with_options": m.withOptions,
		"call":         m.call,
		"async":        m.async,
	})

	t := l.CreateTable(0, 1)
	t.RawSetString("open", l.NewFunction(m.open))
	t.Immutable = true

	m.moduleTable = t
}

// open creates a proxy for a contract binding
func (m *Module) open(l *lua.LState) int {
	bindingIDStr := l.CheckString(1)
	if bindingIDStr == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("binding ID is required"))
		return 2
	}

	bindingID := registry.ParseID(bindingIDStr)
	if bindingID.NS == "" || bindingID.Name == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("invalid binding ID: namespace and name required"))
		return 2
	}

	if !security.IsAllowed(l.Context(), "contract.open", bindingIDStr, nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("not allowed to open binding: " + bindingIDStr))
		return 2
	}

	values := contextapi.GetValues(l.Context())
	if values != nil {
		values = values.Clone().(contextapi.Values)
	} else {
		values = contextapi.NewValues()
	}

	proxy := &Proxy{
		bindingID: bindingID,
		values:    values,
	}

	ud := l.NewUserData()
	ud.Value = proxy
	ud.Metatable = value.GetTypeMetatable(l, TypeNameProxy)
	l.Push(ud)
	l.Push(lua.LNil)
	return 2
}

func (m *Module) withContext(l *lua.LState) int {
	ud := l.CheckUserData(1)
	proxy, ok := ud.Value.(*Proxy)
	if !ok {
		l.ArgError(1, "workflow contract proxy expected")
		return 0
	}

	if !security.IsAllowed(l.Context(), "contract.context", "context", nil) {
		l.RaiseError("not allowed to use contracts with custom context")
		return 0
	}

	ctxTable := l.CheckTable(2)

	newValues := contextapi.NewValues()
	if proxy.values != nil {
		proxy.values.Iterate(func(key string, val any) {
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

	newProxy := &Proxy{
		bindingID: proxy.bindingID,
		values:    newValues,
		actor:     proxy.actor,
		hasActor:  proxy.hasActor,
		scope:     proxy.scope,
		hasScope:  proxy.hasScope,
		options:   proxy.options,
	}

	newUd := l.NewUserData()
	newUd.Value = newProxy
	newUd.Metatable = value.GetTypeMetatable(l, TypeNameProxy)
	l.Push(newUd)
	return 1
}

func (m *Module) withActor(l *lua.LState) int {
	ud := l.CheckUserData(1)
	proxy, ok := ud.Value.(*Proxy)
	if !ok {
		l.ArgError(1, "workflow contract proxy expected")
		return 0
	}

	if !security.IsAllowed(l.Context(), "contract.security", "security", nil) {
		l.RaiseError("not allowed to use contracts with custom security context")
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

	newProxy := &Proxy{
		bindingID: proxy.bindingID,
		values:    proxy.values.Clone().(contextapi.Values),
		actor:     actor,
		hasActor:  true,
		scope:     proxy.scope,
		hasScope:  proxy.hasScope,
		options:   proxy.options,
	}

	newUd := l.NewUserData()
	newUd.Value = newProxy
	newUd.Metatable = value.GetTypeMetatable(l, TypeNameProxy)
	l.Push(newUd)
	return 1
}

func (m *Module) withScope(l *lua.LState) int {
	ud := l.CheckUserData(1)
	proxy, ok := ud.Value.(*Proxy)
	if !ok {
		l.ArgError(1, "workflow contract proxy expected")
		return 0
	}

	if !security.IsAllowed(l.Context(), "contract.security", "security", nil) {
		l.RaiseError("not allowed to use contracts with custom security context")
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

	newProxy := &Proxy{
		bindingID: proxy.bindingID,
		values:    proxy.values.Clone().(contextapi.Values),
		actor:     proxy.actor,
		hasActor:  proxy.hasActor,
		scope:     scope,
		hasScope:  true,
		options:   proxy.options,
	}

	newUd := l.NewUserData()
	newUd.Value = newProxy
	newUd.Metatable = value.GetTypeMetatable(l, TypeNameProxy)
	l.Push(newUd)
	return 1
}

func (m *Module) withOptions(l *lua.LState) int {
	ud := l.CheckUserData(1)
	proxy, ok := ud.Value.(*Proxy)
	if !ok {
		l.ArgError(1, "workflow contract proxy expected")
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

	newProxy := &Proxy{
		bindingID: proxy.bindingID,
		values:    proxy.values.Clone().(contextapi.Values),
		actor:     proxy.actor,
		hasActor:  proxy.hasActor,
		scope:     proxy.scope,
		hasScope:  proxy.hasScope,
		options:   options,
	}

	newUd := l.NewUserData()
	newUd.Value = newProxy
	newUd.Metatable = value.GetTypeMetatable(l, TypeNameProxy)
	l.Push(newUd)
	return 1
}

// call sends a contract.call command and yields waiting for response
func (m *Module) call(l *lua.LState) int {
	ud := l.CheckUserData(1)
	proxy, ok := ud.Value.(*Proxy)
	if !ok {
		l.ArgError(1, "workflow contract proxy expected")
		return 0
	}

	methodIndex := 2
	method := l.CheckString(methodIndex)
	if method == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("method name is required"))
		return 2
	}

	permResource := proxy.bindingID.String() + ":" + method
	if !security.IsAllowed(l.Context(), "contract.call", permResource, nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("not allowed to call method: " + method))
		return 2
	}

	payloads := proxy.buildPayloads(l, method, methodIndex+1)
	req := upstream.NewRequest(l, std.TypeContractCall, nil, payloads...)

	return upstream.SendAndYield(l, req)
}

// async sends a contract.call command and returns immediately with Request
func (m *Module) async(l *lua.LState) int {
	ud := l.CheckUserData(1)
	proxy, ok := ud.Value.(*Proxy)
	if !ok {
		l.ArgError(1, "workflow contract proxy expected")
		return 0
	}

	methodIndex := 2
	method := l.CheckString(methodIndex)
	if method == "" {
		l.RaiseError("method name is required")
		return 0
	}

	permResource := proxy.bindingID.String() + ":" + method
	if !security.IsAllowed(l.Context(), "contract.call", permResource, nil) {
		l.RaiseError("not allowed to call method: %s", method)
		return 0
	}

	payloads := proxy.buildPayloads(l, method, methodIndex+1)
	req := upstream.NewRequest(l, std.TypeContractCall, nil, payloads...)

	up, ok := runtime.GetUpstream(l.Context())
	if !ok {
		l.RaiseError("no upstream handler found in context")
		return 0
	}

	if err := up.SendRequest(req); err != nil {
		l.RaiseError("failed to send request: %s", err.Error())
		return 0
	}

	l.Push(upstream.WrapRequest(l, req))
	return 1
}

// buildPayloads creates typed header and extracts argument payloads
func (p *Proxy) buildPayloads(l *lua.LState, method string, argsStartIndex int) []payload.Payload {
	header := &std.ContractCallHeader{
		BindingID: p.bindingID,
		Method:    method,
	}

	if p.values != nil && p.values.Len() > 0 {
		header.Context = make(map[string]any)
		p.values.Iterate(func(k string, v any) {
			header.Context[k] = v
		})
	}

	if p.hasActor || p.hasScope {
		header.Security = &std.SecurityContext{}
		if p.hasActor {
			header.Security.ActorID = p.actor.ID
			header.Security.ActorMeta = p.actor.Meta
		}
		if p.hasScope {
			policies := p.scope.Policies()
			header.Security.ScopePolicies = make([]registry.ID, len(policies))
			for i, pol := range policies {
				header.Security.ScopePolicies[i] = pol.ID()
			}
		}
	}

	if p.options != nil {
		header.Options = p.convertOptions()
	}

	payloads := []payload.Payload{payload.New(header)}

	for i := argsStartIndex; i <= l.GetTop(); i++ {
		val := l.Get(i)

		if ud, ok := val.(*lua.LUserData); ok {
			if pw, ok := ud.Value.(*payloadmod.Wrapper); ok {
				payloads = append(payloads, pw.Payload)
				continue
			}
		}

		payloads = append(payloads, luaconv.ExportPayload(val))
	}

	return payloads
}

func (p *Proxy) convertOptions() *std.ContractCallOptions {
	if p.options == nil {
		return nil
	}

	opts := &std.ContractCallOptions{}

	if v, ok := p.options["timeout"].(string); ok {
		opts.Timeout = v
	}

	if retryMap, ok := p.options["retry"].(map[string]any); ok {
		opts.Retry = &std.RetryPolicy{}
		if v, ok := retryMap["max_attempts"].(int); ok {
			opts.Retry.MaxAttempts = v
		}
		if v, ok := retryMap["initial_interval"].(string); ok {
			opts.Retry.InitialInterval = v
		}
		if v, ok := retryMap["backoff_coefficient"].(float64); ok {
			opts.Retry.BackoffCoefficient = v
		}
		if v, ok := retryMap["max_interval"].(string); ok {
			opts.Retry.MaxInterval = v
		}
	}

	return opts
}
