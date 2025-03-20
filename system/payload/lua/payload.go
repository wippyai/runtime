package lua

import (
	"github.com/ponyruntime/pony/api/payload"
	lua "github.com/yuin/gopher-lua"
)

func ExportLuaValue(value lua.LValue) payload.Payload {
	return payload.NewPayload(value, payload.Lua)
}
