// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"sync/atomic"
	"testing"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
)

func newSubscriptionMatrixProcess(t *testing.T) *Process {
	t.Helper()

	proto, err := lua.CompileString(`return "ok"`, "subscription_matrix.lua")
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	proc := mustNewProcess(t, WithProto(proto))
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	return proc
}

func addMatrixSubscription(t *testing.T, proc *Process, topic string) (*Channel, *subscription) {
	t.Helper()
	ch := NewChannel(1)
	if err := proc.SubscribeExisting(topic, ch); err != nil {
		t.Fatalf("SubscribeExisting(%q): %v", topic, err)
	}
	proc.subs.mu.RLock()
	sub := proc.subs.byTopic[topic]
	proc.subs.mu.RUnlock()
	if sub == nil {
		t.Fatalf("missing subscription for %q", topic)
	}
	return ch, sub
}

func subscriptionFrameMessage(topic string, epoch, subID, gen uint64, payloads payload.Payloads) queuedMessage {
	return queuedMessage{
		Topic: topic,
		Payloads: payload.Payloads{NewSubscriptionFramePayload(&SubscriptionFrame{
			Epoch:    epoch,
			SubID:    subID,
			Gen:      gen,
			Payloads: payloads,
		})},
	}
}

// oneShotFireMessage mirrors the clock dispatcher's one-shot timer fire:
// a single-value SubscriptionFrame payload followed by an outer terminal
// payload (value plus terminal). time.after / time.timer deliver exactly
// this shape.
func oneShotFireMessage(topic string, epoch, subID, gen uint64, value lua.LValue) queuedMessage {
	return queuedMessage{
		Topic: topic,
		Payloads: payload.Payloads{
			NewSubscriptionFramePayload(&SubscriptionFrame{
				Epoch:    epoch,
				SubID:    subID,
				Gen:      gen,
				Payloads: payload.Payloads{payload.NewPayload(value, payload.Lua)},
			}),
			payload.NewTerminal(),
		},
	}
}

// tickFireMessage mirrors the clock dispatcher's ticker fire: a single
// non-terminal SubscriptionFrame payload. time.ticker delivers this shape
// once per tick.
func tickFireMessage(topic string, epoch, subID, gen uint64, value lua.LValue) queuedMessage {
	return queuedMessage{
		Topic: topic,
		Payloads: payload.Payloads{
			NewSubscriptionFramePayload(&SubscriptionFrame{
				Epoch:    epoch,
				SubID:    subID,
				Gen:      gen,
				Payloads: payload.Payloads{payload.NewPayload(value, payload.Lua)},
			}),
		},
	}
}

func assertSubscriptionMatrixSizes(t *testing.T, proc *Process, topics, channels, handlers int) {
	t.Helper()
	proc.subs.mu.RLock()
	gotTopics := len(proc.subs.byTopic)
	gotChannels := len(proc.subs.byChannel)
	proc.subs.mu.RUnlock()
	if gotTopics != topics || gotChannels != channels {
		t.Fatalf("subscription maps = (%d topics, %d channels), want (%d, %d)", gotTopics, gotChannels, topics, channels)
	}
	if gotHandlers := len(proc.handlers); gotHandlers != handlers {
		t.Fatalf("handlers = %d, want %d", gotHandlers, handlers)
	}
}

func stringHandler(_ context.Context, _ *lua.LState, _ pid.PID, _ string, payloads []payload.Payload) lua.LValue {
	if len(payloads) == 0 {
		return lua.LNil
	}
	if s, ok := payloads[0].Data().(lua.LString); ok {
		return s
	}
	return lua.LNil
}

