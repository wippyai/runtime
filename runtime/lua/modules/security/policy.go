package security

import (
	secapi "github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

const policyTypeName = "security.Policy"

var policyMethods = map[string]lua.LGoFunc{
	"id":       policyID,
	"evaluate": policyEvaluate,
}

func wrapPolicy(l *lua.LState, policy secapi.Policy) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = policy
	ud.Metatable = value.GetTypeMetatable(l, policyTypeName)
	return ud
}

func checkPolicy(l *lua.LState, idx int) secapi.Policy {
	ud := l.CheckUserData(idx)
	if policy, ok := ud.Value.(secapi.Policy); ok {
		return policy
	}
	l.ArgError(idx, "Policy expected")
	return nil
}

func policyID(l *lua.LState) int {
	policy := checkPolicy(l, 1)
	if policy == nil {
		return 0
	}
	l.Push(lua.LString(policy.ID().String()))
	return 1
}

func policyEvaluate(l *lua.LState) int {
	policy := checkPolicy(l, 1)
	if policy == nil {
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

	result := policy.Evaluate(actor, action, resource, meta)

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
