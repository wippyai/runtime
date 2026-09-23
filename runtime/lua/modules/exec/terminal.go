// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"context"
	"errors"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	execapi "github.com/wippyai/runtime/api/service/exec"
	ttyapi "github.com/wippyai/runtime/api/tty"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/service/terminal/proxy"
)

type terminalAttachment struct {
	ctx        context.Context
	cancel     context.CancelFunc
	completion *terminalCompletion
	surface    ttyapi.Surface
	width      int
	height     int
}

func openTerminalAttachment(l *lua.LState) (*terminalAttachment, bool) {
	luaProcess := engine.GetProcess(l)
	target, hasPID := runtime.GetFramePID(l.Context())
	router := relay.GetNode(l.Context())
	if luaProcess == nil || !hasPID || router == nil {
		pushTerminalError(l, nil, "terminal completion relay unavailable")
		return nil, false
	}
	port, err := ttyapi.GetPort(l.Context())
	if err != nil || port == nil {
		pushTerminalError(l, err, "terminal port unavailable")
		return nil, false
	}
	input := port.InputController()
	if input == nil {
		pushTerminalError(l, nil, "terminal input unavailable")
		return nil, false
	}
	width, height, err := input.ScreenSize()
	if err != nil {
		pushTerminalError(l, err, "read terminal size")
		return nil, false
	}
	if err := execapi.ValidatePTYSize(width, height); err != nil {
		pushTerminalError(l, err, "read terminal size")
		return nil, false
	}
	surface, err := port.OpenSurface(ttyapi.SurfaceOptions{})
	if err != nil {
		pushTerminalError(l, err, "open terminal surface")
		return nil, false
	}
	ctx, cancel := context.WithCancel(l.Context())
	completion, err := newTerminalCompletion(ctx, l, luaProcess, router, target, cancel)
	if err != nil {
		cancel()
		_ = surface.Close()
		pushTerminalError(l, err, "subscribe terminal completion")
		return nil, false
	}
	return &terminalAttachment{ctx: ctx, cancel: cancel, completion: completion, surface: surface, width: width, height: height}, true
}

func (a *terminalAttachment) close() {
	a.cancel()
	a.completion.close()
	_ = a.surface.Close()
}

func (a *terminalAttachment) run(bridge *proxy.Proxy, session *terminalSession, ready chan<- error) {
	var proxyReady chan error
	var finished chan struct{}
	if ready != nil {
		proxyReady = make(chan error, 1)
		finished = make(chan struct{})
		go func() {
			err := <-proxyReady
			if err != nil {
				// A failed constructor must not return until the leased surface
				// and any started child have been cleaned up.
				<-finished
			}
			ready <- err
		}()
	}
	go func() {
		var result proxy.Result
		if ready == nil {
			result.Err = bridge.Run(a.ctx, session.events)
		} else {
			result = bridge.RunWithReady(a.ctx, session.events, proxyReady)
		}
		if err := a.surface.Close(); err != nil {
			result.Err = errors.Join(result.Err, err)
			result.TerminalError = errors.Join(result.TerminalError, err)
		}
		if finished != nil {
			close(finished)
		}
		session.complete(result)
	}()
}

// procAttachTerminal consumes an unstarted PTY-backed process and attaches it
// to the current process terminal. The returned session owns its lifecycle.
func procAttachTerminal(l *lua.LState) int {
	a, ok := openTerminalAttachment(l)
	if !ok {
		return 2
	}
	process, err := takePTYProcess(l.CheckAny(1))
	if err != nil {
		a.close()
		pushTerminalError(l, err, "acquire PTY process")
		return 2
	}
	identity, _ := process.(execapi.ProcessIdentity)
	bridge, err := proxy.New(process, a.surface, a.width, a.height)
	if err != nil {
		a.close()
		pushTerminalError(l, err, "create terminal proxy")
		return 2
	}
	session := newTerminalSession(bridge, a.completion, identity)
	a.run(bridge, session, nil)
	value.PushTypedUserData(l, session, terminalSessionTypeName)
	l.Push(lua.LNil)
	return 2
}

// executorTerminal creates a PTY-backed host process directly in the current
// terminal. The Lua call yields until startup and initial PTY setup succeed.
func executorTerminal(l *lua.LState) int {
	e := checkExecutor(l, 1)
	if e == nil {
		return 0
	}
	// Validate Lua arguments before taking the exclusive surface lease; a Lua
	// argument error unwinds the call without running ordinary error cleanup.
	l.CheckString(2)
	a, ok := openTerminalAttachment(l)
	if !ok {
		return 2
	}
	process, ok := createAuthorizedProcess(l, e, true, a.width, a.height)
	if !ok {
		a.close()
		return 2
	}
	ptyProcess, ok := process.(execapi.PTYProcess)
	if !ok {
		a.close()
		if stopper, canStop := process.(interface{ Stop() }); canStop {
			stopper.Stop()
		}
		pushTerminalError(l, execapi.ErrPTYUnavailable, "acquire PTY process")
		return 2
	}
	bridge, err := proxy.New(ptyProcess, a.surface, a.width, a.height)
	if err != nil {
		a.close()
		if stopper, canStop := process.(interface{ Stop() }); canStop {
			stopper.Stop()
		}
		pushTerminalError(l, err, "create terminal proxy")
		return 2
	}
	identity, _ := process.(execapi.ProcessIdentity)
	session := newTerminalSession(bridge, a.completion, identity)
	session.structured = true
	ready := make(chan error, 1)
	a.run(bridge, session, ready)
	l.Push(&TerminalReadyYield{Ready: ready, Session: session, Completion: a.completion})
	return -1
}

func pushTerminalError(l *lua.LState, err error, message string) {
	l.Push(lua.LNil)
	if err != nil {
		l.Push(wrapExecError(l, err, message, lua.Unavailable))
		return
	}
	l.Push(lua.NewLuaError(l, message).WithKind(lua.Unavailable).WithRetryable(false))
}
