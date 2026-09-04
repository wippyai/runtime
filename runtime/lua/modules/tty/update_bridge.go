// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	ttyapi "github.com/wippyai/runtime/api/tty"
	"github.com/wippyai/runtime/runtime/lua/engine"
)

var viewportSubscriptionID atomic.Uint64

// updateBridge is the scheduler boundary for a viewport's native update
// watermarks. The Viewport userdata owns it, and closing either the Lua
// subscription or the userdata stops its producer goroutine exactly once.
type updateBridge struct {
	proc  *engine.Process
	ch    *engine.Channel
	value *lua.LUserData
	done  chan struct{}
	once  sync.Once
}

func newUpdateBridge(l *lua.LState, view ttyapi.Viewport) (*updateBridge, error) {
	proc := engine.GetProcess(l)
	target, ok := runtime.GetFramePID(l.Context())
	router := relay.GetNode(l.Context())
	if proc == nil || !ok || router == nil {
		return nil, fmt.Errorf("viewport update stream unavailable")
	}
	topic := fmt.Sprintf("@tty/viewport/%d", viewportSubscriptionID.Add(1))
	ch, subID, generation, err := proc.SubscribeRouted(topic, 1)
	if err != nil {
		return nil, err
	}
	ack := make(chan struct{}, 1)
	proc.SetTopicHandler(topic, viewportUpdateHandler(ack))
	value := engine.PushChannel(l, ch)
	l.Pop(1)
	bridge := &updateBridge{proc: proc, ch: ch, value: value, done: make(chan struct{})}
	if !proc.SetSubscriptionCleanup(ch, bridge.stop) {
		proc.UnsubscribeChannel(ch)
		return nil, fmt.Errorf("viewport update subscription unavailable")
	}
	go bridge.run(view.Updates(), router, target, topic, subID, generation, ack)
	return bridge, nil
}

func (b *updateBridge) stop() { b.once.Do(func() { close(b.done) }) }

func (b *updateBridge) close() {
	b.stop()
	b.proc.UnsubscribeChannel(b.ch)
}

func (b *updateBridge) run(updates <-chan ttyapi.Update, router relay.Receiver, target pid.PID, topic string, subID uint64, generation *atomic.Uint64, ack <-chan struct{}) {
	send := func(update ttyapi.Update) bool {
		frame := &engine.SubscriptionFrame{
			Payloads: []payload.Payload{payload.New(&update)}, Epoch: b.proc.Epoch(),
			SubID: subID, Gen: generation.Load(),
		}
		pkg := relay.AcquirePackage()
		pkg.Target = target
		pkg.AddMessage(topic, engine.NewSubscriptionFramePayload(frame))
		if err := router.Send(pkg); err != nil {
			relay.ReleasePackage(pkg)
			return false
		}
		return true
	}
	for {
		select {
		case update, open := <-updates:
			if !open {
				finishUpdateBridge(router, target, topic)
				return
			}
			if !send(update) {
				return
			}
			if !b.waitForAck(updates, ack, send, func() { finishUpdateBridge(router, target, topic) }) {
				return
			}
		case <-b.done:
			return
		}
	}
}

func (b *updateBridge) waitForAck(updates <-chan ttyapi.Update, ack <-chan struct{}, send func(ttyapi.Update) bool, finish func()) bool {
	var latest ttyapi.Update
	dirty := false
	for {
		select {
		case next, open := <-updates:
			if !open {
				// The broker closed the native stream independently of the Lua
				// userdata. Close its scheduler subscription through the relay.
				// This path cannot call UnsubscribeChannel from a producer goroutine.
				finish()
				return false
			}
			latest, dirty = next, true
		case <-ack:
			if !dirty {
				return true
			}
			if !send(latest) {
				return false
			}
			dirty = false
		case <-b.done:
			return false
		}
	}
}

func finishUpdateBridge(router relay.Receiver, target pid.PID, topic string) {
	pkg := relay.AcquirePackage()
	pkg.Target = target
	pkg.AddMessage(topic, payload.NewTerminal())
	if err := router.Send(pkg); err != nil {
		relay.ReleasePackage(pkg)
	}
}

func viewportUpdateHandler(ack chan<- struct{}) engine.TopicHandler {
	return func(_ context.Context, _ *lua.LState, _ pid.PID, _ string, payloads []payload.Payload) lua.LValue {
		if len(payloads) == 0 || payloads[0] == nil {
			return nil
		}
		update, ok := payloads[0].Data().(ttyapi.Update)
		if !ok {
			if ptr, ptrOK := payloads[0].Data().(*ttyapi.Update); ptrOK && ptr != nil {
				update, ok = *ptr, true
			}
		}
		if !ok {
			return nil
		}
		select {
		case ack <- struct{}{}:
		default:
		}
		return lua.LInteger(update.Revision)
	}
}
