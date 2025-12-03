package registry

import (
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

func newRegistryOperationError(l *lua.LState, err error, operation string) lua.LValue {
	engErr := engine.NewOperationError(operation, err)
	ud := value.PushTypedUserData(l, engErr, "error")
	if ud != nil {
		return ud
	}
	return lua.LString(err.Error())
}
