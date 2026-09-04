// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"sync"
	"sync/atomic"

	lua "github.com/wippyai/go-lua"
	ttyapi "github.com/wippyai/runtime/api/tty"
	luatty "github.com/wippyai/runtime/runtime/lua/modules/tty"
	"github.com/wippyai/runtime/service/terminal/proxy"
)

const terminalSessionTypeName = "exec.TerminalSession"

var terminalSessionMethods = map[string]lua.LGoFunc{
	"send":   terminalSessionSend,
	"close":  terminalSessionClose,
	"done":   terminalSessionDone,
	"status": terminalSessionStatus,
}

type terminalSession struct {
	err        error
	events     chan ttyapi.Event
	completion *terminalCompletion
	bridge     *proxy.Proxy
	errMu      sync.RWMutex
	once       sync.Once
	done       atomic.Bool
}

func newTerminalSession(bridge *proxy.Proxy, completion *terminalCompletion) *terminalSession {
	return &terminalSession{
		events: make(chan ttyapi.Event, 256), bridge: bridge, completion: completion,
	}
}

func (s *terminalSession) complete(err error) {
	s.errMu.Lock()
	s.err = err
	s.errMu.Unlock()
	s.done.Store(true)
	s.completion.notify()
}

func checkTerminalSession(l *lua.LState) *terminalSession {
	ud := l.CheckUserData(1)
	if session, ok := ud.Value.(*terminalSession); ok {
		return session
	}
	l.ArgError(1, "exec.TerminalSession expected")
	return nil
}

func terminalSessionSend(l *lua.LState) int {
	session := checkTerminalSession(l)
	if session.done.Load() {
		pushTerminalError(l, nil, "PTY process is not running")
		return 2
	}
	event, err := luatty.DecodeEvent(l.CheckTable(2))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, err.Error()).WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	if err := enqueueTerminalEvent(session.events, event); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "send terminal input").
			WithKind(lua.Internal).WithRetryable(true))
		return 2
	}
	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func terminalSessionClose(l *lua.LState) int {
	session := checkTerminalSession(l)
	session.once.Do(func() {
		if !session.done.Load() {
			session.bridge.RequestClose()
		}
	})
	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func terminalSessionDone(l *lua.LState) int {
	l.Push(checkTerminalSession(l).completion.value)
	return 1
}

func terminalSessionStatus(l *lua.LState) int {
	session := checkTerminalSession(l)
	if !session.done.Load() {
		l.Push(lua.LString("running"))
		l.Push(lua.LNil)
		return 2
	}
	session.errMu.RLock()
	err := session.err
	session.errMu.RUnlock()
	l.Push(lua.LString("done"))
	if err != nil {
		l.Push(lua.WrapErrorWithLua(l, err, "PTY process"))
	} else {
		l.Push(lua.LNil)
	}
	return 2
}

func terminalSessionGC(l *lua.LState) int {
	_ = terminalSessionClose(l)
	l.Pop(2)
	return 0
}