// TestSubscriptionLifecycleProofMatrix covers the process-side foundation
// cases for subscription cleanup, generation checks, id reuse, and close
// idempotence. Dispatcher-side map cleanup is covered in system/clock.
func TestSubscriptionLifecycleProofMatrix(t *testing.T) {
	// case2 fire+NEVER-read: a terminal one-shot frame closes and removes the
	// subscription even when Lua never receives from the channel.
	t.Run("case2 fire+NEVER-read", func(t *testing.T) {
		proc := newSubscriptionMatrixProcess(t)
		defer proc.Close()

		ch, sub := addMatrixSubscription(t, proc, "timer")
		epoch := proc.epoch.Load()
		if !proc.deliverMessage(proc.subs, subscriptionFrameMessage("timer", epoch, sub.id, sub.gen.Load(), payload.Payloads{
			payload.NewPayload(lua.LString("done"), payload.Lua),
			payload.NewTerminal(),
		})) {
			t.Fatal("terminal frame should be delivered")
		}
		if !ch.IsClosed() {
			t.Fatal("timer channel should be closed after terminal fire")
		}
		assertSubscriptionMatrixSizes(t, proc, 0, 0, 0)
	})

	// case6 reset old-arm dropped: bumping the subscription generation makes
	// an older frame consume-and-drop without closing the live subscription.
	t.Run("case6 reset old-arm dropped", func(t *testing.T) {
		proc := newSubscriptionMatrixProcess(t)
		defer proc.Close()

		ch, sub := addMatrixSubscription(t, proc, "timer")
		epoch := proc.epoch.Load()
		oldGen := sub.gen.Load()
		newGen, ok := proc.BumpSubscriptionGen(ch)
		if !ok || newGen == oldGen {
			t.Fatalf("BumpSubscriptionGen = (%d, %v), old=%d", newGen, ok, oldGen)
		}

		delivered := proc.deliverMessage(proc.subs, subscriptionFrameMessage("timer", epoch, sub.id, oldGen, payload.Payloads{
			payload.NewPayload(lua.LString("old"), payload.Lua),
		}))
		if !delivered {
			t.Fatal("stale frame should be consumed")
		}
		if ch.Size() != 0 {
			t.Fatalf("stale frame was delivered to channel, size=%d", ch.Size())
		}
		assertSubscriptionMatrixSizes(t, proc, 1, 1, 0)

		delivered = proc.deliverMessage(proc.subs, subscriptionFrameMessage("timer", epoch, sub.id, newGen, payload.Payloads{
			payload.NewPayload(lua.LString("new"), payload.Lua),
			payload.NewTerminal(),
		}))
		if !delivered {
			t.Fatal("current terminal frame should be delivered")
		}
		assertSubscriptionMatrixSizes(t, proc, 0, 0, 0)
	})

	// case9 pool-reuse stale fire dropped: an old epoch frame with a recycled
	// subID/topic must not close the new subscription.
	t.Run("case9 pool-reuse stale fire dropped", func(t *testing.T) {
		proc := newSubscriptionMatrixProcess(t)
		defer proc.Close()

		oldCh, oldSub := addMatrixSubscription(t, proc, "timer")
		oldEpoch := proc.epoch.Load()
		if !proc.closeChannel(oldCh) {
			t.Fatal("initial closeChannel should remove the old subscription")
		}
		proc.epoch.Add(1)
		proc.subs.nextID = 0
		newCh, newSub := addMatrixSubscription(t, proc, "timer")
		if newSub.id != oldSub.id {
			t.Fatalf("test requires recycled sub id, got old=%d new=%d", oldSub.id, newSub.id)
		}

		delivered := proc.deliverMessage(proc.subs, subscriptionFrameMessage("timer", oldEpoch, oldSub.id, oldSub.gen.Load(), payload.Payloads{
			payload.NewPayload(lua.LString("stale"), payload.Lua),
			payload.NewTerminal(),
		}))
		if !delivered {
			t.Fatal("old-epoch frame should be consumed")
		}
		if newCh.IsClosed() {
			t.Fatal("old-epoch frame closed the recycled subscription")
		}
		assertSubscriptionMatrixSizes(t, proc, 1, 1, 0)
	})

	// case11 in-flight-after-teardown dropped: frames arriving after cleanup
	// are consumed instead of being retained in the message queue.
	t.Run("case11 in-flight-after-teardown dropped", func(t *testing.T) {
		proc := newSubscriptionMatrixProcess(t)
		defer proc.Close()

		ch, sub := addMatrixSubscription(t, proc, "timer")
		epoch := proc.epoch.Load()
		proc.closeChannel(ch)

		delivered := proc.deliverMessage(proc.subs, subscriptionFrameMessage("timer", epoch, sub.id, sub.gen.Load(), payload.Payloads{
			payload.NewPayload(lua.LString("late"), payload.Lua),
		}))
		if !delivered {
			t.Fatal("post-teardown frame should be consumed")
		}
		assertSubscriptionMatrixSizes(t, proc, 0, 0, 0)
	})

	// case25 double-clean idempotent: closeChannel may be reached by fire,
	// stop, and drain, but the dispatcher cleanup closure runs once.
	t.Run("case25 double-clean idempotent", func(t *testing.T) {
		proc := newSubscriptionMatrixProcess(t)
		defer proc.Close()

		ch, _ := addMatrixSubscription(t, proc, "timer")
		var cleanupCount atomic.Int32
		if !proc.SetSubscriptionCleanup(ch, func() { cleanupCount.Add(1) }) {
			t.Fatal("SetSubscriptionCleanup failed")
		}

		if !proc.closeChannel(ch) {
			t.Fatal("first closeChannel should remove subscription")
		}
		if proc.closeChannel(ch) {
			t.Fatal("second closeChannel should be a no-op")
		}
		if got := cleanupCount.Load(); got != 1 {
			t.Fatalf("cleanup called %d times, want 1", got)
		}
		assertSubscriptionMatrixSizes(t, proc, 0, 0, 0)
	})

	// case27 one-shot 2nd frame dropped: after a terminal one-shot closes the
	// subscription, a duplicate fire frame is consumed and cannot enqueue.
	t.Run("case27 one-shot 2nd frame dropped", func(t *testing.T) {
		proc := newSubscriptionMatrixProcess(t)
		defer proc.Close()

		_, sub := addMatrixSubscription(t, proc, "timer")
		epoch := proc.epoch.Load()
		msg := subscriptionFrameMessage("timer", epoch, sub.id, sub.gen.Load(), payload.Payloads{
			payload.NewPayload(lua.LString("once"), payload.Lua),
			payload.NewTerminal(),
		})

		if !proc.deliverMessage(proc.subs, msg) {
			t.Fatal("first terminal frame should deliver")
		}
		if !proc.deliverMessage(proc.subs, msg) {
			t.Fatal("second terminal frame should be consumed")
		}
		assertSubscriptionMatrixSizes(t, proc, 0, 0, 0)
		if len(proc.messageQueue) != 0 {
			t.Fatalf("messageQueue len=%d, want 0", len(proc.messageQueue))
		}
	})

	// case28 id-recycle collision dropped: matching topic/subID/gen is still
	// rejected when the process epoch does not match.
	t.Run("case28 id-recycle collision dropped", func(t *testing.T) {
		proc := newSubscriptionMatrixProcess(t)
		defer proc.Close()

		oldCh, oldSub := addMatrixSubscription(t, proc, "timer")
		oldEpoch := proc.epoch.Load()
		oldGen := oldSub.gen.Load()
		proc.closeChannel(oldCh)
		proc.epoch.Add(1)
		proc.subs.nextID = 0
		_, newSub := addMatrixSubscription(t, proc, "timer")
		if newSub.id != oldSub.id || newSub.gen.Load() != oldGen {
			t.Fatalf("test requires id/gen collision, old=(%d,%d) new=(%d,%d)", oldSub.id, oldGen, newSub.id, newSub.gen.Load())
		}

		if !proc.deliverMessage(proc.subs, subscriptionFrameMessage("timer", oldEpoch, oldSub.id, oldGen, payload.Payloads{payload.NewTerminal()})) {
			t.Fatal("epoch-collision frame should be consumed")
		}
		assertSubscriptionMatrixSizes(t, proc, 1, 1, 0)
	})

	// case29 handler removed iff sub removed: closeChannel removes the handler
	// for the removed subscription and leaves unrelated handlers alone.
	t.Run("case29 handler removed iff sub removed", func(t *testing.T) {
		proc := newSubscriptionMatrixProcess(t)
		defer proc.Close()

		ch, _ := addMatrixSubscription(t, proc, "timer")
		proc.SetTopicHandler("timer", stringHandler)
		proc.SetTopicHandler("other", stringHandler)
		proc.closeChannel(ch)

		if _, ok := proc.GetTopicHandler("timer"); ok {
			t.Fatal("removed subscription handler still present")
		}
		if _, ok := proc.GetTopicHandler("other"); !ok {
			t.Fatal("unrelated handler was removed")
		}
		assertSubscriptionMatrixSizes(t, proc, 0, 0, 1)
	})

	// case3 discard-before-fire (bounded, ->0 after fire): a one-shot timer
	// fire delivered in the dispatcher's value-plus-terminal shape reclaims
	// the subscription even when Lua never reads the cap-1 channel.
	t.Run("case3 discard-before-fire", func(t *testing.T) {
		proc := newSubscriptionMatrixProcess(t)
		defer proc.Close()

		ch, sub := addMatrixSubscription(t, proc, "after@1")
		proc.SetTopicHandler("after@1", stringHandler)
		epoch := proc.epoch.Load()
		if !proc.deliverMessage(proc.subs, oneShotFireMessage("after@1", epoch, sub.id, sub.gen.Load(), lua.LString("fire"))) {
			t.Fatal("one-shot fire should be delivered")
		}
		if !ch.IsClosed() {
			t.Fatal("one-shot channel should close after an unread fire")
		}
		assertSubscriptionMatrixSizes(t, proc, 0, 0, 0)
	})

	// case13 ticker-never-stopped (->0 on process drain): a live ticker
	// subscription with a fire still buffered is released by the process
	// drain, running its cleanup closure exactly once.
	t.Run("case13 ticker-never-stopped", func(t *testing.T) {
		proc := newSubscriptionMatrixProcess(t)

		ch, sub := addMatrixSubscription(t, proc, "ticker@1")
		proc.SetTopicHandler("ticker@1", stringHandler)
		var stops atomic.Int32
		if !proc.SetSubscriptionCleanup(ch, func() { stops.Add(1) }) {
			t.Fatal("SetSubscriptionCleanup failed")
		}
		epoch := proc.epoch.Load()
		if !proc.deliverMessage(proc.subs, tickFireMessage("ticker@1", epoch, sub.id, sub.gen.Load(), lua.LString("tick"))) {
			t.Fatal("tick should be delivered")
		}
		assertSubscriptionMatrixSizes(t, proc, 1, 1, 1)

		proc.drainSubscriptionChannels()
		if got := stops.Load(); got != 1 {
			t.Fatalf("ticker cleanup called %d times, want 1", got)
		}
		assertSubscriptionMatrixSizes(t, proc, 0, 0, 0)
		proc.Close()
	})

	// case16 ticker-dropped-without-stop (bounded, ->0 on drain): ticks
	// that arrive on a full bounded buffer with no waiting receiver are
	// dropped, never retained in the message queue, and the drain still
	// reclaims the subscription.
	t.Run("case16 ticker-dropped-without-stop", func(t *testing.T) {
		proc := newSubscriptionMatrixProcess(t)

		ch := NewChannel(2)
		if err := proc.SubscribeExisting("ticker@2", ch); err != nil {
			t.Fatalf("SubscribeExisting: %v", err)
		}
		proc.subs.mu.RLock()
		sub := proc.subs.byTopic["ticker@2"]
		proc.subs.mu.RUnlock()
		proc.SetTopicHandler("ticker@2", stringHandler)
		epoch := proc.epoch.Load()

		// Fire more ticks than the buffer holds; overflow is dropped.
		for i := 0; i < 10; i++ {
			if !proc.deliverMessage(proc.subs, tickFireMessage("ticker@2", epoch, sub.id, sub.gen.Load(), lua.LString("tick"))) {
				t.Fatalf("tick %d should be consumed", i)
			}
		}
		if len(proc.messageQueue) != 0 {
			t.Fatalf("dropped ticks leaked into messageQueue, len=%d", len(proc.messageQueue))
		}
		if got := ch.Size(); got != 2 {
			t.Fatalf("bounded buffer = %d, want capacity 2", got)
		}
		assertSubscriptionMatrixSizes(t, proc, 1, 1, 1)

		proc.drainSubscriptionChannels()
		assertSubscriptionMatrixSizes(t, proc, 0, 0, 0)
		proc.Close()
	})

	// case30 one-shot loses a select (fires terminal, maps gone): a
	// time.after channel that loses a select is never read; its later
	// one-shot fire still reclaims the subscription through the terminal
	// frame.
	t.Run("case30 one-shot terminal reclaims unread", func(t *testing.T) {
		proc := newSubscriptionMatrixProcess(t)
		defer proc.Close()

		ready, _ := addMatrixSubscription(t, proc, "ready")
		loser, loserSub := addMatrixSubscription(t, proc, "after@30")
		proc.SetTopicHandler("after@30", stringHandler)
		assertSubscriptionMatrixSizes(t, proc, 2, 2, 1)

		// The select winner stays subscribed; the losing deadline fires
		// later and retires itself.
		epoch := proc.epoch.Load()
		if !proc.deliverMessage(proc.subs, oneShotFireMessage("after@30", epoch, loserSub.id, loserSub.gen.Load(), lua.LString("late"))) {
			t.Fatal("losing one-shot fire should be delivered")
		}
		if !loser.IsClosed() {
			t.Fatal("losing one-shot channel should close on fire")
		}
		if ready.IsClosed() {
			t.Fatal("select winner must remain subscribed")
		}
		assertSubscriptionMatrixSizes(t, proc, 1, 1, 0)
	})
}

// TestSubscriptionCloseChannelWakesBlockedReceiver covers the step-thread
// wake/refcount behavior that requires real Lua tasks.
func TestSubscriptionCloseChannelWakesBlockedReceiver(t *testing.T) {
	// case26 refcount/blocked-receiver wake on close: closeChannel applies the
	// channel close result so parked receivers resume with ok=false.
	t.Run("case26 refcount/blocked-receiver wake on close", func(t *testing.T) {
		script := `
			local ch = channel.new(0)
			subscribe("timer", ch)
			local v, ok = ch:receive()
			if v ~= nil then
				return nil, "expected nil"
			end
			if ok ~= false then
				return nil, "expected ok=false"
			end
			return "ok"
		`
		proc := startCleanupProcess(t, script)
		defer proc.Close()
		cleanupRunUntilIdle(t, proc)

		proc.subs.mu.RLock()
		ch := proc.subs.byTopic["timer"].channel
		proc.subs.mu.RUnlock()
		if !proc.closeChannel(ch) {
			t.Fatal("closeChannel should remove the live subscription")
		}
		cleanupRunUntilDone(t, proc)
	})
}
