// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"context"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	ttyapi "github.com/wippyai/runtime/api/tty"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/service/terminal/proxy"
)

// procAttachTerminal consumes an unstarted PTY-backed process and attaches it
// to the current process terminal. The returned session owns its lifecycle.
func procAttachTerminal(l *lua.LState) int {
	luaProcess := engine.GetProcess(l)
	target, hasPID := runtime.GetFramePID(l.Context())
	router := relay.GetNode(l.Context())
	if luaProcess == nil || !hasPID || router == nil {
		pushTerminalError(l, nil, "terminal completion relay unavailable")
		return 2
	}
	port, err := ttyapi.GetPort(l.Context())
	if err != nil || port == nil {
		pushTerminalError(l, err, "terminal port unavailable")
		return 2
	}
	input := port.InputController()
	if input == nil {
		pushTerminalError(l, nil, "terminal input unavailable")
		return 2
	}
	width, height, err := input.ScreenSize()
	if err != nil {
		pushTerminalError(l, err, "read terminal size")
		return 2
	}
	if width < 1 || height < 1 {
		pushTerminalError(l, proxy.ErrInvalidProxy, "read terminal size")
		return 2
	}
	surface, err := port.OpenSurface(ttyapi.SurfaceOptions{})
	if err != nil {
		pushTerminalError(l, err, "open terminal surface")
		return 2
	}
	ctx, cancel := context.WithCancel(l.Context())
	completion, err := newTerminalCompletion(ctx, l, luaProcess, router, target, cancel)
	if err != nil {
		cancel()
		_ = surface.Close()
		pushTerminalError(l, err, "subscribe terminal completion")
		return 2
	}
	process, err := takePTYProcess(l.CheckAny(1))
	if err != nil {
		completion.close()
		_ = surface.Close()
		pushTerminalError(l, err, "acquire PTY process")
		return 2
	}
	bridge, err := proxy.New(process, surface, width, height)
	if err != nil {
		completion.close()
		_ = surface.Close()
		pushTerminalError(l, err, "create terminal proxy")
		return 2
	}
	session := newTerminalSession(bridge, completion)
	go func() {
		err := bridge.Run(ctx, session.events)
		_ = surface.Close()
		session.complete(err)
	}()
	value.PushTypedUserData(l, session, terminalSessionTypeName)
	l.Push(lua.LNil)
	return 2
}

func pushTerminalError(l *lua.LState, err error, message string) {
	l.Push(lua.LNil)
	if err != nil {
		l.Push(lua.WrapErrorWithLua(l, err, message))
		return
	}
	l.Push(lua.NewLuaError(l, message).WithKind(lua.Unavailable).WithRetryable(false))
}
