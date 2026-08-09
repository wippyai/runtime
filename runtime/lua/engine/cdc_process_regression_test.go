// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/topology"
)

func newCDCRegressionProcess(t *testing.T) *Process {
	t.Helper()
	proc := mustNewProcess(t, WithScript(`return 1`, "cdc_process_regression.lua"))
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		proc.Close()
		t.Fatalf("process init failed: %v", err)
	}
	return proc
}

func cdcRegressionHandler(_ context.Context, _ *lua.LState, _ pid.PID, _ string, _ []payload.Payload) lua.LValue {
	return lua.LTrue
}

// A bounded relay message must wait for its exact subscription. In particular,
// a startup message cannot be consumed by the process inbox before the CDC
// subscription has finished registering.
func TestBoundedMessageDoesNotFallbackToInbox(t *testing.T) {
	proc := newCDCRegressionProcess(t)
	defer proc.Close()

	inbox := NewChannel(1)
	if err := proc.SubscribeExisting(topology.TopicInbox, inbox); err != nil {
		t.Fatal(err)
	}
	const topic = "cdc.startup"
	proc.enqueueMessage(queuedMessage{
		Topic:        topic,
		Payloads:     payload.Payloads{payload.NewString("snapshot")},
		PayloadBytes: 8,
		MaxItems:     4,
		MaxBytes:     64,
	})
	proc.flushMessageQueue(proc.subs)

	if got := inbox.Size(); got != 0 {
		t.Fatalf("bounded startup message fell back to inbox, size=%d", got)
	}
	if got := len(proc.messageQueue); got != 1 {
		t.Fatalf("bounded startup message was not retained, queue=%d", got)
	}

	cdc := NewChannel(1)
	if err := proc.SubscribeExisting(topic, cdc); err != nil {
		t.Fatal(err)
	}
	proc.SetTopicHandler(topic, cdcRegressionHandler)
	proc.flushMessageQueue(proc.subs)
	if got := cdc.Size(); got != 1 {
		t.Fatalf("exact subscription did not receive startup message, size=%d", got)
	}
	if got := len(proc.messageQueue); got != 0 {
		t.Fatalf("startup message remained after exact subscription, queue=%d", got)
	}
}

func TestBoundedTerminalDoesNotCloseInbox(t *testing.T) {
	proc := newCDCRegressionProcess(t)
	defer proc.Close()

	inbox := NewChannel(1)
	if err := proc.SubscribeExisting(topology.TopicInbox, inbox); err != nil {
		t.Fatal(err)
	}
	const topic = "cdc.startup-terminal"
	proc.enqueueMessage(queuedMessage{
		Topic:    topic,
		Payloads: payload.Payloads{payload.NewError(errors.New("snapshot failed")), payload.NewTerminal()},
		MaxItems: 1,
		MaxBytes: 64,
	})
	proc.flushMessageQueue(proc.subs)

	if inbox.IsClosed() {
		t.Fatal("bounded terminal closed the inbox fallback channel")
	}
	if got := len(proc.messageQueue); got != 1 {
		t.Fatalf("bounded terminal was not retained for exact subscription, queue=%d", got)
	}

	cdc := NewChannel(1)
	if err := proc.SubscribeExisting(topic, cdc); err != nil {
		t.Fatal(err)
	}
	proc.flushMessageQueue(proc.subs)
	if !cdc.IsClosed() {
		t.Fatal("exact subscription did not receive terminal close")
	}
	if inbox.IsClosed() {
		t.Fatal("inbox was closed while delivering exact terminal")
	}
}

func TestOrdinaryClosePreservesQueuedMessage(t *testing.T) {
	proc := newCDCRegressionProcess(t)
	defer proc.Close()

	const topic = "ordinary.close"
	old := NewChannel(1)
	if err := proc.SubscribeExisting(topic, old); err != nil {
		t.Fatal(err)
	}
	proc.SetTopicHandler(topic, cdcRegressionHandler)
	proc.enqueueMessage(queuedMessage{
		Topic:    topic,
		Payloads: payload.Payloads{payload.NewString("ordinary")},
	})
	if !proc.closeChannel(old) {
		t.Fatal("closeChannel did not remove ordinary subscription")
	}
	if got := len(proc.messageQueue); got != 1 {
		t.Fatalf("ordinary queued message was discarded on close, queue=%d", got)
	}
	if old.IsClosed() == false {
		t.Fatal("closed ordinary channel remains open")
	}

	current := NewChannel(1)
	if err := proc.SubscribeExisting(topic, current); err != nil {
		t.Fatal(err)
	}
	proc.SetTopicHandler(topic, cdcRegressionHandler)
	proc.flushMessageQueue(proc.subs)
	if got := current.Size(); got != 1 {
		t.Fatalf("preserved ordinary message was not delivered, size=%d", got)
	}
}

