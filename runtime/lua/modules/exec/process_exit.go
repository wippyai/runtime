// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"context"
	"fmt"
	"sync/atomic"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	apiexec "github.com/wippyai/runtime/api/service/exec"
	"github.com/wippyai/runtime/runtime/lua/engine"
)

var processExitID atomic.Uint64

// procDone returns a one-shot channel carrying the child's exit.
//
// It is the observation half of wait(): the exit arrives on a channel the
// caller can select on, and the handle stays open, so a supervisor can keep
// writing stdin and signaling the child while it waits for the exit. Draining
// stdout is not a substitute -- a grandchild can hold the pipe open long after
// the child itself is gone.
func procDone(l *lua.LState) int {
	p := checkProcess(l, 1)
	if p == nil {
		return 0
	}

	p.mu.Lock()
	if existing := p.doneChannel; existing != nil {
		p.mu.Unlock()
		l.Push(existing)
		l.Push(lua.LNil)
		return 2
	}
	if p.closed || p.handle == nil {
		p.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "process is closed").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	if !p.started {
		p.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "process not started: call start() first").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	handle := p.handle
	p.mu.Unlock()

	luaProcess := engine.GetProcess(l)
	target, hasPID := runtime.GetFramePID(l.Context())
	router := relay.GetNode(l.Context())
	if luaProcess == nil || !hasPID || router == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "process exit relay unavailable").WithKind(lua.Unavailable).WithRetryable(false))
		return 2
	}

	topic := fmt.Sprintf("@exec/process/exit/%d", processExitID.Add(1))
	ch := engine.NewChannel(1)
	if err := luaProcess.SubscribeExisting(topic, ch); err != nil {
		l.Push(lua.LNil)
		l.Push(wrapExecError(l, err, "subscribe process exit", lua.Internal))
		return 2
	}
	luaProcess.SetTopicHandler(topic, processExitHandler)
	channel := engine.PushChannel(l, ch)
	l.Pop(1)

	ctx, cancel := context.WithCancel(l.Context())
	if !luaProcess.SetSubscriptionCleanup(ch, cancel) {
		luaProcess.UnsubscribeChannel(ch)
		cancel()
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "process exit subscription unavailable").WithKind(lua.Unavailable).WithRetryable(false))
		return 2
	}

	p.mu.Lock()
	p.doneChannel = channel
	p.mu.Unlock()

	go func() {
		deliverProcessExit(ctx, router, target, topic, p.reap(handle))
	}()

	l.Push(channel)
	l.Push(lua.LNil)
	return 2
}

// deliverProcessExit hands the exit to the waiting Lua process. The terminal
// payload closes the channel behind the single value, so a second receive
// reports the channel is finished instead of blocking forever.
func deliverProcessExit(
	ctx context.Context,
	router relay.Receiver,
	target pid.PID,
	topic string,
	status apiexec.ExitStatus,
) {
	select {
	case <-ctx.Done():
		return
	default:
	}
	pkg := relay.AcquirePackage()
	pkg.Target = target
	pkg.AddMessage(topic, payload.New(&status), payload.NewTerminal())
	if err := router.Send(pkg); err != nil {
		relay.ReleasePackage(pkg)
	}
}

func processExitHandler(
	_ context.Context,
	l *lua.LState,
	_ pid.PID,
	_ string,
	payloads []payload.Payload,
) lua.LValue {
	if len(payloads) == 0 {
		return lua.LNil
	}
	status, ok := payloads[0].Data().(*apiexec.ExitStatus)
	if !ok {
		return lua.LNil
	}

	exit := l.CreateTable(0, 3)
	exit.RawSetString("code", lua.LInteger(status.Code))
	if status.Signal != 0 {
		exit.RawSetString("signal", lua.LInteger(status.Signal))
	}
	if status.Err != nil {
		exit.RawSetString("error", wrapExecError(l, status.Err, "process exit", lua.Internal))
	}
	return exit
}
