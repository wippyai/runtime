package security

import (
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	secapi "github.com/ponyruntime/pony/api/security"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/system/security"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Module represents the security module for Lua
type Module struct {
	log *zap.Logger
}

// NewSecurityModule creates a new security module
func NewSecurityModule(log *zap.Logger) *Module {
	return &Module{
		log: log.Named("security"),
	}
}

// Name returns the module name
func (m *Module) Name() string {
	return "security"
}

// Loader is the entry point for loading the module into Lua
func (m *Module) Loader(l *lua.LState) int {
	// Create module table with preallocated size
	mod := l.CreateTable(0, 8)

	// Register context-related functions
	mod.RawSetString("actor", l.NewFunction(m.actor))
	mod.RawSetString("scope", l.NewFunction(m.scope))
	mod.RawSetString("can", l.NewFunction(m.can))

	// Register policy and scope functions
	mod.RawSetString("policy", l.NewFunction(m.policy))
	mod.RawSetString("named_scope", l.NewFunction(m.namedScope))
	mod.RawSetString("new_scope", l.NewFunction(m.newScope))
	mod.RawSetString("with_policy", l.NewFunction(m.withPolicy))
	mod.RawSetString("new_actor", l.NewFunction(m.newActor))

	// Register token store functions
	mod.RawSetString("token_store", l.NewFunction(m.tokenStore))

	// Register types and their methods
	registerActorType(l)
	registerScopeType(l)
	registerPolicyType(l)
	registerTokenStoreType(l)

	// Return the module
	l.Push(mod)
	return 1
}

// Actor retrieves the current actor from context
func (m *Module) actor(l *lua.LState) int {
	actor, exists := secapi.GetActor(l.Context())
	if !exists {
		l.Push(lua.LNil)
		return 1
	}

	// Convert actor to Lua representation
	actorUD := wrapActor(l, actor)
	l.Push(actorUD)
	return 1
}

// Scope retrieves the current scope from context
func (m *Module) scope(l *lua.LState) int {
	scope, exists := secapi.GetScope(l.Context())
	if !exists {
		l.Push(lua.LNil)
		return 1
	}

	// Convert scope to Lua representation
	scopeUD := wrapScope(l, scope)
	l.Push(scopeUD)
	return 1
}

// Can checks if the current actor can perform an action on a resource
func (m *Module) can(l *lua.LState) int {
	action := l.CheckString(1)
	resourceStr := l.CheckString(2)

	// Get metadata from third argument if provided
	meta, err := optMetadataFromLuaTable(l, 3)
	if err != nil {
		return 0 // Error already raised
	}

	allowed := secapi.IsAllowed(l.Context(), action, resourceStr, meta)
	l.Push(lua.LBool(allowed))
	return 1
}

// Policy retrieves a policy by ID
func (m *Module) policy(l *lua.LState) int {
	idStr := l.CheckString(1)
	id := registry.ParseID(idStr)

	policy, err := secapi.GetPolicy(l.Context(), id)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	policyUD := wrapPolicy(l, policy)
	l.Push(policyUD)
	l.Push(lua.LNil)
	return 2
}

// NamedScope retrieves a policy group as a scope
func (m *Module) namedScope(l *lua.LState) int {
	idStr := l.CheckString(1)
	id := registry.ParseID(idStr)

	scope, err := secapi.GetPolicyGroup(l.Context(), id)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	scopeUD := wrapScope(l, scope)
	l.Push(scopeUD)
	l.Push(lua.LNil)
	return 2
}

// NewScope creates a new empty scope
func (m *Module) newScope(l *lua.LState) int {
	// Create an empty scope
	scope := security.NewScope(nil)

	// If policies table provided, add them to the scope
	if l.GetTop() >= 1 {
		policiesTable := l.CheckTable(1)
		policiesTable.ForEach(func(_, policyValue lua.LValue) {
			if policyUD, ok := policyValue.(*lua.LUserData); ok {
				if policy, ok := policyUD.Value.(secapi.Policy); ok {
					scope = scope.With(policy)
				}
			}
		})
	}

	scopeUD := wrapScope(l, scope)
	l.Push(scopeUD)
	return 1
}

// WithPolicy creates a new context with added policy
func (m *Module) withPolicy(l *lua.LState) int {
	policyUD := l.CheckUserData(1)
	policy, ok := policyUD.Value.(secapi.Policy)
	if !ok {
		l.ArgError(1, "policy expected")
		return 0
	}

	ctx := secapi.WithPolicy(l.Context(), policy)
	l.SetContext(ctx)
	return 0
}

// NewActor creates a new actor
func (m *Module) newActor(l *lua.LState) int {
	id := l.CheckString(1)

	// Get metadata from second argument if provided
	meta, err := optMetadataFromLuaTable(l, 2)
	if err != nil {
		return 0 // Error already raised
	}

	actor := secapi.Actor{
		ID:   id,
		Meta: meta,
	}

	actorUD := wrapActor(l, actor)
	l.Push(actorUD)
	return 1
}

// TokenStore gets a token store resource
func (m *Module) tokenStore(l *lua.LState) int {
	idStr := l.CheckString(1)
	id := registry.ParseID(idStr)

	// Get unit of work from context
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("unit of work not found in context"))
		return 2
	}

	// Get resource registry from context
	resources := resource.GetResources(l.Context())
	if resources == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("resource registry not found in context"))
		return 2
	}

	// Acquire the token store resource
	res, err := resources.Acquire(l.Context(), id, resource.ModeNormal)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Get the actual token store from the resource
	tokenStore, err := getTokenStoreFromResource(res)
	if err != nil {
		res.Release()
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Create a token store wrapper
	wrapper := NewTokenStore(uw, res, tokenStore, m.log)
	tokenStoreUD := wrapTokenStore(l, wrapper)

	l.Push(tokenStoreUD)
	l.Push(lua.LNil)
	return 2
}
