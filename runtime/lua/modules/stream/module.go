// SPDX-License-Identifier: MPL-2.0

package stream

import (
	lua "github.com/wippyai/go-lua"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	streamapi "github.com/wippyai/runtime/api/stream"
)

// Module is the stream module definition.
var Module = &luaapi.ModuleDef{
	Name:        "stream",
	Description: "Stream read/write operations",
	Class:       []string{luaapi.ClassIO, luaapi.ClassNondeterministic},
	Build:       buildModule,
	Types:       ModuleTypes,
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
	registerStreamMetatable()
	registerScannerMetatable()

	mod := lua.CreateTable(0, 0)
	mod.Immutable = true

	yields := []luaapi.YieldType{
		{Sample: &ReadYield{}, CmdID: streamapi.Read},
		{Sample: &WriteYield{}, CmdID: streamapi.Write},
		{Sample: &SeekYield{}, CmdID: streamapi.Seek},
		{Sample: &FlushYield{}, CmdID: streamapi.Flush},
		{Sample: &StatYield{}, CmdID: streamapi.Stat},
		{Sample: &CloseYield{}, CmdID: streamapi.Close},
		{Sample: &ScannerCreateYield{}, CmdID: streamapi.ScannerCreate},
		{Sample: &ScannerScanYield{}, CmdID: streamapi.ScannerScan},
	}

	return mod, yields
}
