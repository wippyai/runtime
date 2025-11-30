package security

import (
	"github.com/wippyai/runtime/api/registry"
	secapi "github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/lua/security"
	lua "github.com/yuin/gopher-lua"
)

const scopeTypeName = "security.Scope"

var scopeMethods = map[string]lua.LGFunction{
	"with":     scopeWith,
	"without":  scopeWithout,
	"evaluate": scopeEvaluate,
	"contains": scopeContains,
	"policies": scopePolicies,
}

func wrapScope(l *lua.LState, scope secapi.Scope) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = scope
	ud.Metatable = scopeMetatable
	return ud
}

func checkScope(l *lua.LState, idx int) secapi.Scope {
	ud := l.CheckUserData(idx)
	if scope, ok := ud.Value.(secapi.Scope); ok {
		return scope
	}
	l.ArgError(idx, "Scope expected")
	return nil
}

func scopeWith(l *lua.LState) int {
	scope := checkScope(l, 1)
	if scope == nil {
		return 0
	}
	policyUD := l.CheckUserData(2)
	pol, ok := policyUD.Value.(secapi.Policy)
	if !ok {
		l.ArgError(2, "Policy expected")
		return 0
	}

	ctx := l.Context()
	if ctx == nil || !security.IsAllowed(ctx, "security.scope.create", "with", nil) {
		l.RaiseError("not allowed to add policy to scope")
		return 0
	}

	newScope := scope.With(pol)
	l.Push(wrapScope(l, newScope))
	return 1
}

func scopeWithout(l *lua.LState) int {
	scope := checkScope(l, 1)
	if scope == nil {
		return 0
	}
	var policyID registry.ID

	arg := l.Get(2)
	switch v := arg.(type) {
	case lua.LString:
		policyID = registry.ParseID(string(v))
	case *lua.LUserData:
		if pol, ok := v.Value.(secapi.Policy); ok {
			policyID = pol.ID()
		} else {
			l.ArgError(2, "Policy or policy ID string expected")
			return 0
		}
	case *lua.LTable:
		ns := v.RawGetString("ns")
		name := v.RawGetString("name")
		if ns == lua.LNil || name == lua.LNil {
			l.ArgError(2, "ID table must have ns and name fields")
			return 0
		}
		policyID = registry.NewID(ns.String(), name.String())
	default:
		l.ArgError(2, "Policy ID expected as string, table, or policy object")
		return 0
	}

	ctx := l.Context()
	if ctx == nil || !security.IsAllowed(ctx, "security.scope.create", "without", nil) {
		l.RaiseError("not allowed to remove policy from scope")
		return 0
	}

	newScope := scope.Without(policyID)
	l.Push(wrapScope(l, newScope))
	return 1
}

func scopeEvaluate(l *lua.LState) int {
	scope := checkScope(l, 1)
	if scope == nil {
		return 0
	}
	actor := checkActor(l, 2)
	action := l.CheckString(3)
	resource := l.CheckString(4)

	meta, err := optMetadataFromLuaTable(l, 5)
	if err != nil {
		l.RaiseError("%s", err.Error())
		return 0
	}

	result := scope.Evaluate(actor, action, resource, meta)

	var resultValue lua.LValue
	switch result {
	case secapi.Allow:
		resultValue = lua.LString("allow")
	case secapi.Deny:
		resultValue = lua.LString("deny")
	default:
		resultValue = lua.LString("undefined")
	}

	l.Push(resultValue)
	return 1
}

func scopeContains(l *lua.LState) int {
	scope := checkScope(l, 1)
	if scope == nil {
		return 0
	}
	var policyID registry.ID

	arg := l.Get(2)
	switch v := arg.(type) {
	case lua.LString:
		policyID = registry.ParseID(string(v))
	case *lua.LUserData:
		if pol, ok := v.Value.(secapi.Policy); ok {
			policyID = pol.ID()
		} else {
			l.ArgError(2, "Policy or policy ID string expected")
			return 0
		}
	case *lua.LTable:
		ns := v.RawGetString("ns")
		name := v.RawGetString("name")
		if ns == lua.LNil || name == lua.LNil {
			l.ArgError(2, "ID table must have ns and name fields")
			return 0
		}
		policyID = registry.NewID(ns.String(), name.String())
	default:
		l.ArgError(2, "Policy ID expected as string, table, or policy object")
		return 0
	}

	l.Push(lua.LBool(scope.Contains(policyID)))
	return 1
}

func scopePolicies(l *lua.LState) int {
	scope := checkScope(l, 1)
	if scope == nil {
		return 0
	}
	policies := scope.Policies()

	policiesTable := lua.CreateTable(len(policies), 0)
	for i, pol := range policies {
		policyUD := wrapPolicy(l, pol)
		policiesTable.RawSetInt(i+1, policyUD)
	}

	l.Push(policiesTable)
	return 1
}

func scopeToString(l *lua.LState) int {
	l.Push(lua.LString("security.Scope{}"))
	return 1
}
