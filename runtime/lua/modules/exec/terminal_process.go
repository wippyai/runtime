// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"sync"
	"sync/atomic"

	lua "github.com/wippyai/go-lua"
	execapi "github.com/wippyai/runtime/api/service/exec"
	ttyapi "github.com/wippyai/runtime/api/tty"
	luatty "github.com/wippyai/runtime/runtime/lua/modules/tty"
	"github.com/wippyai/runtime/service/terminal/proxy"
)

var terminalProcessMethods = map[string]lua.LGoFunc{
	"send":   terminalProcessSend,
	"close":  terminalProcessClose,
	"done":   terminalProcessDone,
	"pid":    terminalProcessPID,
	"status": terminalProcessStatus,
}

type terminalProcess struct {
	err        error
	events     chan ttyapi.Event
	completion *terminalCompletion
	bridge     *proxy.Proxy
	identity   execapi.ProcessIdentity
	errMu      sync.RWMutex
	once       sync.Once
	done       atomic.Bool
}

func newTerminalProcess(bridge *proxy.Proxy, completion *terminalCompletion, identity execapi.ProcessIdentity) *terminalProcess {
	return &terminalProcess{
		events: make(chan ttyapi.Event, 256), bridge: bridge, completion: completion,
		identity: identity,
	}
}

func (s *terminalProcess) complete(result proxy.Result) {
	s.errMu.Lock()
	s.err = result.Err
	s.errMu.Unlock()
	s.done.Store(true)
	s.completion.notify(&terminalResult{Exit: result.Exit, TerminalError: result.TerminalError})
}

func checkTerminalProcess(l *lua.LState) *terminalProcess {
	ud := l.CheckUserData(1)
	if session, ok := ud.Value.(*terminalProcess); ok {
		return session
	}
	l.ArgError(1, "exec.TerminalProcess expected")
	return nil
}

func terminalProcessSend(l *lua.LState) int {
	session := checkTerminalProcess(l)
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
			WithKind(lua.Unavailable).WithRetryable(true))
		return 2
	}
	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func terminalProcessClose(l *lua.LState) int {
	session := checkTerminalProcess(l)
	session.once.Do(func() {
		if !session.done.Load() {
			session.bridge.RequestClose()
		}
	})
	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func terminalProcessDone(l *lua.LState) int {
	l.Push(checkTerminalProcess(l).completion.value)
	return 1
}

func terminalProcessPID(l *lua.LState) int {
	session := checkTerminalProcess(l)
	if session.identity == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "process has no host process id").WithKind(lua.Unavailable).WithRetryable(false))
		return 2
	}
	pid, err := session.identity.Pid()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(wrapExecError(l, err, "read process id", lua.Internal))
		return 2
	}
	l.Push(lua.LInteger(pid))
	l.Push(lua.LNil)
	return 2
}

func terminalProcessStatus(l *lua.LState) int {
	session := checkTerminalProcess(l)
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
		l.Push(wrapExecError(l, err, "PTY process", lua.Internal))
	} else {
		l.Push(lua.LNil)
	}
	return 2
}

func terminalProcessGC(l *lua.LState) int {
	_ = terminalProcessClose(l)
	l.Pop(2)
	return 0
}
