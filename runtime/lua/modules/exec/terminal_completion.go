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
	"github.com/wippyai/runtime/runtime/lua/engine"
)

var terminalCompletionID atomic.Uint64

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

func (c *terminalCompletion) notify() {
	deliverTerminalCompletion(c.ctx, c.router, c.target, c.topic)
}

func deliverTerminalCompletion(ctx context.Context, router relay.Receiver, target pid.PID, topic string) {
	select {
	case <-ctx.Done():
		return
	default:
	}
	pkg := relay.AcquirePackage()
	pkg.Target = target
	pkg.AddMessage(topic, payload.New(true), payload.NewTerminal())
	if err := router.Send(pkg); err != nil {
		relay.ReleasePackage(pkg)
	}
}

func (c *terminalCompletion) close() {
	c.once.Do(func() { c.proc.UnsubscribeChannel(c.ch) })
}

func terminalCompletionHandler(
	_ context.Context,
	_ *lua.LState,
	_ pid.PID,
	_ string,
	_ []payload.Payload,
) lua.LValue {
	return lua.LTrue
}
