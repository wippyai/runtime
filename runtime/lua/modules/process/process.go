package process

import (
	"fmt"

	"github.com/ponyruntime/pony/runtime/lua/modules/stream"
	lua "github.com/yuin/gopher-lua"
)

// todo: i think we need to rename this method according to what stream pipe we connected to
func (m *Module) readProcess(l *lua.LState) int {
	log := getCtxLogger(l)
	executor := getProcessExecutor(l)
	log.Debug("reading process output")

	// Create stream
	ctx := l.Context()
	// cleanup := closer.FromContext(ctx)
	// if cleanup == nil {
	// 	ctx, c := closer.WithContext(ctx)
	// 	cleanup = c
	// }

	s, err := stream.NewStream(ctx, executor.StderrReader(), stream.NewStreamConfig(65535))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to create stream: %v", err)))
		return 2
	}

	// Create Lua stream object
	luaStream := &stream.LuaStream{Stream: s}
	ud := l.NewUserData()
	ud.Value = luaStream
	l.SetMetatable(ud, l.GetTypeMetatable("Stream"))

	l.Push(ud)
	l.Push(lua.LNil)
	return 2
}
