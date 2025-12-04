package stream

import (
	"github.com/wippyai/runtime/api/dispatcher"
	streamapi "github.com/wippyai/runtime/api/dispatcher/stream"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua "github.com/yuin/gopher-lua"
)

// Module is the stream module definition.
var Module = &luaapi.ModuleDef{
	Name:        "stream",
	Description: "Stream read/write operations",
	Class:       []string{luaapi.ClassIO, luaapi.ClassNondeterministic},
	Build:       buildModule,
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
	registerStreamMetatable()

	mod := lua.CreateTable(0, 0)
	mod.Immutable = true

	yields := []luaapi.YieldType{
		{Sample: &ReadYield{}, CmdID: dispatcher.CommandID(streamapi.CmdRead)},
		{Sample: &WriteYield{}, CmdID: dispatcher.CommandID(streamapi.CmdWrite)},
		{Sample: &SeekYield{}, CmdID: dispatcher.CommandID(streamapi.CmdSeek)},
		{Sample: &FlushYield{}, CmdID: dispatcher.CommandID(streamapi.CmdFlush)},
		{Sample: &StatYield{}, CmdID: dispatcher.CommandID(streamapi.CmdStat)},
		{Sample: &CloseYield{}, CmdID: dispatcher.CommandID(streamapi.CmdClose)},
	}

	return mod, yields
}