func TestGoErrorWithoutTerminalConsumesBoundedCapacity(t *testing.T) {
	proc := newCDCRegressionProcess(t)
	defer proc.Close()

	const topic = "cdc.error-data"
	message := func() queuedMessage {
		return queuedMessage{
			Topic:    topic,
			Payloads: payload.Payloads{payload.NewError(errors.New("row failed"))},
			MaxItems: 1,
		}
	}
	proc.enqueueMessage(message())
	proc.enqueueMessage(message())

	if got := len(proc.messageQueue); got != 2 {
		t.Fatalf("GoError-only data bypassed bounded admission, queue=%d", got)
	}
	if got := proc.messageQueueItems[topic]; got != 1 {
		t.Fatalf("GoError-only data was not charged as an item, items=%d", got)
	}
	if !isOverflowTerminal(proc.messageQueue[1].Payloads) {
		t.Fatalf("second GoError-only message did not produce overflow terminal")
	}
}

func TestCleanupRegisteredAfterOverflowStillRuns(t *testing.T) {
	proc := newCDCRegressionProcess(t)
	defer proc.Close()

	const topic = "cdc.late-cleanup"
	ch := NewChannel(1)
	if err := proc.SubscribeExisting(topic, ch); err != nil {
		t.Fatal(err)
	}
	proc.enqueueMessage(queuedMessage{
		Topic:        topic,
		Payloads:     payload.Payloads{payload.NewString("too large")},
		PayloadBytes: 2,
		MaxBytes:     1,
	})
	var calls atomic.Int32
	if !proc.SetSubscriptionCleanup(ch, func() { calls.Add(1) }) {
		t.Fatal("SetSubscriptionCleanup failed")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("late cleanup callback count=%d, want 1", got)
	}
	proc.closeChannel(ch)
	proc.drainSubscriptionChannels()
	if got := calls.Load(); got != 1 {
		t.Fatalf("cleanup callback ran more than once: %d", got)
	}
}

func TestProcessQueueBackingReferencesClearBeforeProcessReuse(t *testing.T) {
	proc := newCDCRegressionProcess(t)
	defer proc.Close()

	oldPayloads := payload.Payloads{payload.NewString("large retained value")}
	proc.enqueueMessage(queuedMessage{Topic: "unsubscribed", Payloads: oldPayloads})
	backing := proc.messageQueue
	if len(backing) != 1 || backing[0].Payloads == nil {
		t.Fatal("test message was not queued")
	}

	proc.clearExecution()
	if len(proc.messageQueue) != 1 {
		t.Fatalf("completed execution lost observable queued messages: %d", len(proc.messageQueue))
	}

	nextCtx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(nextCtx, "", nil); err != nil {
		t.Fatalf("process reinit failed: %v", err)
	}
	if len(proc.messageQueue) != 0 {
		t.Fatalf("process reuse left queued messages: %d", len(proc.messageQueue))
	}
	if backing[0].Payloads != nil {
		t.Fatal("process reuse left payloads reachable through queue backing array")
	}
}

type cdcTestLease struct {
	calls atomic.Int32
}

func (l *cdcTestLease) Release() {
	l.calls.Add(1)
}

func TestLeasedMessageUsesUpstreamBudgetAndReleasesOnClear(t *testing.T) {
	proc := newCDCRegressionProcess(t)
	defer proc.Close()

	lease := &cdcTestLease{}
	proc.enqueueMessage(queuedMessage{
		Topic:    "cdc.leased",
		Payloads: payload.Payloads{payload.NewString("leased")},
		MaxItems: 1,
		MaxBytes: 32,
		Lease:    lease,
	})
	if got := proc.messageQueueItems["cdc.leased"]; got != 0 {
		t.Fatalf("leased message consumed a second local item budget: %d", got)
	}
	if got := proc.messageQueueBytes["cdc.leased"]; got != 0 {
		t.Fatalf("leased message consumed a second local byte budget: %d", got)
	}
	proc.clearMessageQueue()
	if got := lease.calls.Load(); got != 1 {
		t.Fatalf("leased message release count=%d, want 1", got)
	}
}
