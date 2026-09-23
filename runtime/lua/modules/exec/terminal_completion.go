// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	execapi "github.com/wippyai/runtime/api/service/exec"
	"github.com/wippyai/runtime/runtime/lua/engine"
)

var terminalCompletionID atomic.Uint64

// terminalResult is delivered only for the new one-call TerminalProcess API.
// The legacy TerminalSession continues to receive its boolean completion.
type terminalResult struct {
	Exit          *execapi.ExitStatus
	TerminalError error
}

// terminalCompletion owns the one-shot scheduler subscription that wakes Lua
// when an attached PTY process finishes.
type terminalCompletion struct {
	ctx    context.Context
	router relay.Receiver
	proc   *engine.Process
	ch     *engine.Channel
	value  *lua.LUserData
	target pid.PID
	topic  string
	once   sync.Once
}

func newTerminalCompletion(
	ctx context.Context,
	l *lua.LState,
	proc *engine.Process,
	router relay.Receiver,
	target pid.PID,
	cancel context.CancelFunc,
) (*terminalCompletion, error) {
	topic := fmt.Sprintf("@exec/terminal/completion/%d", terminalCompletionID.Add(1))
	ch := engine.NewChannel(1)
	if err := proc.SubscribeExisting(topic, ch); err != nil {
		return nil, err
	}
	proc.SetTopicHandler(topic, terminalCompletionHandler)
	value := engine.PushChannel(l, ch)
	l.Pop(1)
	if !proc.SetSubscriptionCleanup(ch, cancel) {
		proc.UnsubscribeChannel(ch)
		return nil, fmt.Errorf("terminal completion subscription unavailable")
	}
	return &terminalCompletion{
		ctx: ctx, router: router, target: target, topic: topic,
		proc: proc, ch: ch, value: value,
	}, nil
}

func (c *terminalCompletion) notify(result *terminalResult) {
	deliverTerminalCompletionResult(c.ctx, c.router, c.target, c.topic, result)
}

func deliverTerminalCompletion(ctx context.Context, router relay.Receiver, target pid.PID, topic string) {
	deliverTerminalCompletionResult(ctx, router, target, topic, nil)
}

func deliverTerminalCompletionResult(ctx context.Context, router relay.Receiver, target pid.PID, topic string, result *terminalResult) {
	select {
	case <-ctx.Done():
		return
	default:
	}
	pkg := relay.AcquirePackage()
	pkg.Target = target
	if result == nil {
		pkg.AddMessage(topic, payload.New(true), payload.NewTerminal())
	} else {
		pkg.AddMessage(topic, payload.New(result), payload.NewTerminal())
	}
	if err := router.Send(pkg); err != nil {
		relay.ReleasePackage(pkg)
	}
}

func (c *terminalCompletion) close() {
	c.once.Do(func() { c.proc.UnsubscribeChannel(c.ch) })
}

func terminalCompletionHandler(
	_ context.Context,
	l *lua.LState,
	_ pid.PID,
	_ string,
	payloads []payload.Payload,
) lua.LValue {
	if len(payloads) > 0 {
		if result, ok := payloads[0].Data().(*terminalResult); ok {
			out := l.CreateTable(0, 2)
			if result.Exit != nil {
				exit := l.CreateTable(0, 3)
				exit.RawSetString("code", lua.LInteger(result.Exit.Code))
				if result.Exit.Signal != 0 {
					exit.RawSetString("signal", lua.LInteger(result.Exit.Signal))
				}
				if result.Exit.Err != nil {
					exit.RawSetString("error", wrapExecError(l, result.Exit.Err, "process exit", lua.Internal))
				}
				out.RawSetString("exit", exit)
			}
			if result.TerminalError != nil {
				out.RawSetString("terminal_error", wrapExecError(l, result.TerminalError, "terminal", lua.Internal))
			}
			return out
		}
	}
	return lua.LTrue
}
