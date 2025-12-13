package stream

import (
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	streamapi "github.com/wippyai/runtime/api/stream"
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
	registerScannerMetatable()

	mod := lua.CreateTable(0, 0)
	mod.Immutable = true

	yields := []luaapi.YieldType{
		{Sample: &ReadYield{}, CmdID: streamapi.CmdRead},
		{Sample: &WriteYield{}, CmdID: streamapi.CmdWrite},
		{Sample: &SeekYield{}, CmdID: streamapi.CmdSeek},
		{Sample: &FlushYield{}, CmdID: streamapi.CmdFlush},
		{Sample: &StatYield{}, CmdID: streamapi.CmdStat},
		{Sample: &CloseYield{}, CmdID: streamapi.CmdClose},
		{Sample: &ScannerCreateYield{}, CmdID: streamapi.CmdScannerCreate},
		{Sample: &ScannerScanYield{}, CmdID: streamapi.CmdScannerScan},
	}

	return mod, yields
}
