package security

import (
	"github.com/ponyruntime/pony/api/registry"
	secapi "github.com/ponyruntime/pony/api/security"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	securityapi "github.com/ponyruntime/pony/runtime/lua/security"
	lua "github.com/yuin/gopher-lua"
)

const ScopeMetatable = "security.Scope"

// wrapScope wraps a security.Scope as a Lua userdata
func wrapScope(l *lua.LState, scope secapi.Scope) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = scope
	ud.Metatable = value.GetTypeMetatable(l, ScopeMetatable)
	return ud
}

// checkScope checks if the first argument is a Scope and returns it
func checkScope(l *lua.LState) secapi.Scope {
	ud := l.CheckUserData(1)
	if scope, ok := ud.Value.(secapi.Scope); ok {
		return scope
	}
	l.ArgError(1, "Scope expected")
	return nil
}

// registerScopeType registers the Scope type and methods
func registerScopeType(l *lua.LState) {
	value.RegisterMethods(l, ScopeMetatable, map[string]lua.LGFunction{
		"with":     scopeWith,
		"without":  scopeWithout,
		"evaluate": scopeEvaluate,
		"contains": scopeContains,
		"policies": scopePolicies,
	})
}

// scopeWith adds a policy to the scope
func scopeWith(l *lua.LState) int {
	scope := checkScope(l)
	if scope == nil {
		return 0
	}

	policyUD := l.CheckUserData(2)
	policy, ok := policyUD.Value.(secapi.Policy)
	if !ok {
		l.ArgError(2, "Policy expected")
		return 0
	}

	if !securityapi.IsAllowed(l.Context(), "security.scope.create", "with", nil) {
		l.RaiseError("not allowed to add policy to scope")
		return 0
	}

	newScope := scope.With(policy)
	l.Push(wrapScope(l, newScope))
	return 1
}

// scopeWithout removes a policy from the scope
func scopeWithout(l *lua.LState) int {
	scope := checkScope(l)
	if scope == nil {
		return 0
	}

	// Check if the policy ID is passed as string or ID object
	var policyID registry.ID

	switch l.Get(2).Type() {
	case lua.LTString:
		idStr := l.CheckString(2)
		policyID = registry.ParseID(idStr)
	case lua.LTUserData:
		// Assuming userdata might be a Policy with ID() method
		policyUD := l.CheckUserData(2)
		if policy, ok := policyUD.Value.(secapi.Policy); ok {
			policyID = policy.ID()
		} else {
			l.ArgError(2, "Policy or policy ID string expected")
			return 0
		}
	case lua.LTTable:
		// ID might be represented as a table with ns and name fields
		idTable := l.CheckTable(2)
		ns := idTable.RawGetString("ns")
		name := idTable.RawGetString("name")

		if ns == lua.LNil || name == lua.LNil {
			l.ArgError(2, "ID table must have ns and name fields")
			return 0
		}

		policyID = registry.ID{
			NS:   ns.String(),
			Name: name.String(),
		}
	case lua.LTNil, lua.LTBool, lua.LTNumber, lua.LTFunction, lua.LTThread, lua.LTChannel:
		// FIXME rework on demand
		fallthrough
	default:
		l.ArgError(2, "Policy ID expected as string, table, or policy object")
		return 0
	}

	if !securityapi.IsAllowed(l.Context(), "security.scope.create", "without", nil) {
		l.RaiseError("not allowed to remove policy from scope")
		return 0
	}

	newScope := scope.Without(policyID)
	l.Push(wrapScope(l, newScope))
	return 1
}

// scopeEvaluate evaluates if an action is allowed
func scopeEvaluate(l *lua.LState) int {
	scope := checkScope(l)
	if scope == nil {
		return 0
	}

	// Get actor
	actorUD := l.CheckUserData(2)
	actor, ok := actorUD.Value.(secapi.Actor)
	if !ok {
		l.ArgError(2, "Actor expected")
		return 0
	}

	// Get action and resource
	action := l.CheckString(3)
	resourceStr := l.CheckString(4)

	// Get metadata (optional)
	meta, err := optMetadataFromLuaTable(l, 5)
	if err != nil {
		l.RaiseError("%s", err.Error())
		return 0
	}

	// Evaluate the action
	result := scope.Evaluate(actor, action, resourceStr, meta)

	// Convert result to Lua value
	var resultValue lua.LValue
	switch result {
	case secapi.Allow:
		resultValue = lua.LString("allow")
	case secapi.Deny:
		resultValue = lua.LString("deny")
	case secapi.Undefined:
		resultValue = lua.LString("undefined")
	default:
		resultValue = lua.LString("undefined")
	}

	l.Push(resultValue)
	return 1
}

// scopeContains checks if a policy is in the scope
func scopeContains(l *lua.LState) int {
	scope := checkScope(l)
	if scope == nil {
		return 0
	}

	// Get policy ID
	var policyID registry.ID

	switch l.Get(2).Type() {
	case lua.LTString:
		idStr := l.CheckString(2)
		policyID = registry.ParseID(idStr)
	case lua.LTUserData:
		// Check if userdata is a Policy
		policyUD := l.CheckUserData(2)
		if policy, ok := policyUD.Value.(secapi.Policy); ok {
			policyID = policy.ID()
		} else {
			l.ArgError(2, "Policy or policy ID string expected")
			return 0
		}
	case lua.LTTable:
		// ID might be represented as a table with ns and name fields
		idTable := l.CheckTable(2)
		ns := idTable.RawGetString("ns")
		name := idTable.RawGetString("name")

		if ns == lua.LNil || name == lua.LNil {
			l.ArgError(2, "ID table must have ns and name fields")
			return 0
		}

		policyID = registry.ID{
			NS:   ns.String(),
			Name: name.String(),
		}
	case lua.LTNil, lua.LTBool, lua.LTNumber, lua.LTFunction, lua.LTThread, lua.LTChannel:
		// FIXME rework on demand
		fallthrough
	default:
		l.ArgError(2, "Policy ID expected as string, table, or policy object")
		return 0
	}

	l.Push(lua.LBool(scope.Contains(policyID)))
	return 1
}

// scopePolicies returns all policies in the scope
func scopePolicies(l *lua.LState) int {
	scope := checkScope(l)
	if scope == nil {
		return 0
	}

	policies := scope.Policies()

	// Convert policies to Lua table
	policiesTable := l.CreateTable(len(policies), 0)
	for i, policy := range policies {
		policyUD := wrapPolicy(l, policy)
		policiesTable.RawSetInt(i+1, policyUD)
	}

	l.Push(policiesTable)
	return 1
}
