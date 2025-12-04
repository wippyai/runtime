package channel

import (
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
)

func init() {
	engine.RegisterChannelMetatable()
}

// Module is the channel module definition.
var Module = &luaapi.ModuleDef{
	Name:        "channel",
	Description: "Channel communication primitives",
	Class:       []string{luaapi.ClassProcess, luaapi.ClassNondeterministic},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		return engine.GetChannelModuleTable(), nil
	},
}
