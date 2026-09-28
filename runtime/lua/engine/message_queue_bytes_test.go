// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"bytes"
	"context"
	"sync/atomic"
	"testing"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
)

func TestMessageQueueByteLimitRejectsLargePayload(t *testing.T) {
	proc := mustNewProcess(t, WithScript("return 1", "test.lua"))
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	const topic = "cdc.large"
	const limit = int64(1 << 20)
	ch := NewChannel(1)
	if err := proc.SubscribeExisting(topic, ch); err != nil {
		t.Fatal(err)
	}
	proc.SetTopicHandler(topic, func(context.Context, *lua.LState, pid.PID, string, []payload.Payload) lua.LValue {
		return lua.LTrue
	})
	var cleanup atomic.Bool
	if !proc.SetSubscriptionCleanup(ch, func() { cleanup.Store(true) }) {
		t.Fatal("subscription cleanup was not installed")
	}

	blob := bytes.Repeat([]byte{'x'}, int(limit)+1)
	proc.enqueueMessage(queuedMessage{
		Topic:        topic,
		Payloads:     payload.Payloads{payload.New(blob)},
		PayloadBytes: int64(len(blob)),
		MaxBytes:     limit,
	})

	if got := len(proc.messageQueue); got != 1 {
		t.Fatalf("expected one synthetic terminal, got %d queued messages", got)
	}
	if got := proc.messageQueue[0].Payloads[0].Format(); got != payload.GoError {
		t.Fatalf("expected overflow error payload, got %q", got)
	}
	if got := proc.messageQueueBytes[topic]; got != 0 {
		t.Fatalf("overflow retained %d bytes", got)
	}
	if !cleanup.Load() {
		t.Fatal("overflow did not stop the existing producer")
	}

	proc.flushMessageQueue(proc.subs)
	if !ch.IsClosed() {
		t.Fatal("overflow terminal did not close the subscription")
	}
}

func TestMessageQueueByteLimitBoundsSlowConsumer(t *testing.T) {
	proc := mustNewProcess(t, WithScript("return 1", "test.lua"))
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	const topic = "cdc.slow"
	const messageBytes = int64(100)
	const limit = int64(128)
	ch := NewChannel(0)
	if err := proc.SubscribeExisting(topic, ch); err != nil {
		t.Fatal(err)
	}
	proc.SetTopicHandler(topic, func(context.Context, *lua.LState, pid.PID, string, []payload.Payload) lua.LValue {
		return lua.LTrue
	})
	var cleanup atomic.Bool
	if !proc.SetSubscriptionCleanup(ch, func() { cleanup.Store(true) }) {
		t.Fatal("subscription cleanup was not installed")
	}

	message := func() queuedMessage {
		return queuedMessage{
			Topic:        topic,
			Payloads:     payload.Payloads{payload.NewString("change")},
			PayloadBytes: messageBytes,
			MaxBytes:     limit,
		}
	}

	// With a rendezvous channel and no waiting consumer, the first value stays
	// in Process.messageQueue; the next value exceeds the byte budget.
	proc.enqueueMessage(message())
	proc.flushMessageQueue(proc.subs)
	proc.enqueueMessage(message())
	if got := proc.messageQueueBytes[topic]; got != messageBytes {
		t.Fatalf("expected %d retained bytes, got %d", messageBytes, got)
	}

	// Further values are dropped after exactly one terminal is queued.
	for i := 0; i < 100; i++ {
		proc.enqueueMessage(message())
	}
	if got := proc.messageQueueBytes[topic]; got > limit {
		t.Fatalf("retained %d bytes above limit %d", got, limit)
	}
	if got := len(proc.messageQueue); got != 2 {
		t.Fatalf("expected retained value plus terminal, got %d messages", got)
	}
	if !cleanup.Load() {
		t.Fatal("overflow did not stop the existing producer")
	}
}

func TestMessageQueueItemLimitIsPerTopicAndTerminalOrdered(t *testing.T) {
	proc := mustNewProcess(t, WithScript("return 1", "test.lua"))
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	const limit = 2
	for _, topic := range []string{"cdc.one", "cdc.two"} {
		ch := NewChannel(0)
		if err := proc.SubscribeExisting(topic, ch); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < limit+4; i++ {
			proc.enqueueMessage(queuedMessage{
				Topic:    topic,
				Payloads: payload.Payloads{payload.NewString("change")},
				MaxItems: limit,
			})
		}
	}

	if got := len(proc.messageQueue); got != 2*(limit+1) {
		t.Fatalf("expected two bounded queues with data plus terminal, got %d", got)
	}
	if got := len(proc.messageQueueOverflowed); got != 2 {
		t.Fatalf("expected independent overflow tombstones, got %d", got)
	}
}
