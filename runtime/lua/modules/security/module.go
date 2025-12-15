package security

import (
	"sync"

	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	luasec "github.com/wippyai/runtime/runtime/lua/security"
	secsystem "github.com/wippyai/runtime/system/security"
	lua "github.com/yuin/gopher-lua"
)

var (
	moduleTable *lua.LTable
	initOnce    sync.Once
)

func init() {
	value.RegisterTypeMethods(nil, actorTypeName, nil, actorMethods)
	value.RegisterTypeMethods(nil, scopeTypeName, nil, scopeMethods)
	value.RegisterTypeMethods(nil, policyTypeName, nil, policyMethods)
	value.RegisterTypeMethods(nil, tokenStoreTypeName, nil, tokenStoreMethods)
}

func initModuleTable() {
	mod := lua.CreateTable(0, 8)

	mod.RawSetString("actor", lua.LGoFunc(actor))
	mod.RawSetString("scope", lua.LGoFunc(scope))
	mod.RawSetString("can", lua.LGoFunc(can))
	mod.RawSetString("policy", lua.LGoFunc(policy))
	mod.RawSetString("named_scope", lua.LGoFunc(namedScope))
	mod.RawSetString("new_scope", lua.LGoFunc(newScope))
	mod.RawSetString("new_actor", lua.LGoFunc(newActor))
	mod.RawSetString("token_store", lua.LGoFunc(tokenStoreGet))

	mod.Immutable = true
	moduleTable = mod
}

// Module is the security module definition.
var Module = &luaapi.ModuleDef{
	Name:        "security",
	Description: "Security actors, scopes, and policies",
	Class:       []string{luaapi.ClassSecurity, luaapi.ClassNondeterministic},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		initOnce.Do(initModuleTable)
		return moduleTable, []luaapi.YieldType{
			{Sample: &ValidateYield{}, CmdID: security.ValidateToken},
			{Sample: &CreateYield{}, CmdID: security.CreateToken},
			{Sample: &RevokeYield{}, CmdID: security.RevokeToken},
		}
	},
}

func actor(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		return 1
	}

	act, exists := security.GetActor(ctx)
	if !exists {
		l.Push(lua.LNil)
		return 1
	}

	l.Push(wrapActor(l, act))
	return 1
}

func scope(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		return 1
	}

	sc, exists := security.GetScope(ctx)
	if !exists {
		l.Push(lua.LNil)
		return 1
	}

	l.Push(wrapScope(l, sc))
	return 1
}

func can(l *lua.LState) int {
	action := l.CheckString(1)
	resource := l.CheckString(2)

	meta := optMetadataFromLuaTable(l, 3)

	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LFalse)
		return 1
	}

	allowed := security.IsAllowed(ctx, action, resource, meta)
	l.Push(lua.LBool(allowed))
	return 1
}

func policy(l *lua.LState) int {
	idStr := l.CheckString(1)
	id := registry.ParseID(idStr)

	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "no context").WithKind(lua.KindInternal).WithRetryable(false))
		return 2
	}

	if !luasec.IsAllowed(ctx, "security.policy.get", idStr, nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: access policy").WithKind(lua.KindInvalid).WithRetryable(false))
		return 2
	}

	pol, err := security.GetPolicy(ctx, id)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "get policy").WithKind(lua.KindInternal).WithRetryable(false))
		return 2
	}

	l.Push(wrapPolicy(l, pol))
	l.Push(lua.LNil)
	return 2
}

func namedScope(l *lua.LState) int {
	idStr := l.CheckString(1)
	id := registry.ParseID(idStr)

	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "no context").WithKind(lua.KindInternal).WithRetryable(false))
		return 2
	}

	if !luasec.IsAllowed(ctx, "security.policy_group.get", idStr, nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: access policy group").WithKind(lua.KindInvalid).WithRetryable(false))
		return 2
	}

	sc, err := security.GetPolicyGroup(ctx, id)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "get policy group").WithKind(lua.KindInternal).WithRetryable(false))
		return 2
	}

	l.Push(wrapScope(l, sc))
	l.Push(lua.LNil)
	return 2
}

func newScope(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.RaiseError("no context")
		return 0
	}

	if !luasec.IsAllowed(ctx, "security.scope.create", "custom", nil) {
		l.RaiseError("not allowed to create custom scopes")
		return 0
	}

	sc := secsystem.NewScope(nil)

	if l.GetTop() >= 1 {
		policiesTable := l.CheckTable(1)
		policiesTable.ForEach(func(_, policyValue lua.LValue) {
			if policyUD, ok := policyValue.(*lua.LUserData); ok {
				if pol, ok := policyUD.Value.(security.Policy); ok {
					sc = sc.With(pol)
				}
			}
		})
	}

	l.Push(wrapScope(l, sc))
	return 1
}

func newActor(l *lua.LState) int {
	id := l.CheckString(1)

	ctx := l.Context()
	if ctx == nil {
		l.RaiseError("no context")
		return 0
	}

	if !luasec.IsAllowed(ctx, "security.actor.create", id, nil) {
		l.RaiseError("not allowed to create actor with ID: %s", id)
		return 0
	}

	meta := optMetadataFromLuaTable(l, 2)

	act := security.Actor{
		ID:   id,
		Meta: meta,
	}
	l.Push(wrapActor(l, act))
	return 1
}
