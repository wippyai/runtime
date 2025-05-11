package security

import (
	secapi "github.com/ponyruntime/pony/api/security"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

const PolicyMetatable = "security.Policy"

// wrapPolicy wraps a security.Policy as a Lua userdata
func wrapPolicy(l *lua.LState, policy secapi.Policy) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = policy
	ud.Metatable = value.GetTypeMetatable(l, PolicyMetatable)
	return ud
}

// checkPolicy checks if the first argument is a Policy and returns it
func checkPolicy(l *lua.LState) secapi.Policy {
	ud := l.CheckUserData(1)
	if policy, ok := ud.Value.(secapi.Policy); ok {
		return policy
	}
	l.ArgError(1, "Policy expected")
	return nil
}

// registerPolicyType registers the Policy type and methods
func registerPolicyType(l *lua.LState) {
	value.RegisterMethods(l, PolicyMetatable, map[string]lua.LGFunction{
		"id":       policyID,
		"evaluate": policyEvaluate,
	})
}

// policyID returns the policy's id
func policyID(l *lua.LState) int {
	policy := checkPolicy(l)
	if policy == nil {
		return 0
	}

	l.Push(lua.LString(policy.ID().String()))
	return 1
}

// policyEvaluate evaluates if an action is allowed by this policy
func policyEvaluate(l *lua.LState) int {
	policy := checkPolicy(l)
	if policy == nil {
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
	result := policy.Evaluate(actor, action, resourceStr, meta)

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
