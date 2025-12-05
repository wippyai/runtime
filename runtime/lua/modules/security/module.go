package security

import (
	"sync"

	securityapi "github.com/wippyai/runtime/api/dispatcher/security"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	secapi "github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/security"
	secsystem "github.com/wippyai/runtime/system/security"
	lua "github.com/yuin/gopher-lua"
)

var (
	moduleTable *lua.LTable
	initOnce    sync.Once
)

func init() {
	value.RegisterTypeMethods(nil, actorTypeName,
		map[string]lua.LGFunction{"__tostring": actorToString},
		actorMethods)

	value.RegisterTypeMethods(nil, scopeTypeName,
		map[string]lua.LGFunction{"__tostring": scopeToString},
		scopeMethods)

	value.RegisterTypeMethods(nil, policyTypeName,
		map[string]lua.LGFunction{"__tostring": policyToString},
		policyMethods)

	value.RegisterTypeMethods(nil, tokenStoreTypeName,
		map[string]lua.LGFunction{"__tostring": tokenStoreToString},
		tokenStoreMethods)
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
			{Sample: &ValidateYield{}, CmdID: securityapi.CmdTokenValidate},
			{Sample: &CreateYield{}, CmdID: securityapi.CmdTokenCreate},
			{Sample: &RevokeYield{}, CmdID: securityapi.CmdTokenRevoke},
		}
	},
}

func actor(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		return 1
	}

	act, exists := secapi.GetActor(ctx)
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

	sc, exists := secapi.GetScope(ctx)
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

	meta, err := optMetadataFromLuaTable(l, 3)
	if err != nil {
		l.RaiseError("%s", err.Error())
		return 0
	}

	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LFalse)
		return 1
	}

	allowed := secapi.IsAllowed(ctx, action, resource, meta)
	l.Push(lua.LBool(allowed))
	return 1
}

func policy(l *lua.LState) int {
	idStr := l.CheckString(1)
	id := registry.ParseID(idStr)

	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context"))
		return 2
	}

	if !security.IsAllowed(ctx, "security.policy.get", idStr, nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("permission denied: access policy"))
		return 2
	}

	pol, err := secapi.GetPolicy(ctx, id)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
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
		l.Push(lua.LString("no context"))
		return 2
	}

	if !security.IsAllowed(ctx, "security.policy_group.get", idStr, nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("permission denied: access policy group"))
		return 2
	}

	sc, err := secapi.GetPolicyGroup(ctx, id)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
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

	if !security.IsAllowed(ctx, "security.scope.create", "custom", nil) {
		l.RaiseError("not allowed to create custom scopes")
		return 0
	}

	sc := secsystem.NewScope(nil)

	if l.GetTop() >= 1 {
		policiesTable := l.CheckTable(1)
		policiesTable.ForEach(func(_, policyValue lua.LValue) {
			if policyUD, ok := policyValue.(*lua.LUserData); ok {
				if pol, ok := policyUD.Value.(secapi.Policy); ok {
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

	if !security.IsAllowed(ctx, "security.actor.create", id, nil) {
		l.RaiseError("not allowed to create actor with ID: %s", id)
		return 0
	}

	meta, err := optMetadataFromLuaTable(l, 2)
	if err != nil {
		l.RaiseError("%s", err.Error())
		return 0
	}

	act := secapi.Actor{
		ID:   id,
		Meta: meta,
	}
	l.Push(wrapActor(l, act))
	return 1
}
