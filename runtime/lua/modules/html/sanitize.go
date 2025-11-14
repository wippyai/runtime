package html

import (
	"github.com/microcosm-cc/bluemonday"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

func newPolicy(l *lua.LState) int {
	policy := bluemonday.NewPolicy()

	wrapper := &PolicyWrapper{policy: policy}
	ud := l.NewUserData()
	ud.Value = wrapper
	ud.Metatable = value.GetTypeMetatable(l, policyMetatable)

	l.Push(ud)
	l.Push(lua.LNil)
	return 2
}

func ugcPolicy(l *lua.LState) int {
	policy := bluemonday.UGCPolicy()

	wrapper := &PolicyWrapper{policy: policy}
	ud := l.NewUserData()
	ud.Value = wrapper
	ud.Metatable = value.GetTypeMetatable(l, policyMetatable)

	l.Push(ud)
	l.Push(lua.LNil)
	return 2
}

func strictPolicy(l *lua.LState) int {
	policy := bluemonday.StrictPolicy()

	wrapper := &PolicyWrapper{policy: policy}
	ud := l.NewUserData()
	ud.Value = wrapper
	ud.Metatable = value.GetTypeMetatable(l, policyMetatable)

	l.Push(ud)
	l.Push(lua.LNil)
	return 2
}
