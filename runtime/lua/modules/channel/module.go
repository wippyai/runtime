package channel

import (
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine"
)

// Module is the channel module definition - delegates to engine.ChannelModule.
var Module = &luaapi.ModuleDef{
	Name:        engine.ChannelModule.Name,
	Description: engine.ChannelModule.Description,
	Class:       []string{luaapi.ClassProcess, luaapi.ClassNondeterministic},
	Build:       engine.ChannelModule.Build,
}
