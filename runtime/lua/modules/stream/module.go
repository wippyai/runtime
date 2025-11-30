package stream

import (
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	streamapi "github.com/wippyai/runtime/api/dispatcher/stream"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua2api "github.com/wippyai/runtime/api/runtime/lua2"
	lua "github.com/yuin/gopher-lua"
)

var (
	moduleTable  *lua.LTable
	registration *lua2api.Registration
	initOnce     sync.Once
)

// Module is the singleton stream module instance.
var Module = &streamModule{}

type streamModule struct{}

func (m *streamModule) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "stream",
		Description: "Stream read/write operations",
		Class:       []string{luaapi.ClassIO, luaapi.ClassNondeterministic},
	}
}

func (m *streamModule) Register(l *lua.LState) *lua2api.Registration {
	initOnce.Do(func() {
		registerStreamMetatable()

		mod := lua.CreateTable(0, 0)
		mod.Immutable = true
		moduleTable = mod

		registration = &lua2api.Registration{
			Table: moduleTable,
			YieldTypes: []lua2api.YieldType{
				{Sample: &StreamReadYield{}, CmdID: dispatcher.CommandID(streamapi.CmdStreamRead)},
				{Sample: &StreamWriteYield{}, CmdID: dispatcher.CommandID(streamapi.CmdStreamWrite)},
				{Sample: &StreamSeekYield{}, CmdID: dispatcher.CommandID(streamapi.CmdStreamSeek)},
				{Sample: &StreamFlushYield{}, CmdID: dispatcher.CommandID(streamapi.CmdStreamFlush)},
				{Sample: &StreamStatYield{}, CmdID: dispatcher.CommandID(streamapi.CmdStreamStat)},
				{Sample: &StreamCloseYield{}, CmdID: dispatcher.CommandID(streamapi.CmdStreamClose)},
			},
		}
	})

	BindStream(l)

	return registration
}

func (m *streamModule) Loader(l *lua.LState) int {
	reg := m.Register(l)
	l.Push(reg.Table)
	return 1
}
