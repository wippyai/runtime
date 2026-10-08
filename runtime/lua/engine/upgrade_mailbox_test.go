// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"container/list"
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
)

func TestUpgradeCapturesUnresumedDelivery(t *testing.T) {
	for _, selectReceive := range []bool{false, true} {
		t.Run(map[bool]string{false: "receive", true: "select"}[selectReceive], func(t *testing.T) {
			proc := newCDCRegressionProcess(t)
			defer proc.Close()
			sub, err := proc.subs.add("data", 0)
			require.NoError(t, err)
			sub.retainOnUpgrade = true
			task := proc.createTask(proc.state.NewFunction(func(*lua.LState) int { return 0 }))
			var selectOp *SelectOp
			if selectReceive {
				selectOp = &SelectOp{Task: task.Thread(), Cases: []*ChannelOp{{Channel: sub.channel, Kind: ReceiveOp}}}
			}
			result := sub.channel.Receive(task.Thread(), selectOp)
			proc.updateChannelRefs(proc.channels, result.Block, result.Release)
			ReleaseResult(result)
			proc.enqueueMessage(queuedMessage{Topic: "data", Payloads: payload.Payloads{payload.NewString("unread")}})
			proc.flushMessageQueue(proc.subs)
			require.NotEmpty(t, task.Resumed, "receiver has been woken but has not run")
			require.Empty(t, proc.messageQueue)
			proc.captureUpgradeMessages()
			require.Len(t, proc.upgradeMessages, 1)
			require.Equal(t, "data", proc.upgradeMessages[0].Topic)
		})
	}
}

func TestBufferedMailboxDeliveryMovesBeforeResultRelease(t *testing.T) {
	p := newCDCRegressionProcess(t)
	defer p.Close()
	sub, err := p.subs.add("data", 1)
	require.NoError(t, err)
	sub.retainOnUpgrade = true
	p.enqueueMessage(queuedMessage{Topic: "data", Payloads: payload.Payloads{payload.NewString("unread")}})
	p.flushMessageQueue(p.subs)
	wrapper := sub.channel.buffer.Front().Value.(*bufferedDelivery)
	task := p.createTask(p.state.NewFunction(func(*lua.LState) int { return 0 }))
	p.applyExternalChannelResult(sub.channel.Receive(task.Thread(), nil))
	require.Same(t, wrapper, task.delivery, "channel result must move envelope ownership to the task")
	p.captureUpgradeMessages()
	require.Len(t, p.upgradeMessages, 1, "releasing the channel result must not clear the task's envelope")
	require.Equal(t, "unread", p.upgradeMessages[0].Payloads[0].Data())
	task.releaseDelivery()
	require.Nil(t, wrapper.LValue, "pooled wrappers must not retain Lua values")
	require.Equal(t, mailboxEnvelope{}, wrapper.message, "pooled wrappers must not retain raw payloads")
}

func TestDrainedMailboxIsNotReplayed(t *testing.T) {
	p := newCDCRegressionProcess(t)
	defer p.Close()
	sub, err := p.subs.add("data", 1)
	require.NoError(t, err)
	sub.retainOnUpgrade = true
	p.enqueueMessage(queuedMessage{Topic: "data", Payloads: payload.Payloads{payload.NewString("discarded")}})
	p.flushMessageQueue(p.subs)
	require.Equal(t, 1, sub.channel.Drain())
	require.Zero(t, sub.channel.Drain())
	p.captureUpgradeMessages()
	require.Empty(t, p.upgradeMessages, "explicitly discarded messages must not survive an upgrade")
}

func TestConsumedUpgradeDeliveryDoesNotRetainEnvelope(t *testing.T) {
	p := newChurnReceiver(t, 0)
	sub, exists := p.subs.get("work")
	require.True(t, exists)
	sub.retainOnUpgrade = true
	var out process.StepOutput
	require.NoError(t, p.Step([]process.Event{churnMessage("work", payload.NewPayload(lua.LInteger(1), payload.Lua))}, &out))
	require.Equal(t, "1", p.State().GetGlobal("processed").String())
	for _, task := range p.threads {
		if task.delivery != nil {
			require.Equal(t, mailboxEnvelope{}, task.delivery.message, "idle tasks must not retain a consumed message")
			require.Nil(t, task.delivery.LValue)
		}
	}
	p.captureUpgradeMessages()
	require.Empty(t, p.upgradeMessages, "consumed messages must not be replayed on upgrade")
}

func TestUpgradeRequestClearedBeforeProcessPooling(t *testing.T) {
	p := newCDCRegressionProcess(t)
	p.upgradeRequest = &UpgradeRequest{}
	p.Close()
	require.Nil(t, p.upgradeRequest, "a recycled process must not inherit another execution's upgrade request")
}

func TestUpgradeCapturesSharedMailboxBufferOnce(t *testing.T) {
	for _, valueCopy := range []bool{false, true} {
		t.Run(map[bool]string{false: "shared channel", true: "native value-copy alias"}[valueCopy], func(t *testing.T) {
			p := newCDCRegressionProcess(t)
			defer p.Close()
			ch := NewChannel(1)
			alias := ch
			if valueCopy {
				copy := *ch
				alias = &copy
			}
			first, err := p.subs.addExisting("data", ch)
			require.NoError(t, err)
			second, err := p.subs.addExisting("other", alias)
			require.NoError(t, err)
			first.retainOnUpgrade, second.retainOnUpgrade = true, true
			p.enqueueMessage(queuedMessage{Topic: "data", Payloads: payload.Payloads{payload.NewString("once")}})
			p.flushMessageQueue(p.subs)
			p.captureUpgradeMessages()
			require.Len(t, p.upgradeMessages, 1, "two subscriptions to one buffer must not duplicate an accepted message")
		})
	}
}

func TestUpgradeCapturesAutomaticallyClosedMailbox(t *testing.T) {
	for _, combined := range []bool{false, true} {
		t.Run(map[bool]string{false: "separate terminal", true: "data and terminal"}[combined], func(t *testing.T) {
			p := newCDCRegressionProcess(t)
			defer p.Close()
			sub, err := p.subs.add("data", 1)
			require.NoError(t, err)
			sub.retainOnUpgrade = true
			pls := payload.Payloads{payload.NewPayload(lua.LString("unread"), payload.Lua)}
			if combined {
				pls = append(pls, payload.NewTerminal())
			}
			p.enqueueMessage(queuedMessage{Topic: "data", Payloads: pls})
			if !combined {
				p.enqueueMessage(queuedMessage{Topic: "data", Payloads: payload.Payloads{payload.NewTerminal()}})
			}
			p.flushMessageQueue(p.subs)
			require.True(t, sub.channel.IsClosed())
			require.Equal(t, 1, sub.channel.Size())
			require.Empty(t, p.messageQueue)
			p.captureUpgradeMessages()
			require.Len(t, p.upgradeMessages, 1, "automatic closure must not discard unread data")
			result := sub.channel.Receive(nil, nil)
			require.Equal(t, lua.LString("unread"), result.Updates[0].GetResult()[0])
			ReleaseResult(result)
			p.captureUpgradeMessages()
			require.Empty(t, p.upgradeMessages, "consumed closed-channel data must not be replayed")
		})
	}
}

func TestUpgradeDoesNotRestoreExplicitlyUnlistenedMailbox(t *testing.T) {
	p := newCDCRegressionProcess(t)
	defer p.Close()
	sub, err := p.subs.add("data", 1)
	require.NoError(t, err)
	sub.retainOnUpgrade = true
	p.enqueueMessage(queuedMessage{Topic: "data", Payloads: payload.Payloads{payload.NewString("retired")}})
	p.flushMessageQueue(p.subs)
	require.True(t, p.UnsubscribeChannel(sub.channel))
	p.captureUpgradeMessages()
	require.Empty(t, p.upgradeMessages, "explicit unlisten retires the mailbox")
}

func TestAutomaticMailboxClosurePrunesConsumedBuffers(t *testing.T) {
	p := newCDCRegressionProcess(t)
	defer p.Close()
	for range 100 {
		sub, err := p.subs.add("data", 1)
		require.NoError(t, err)
		sub.retainOnUpgrade = true
		p.enqueueMessage(queuedMessage{Topic: "data", Payloads: payload.Payloads{
			payload.NewPayload(lua.LString("value"), payload.Lua), payload.NewTerminal(),
		}})
		p.flushMessageQueue(p.subs)
		require.Len(t, p.closedMailboxes, 1, "repeated terminal/read cycles must not accumulate empty buffers")
		ReleaseResult(sub.channel.Receive(nil, nil))
	}
	p.captureUpgradeMessages()
	require.Empty(t, p.upgradeMessages)
}

func TestInitClearsCapturedUpgradePayloads(t *testing.T) {
	p := newCDCRegressionProcess(t)
	defer p.Close()
	p.upgradeMessages = []queuedMessage{{Topic: "old", Payloads: payload.Payloads{payload.NewString("previous execution")}}}
	backing := p.upgradeMessages
	p.clearExecution()
	p.closedMailboxes = []*list.List{list.New()}
	closedBacking := p.closedMailboxes
	ctx, frame := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(frame)
	require.NoError(t, p.Init(ctx, "", nil))
	require.Empty(t, p.upgradeMessages)
	require.Empty(t, p.closedMailboxes)
	require.Nil(t, closedBacking[0])
	require.Equal(t, queuedMessage{}, backing[0], "reset must release the payload references, not just truncate")
}

func TestUpgradeRetiresUnframedProducerBacklog(t *testing.T) {
	for _, bounded := range []bool{false, true} {
		t.Run(map[bool]string{false: "websocket", true: "leased producer"}[bounded], func(t *testing.T) {
			old, next := newCDCRegressionProcess(t), newCDCRegressionProcess(t)
			defer old.Close()
			defer next.Close()
			ch := NewChannel(1)
			require.NoError(t, old.SubscribeExisting("producer@1", ch))
			stopped := false
			require.True(t, old.SetSubscriptionCleanup(ch, func() { stopped = true }))
			lease := &cdcTestLease{}
			for i, value := range []string{"buffered", "queued"} {
				qm := queuedMessage{Topic: "producer@1", Payloads: payload.Payloads{payload.NewString(value)}}
				if bounded && i == 1 {
					qm.Lease = lease
					qm.MaxItems, qm.MaxBytes = 4, 64
				}
				old.enqueueMessage(qm)
			}
			old.enqueueMessage(queuedMessage{Topic: "ordinary", Payloads: payload.Payloads{payload.NewString("kept")}})
			old.flushMessageQueue(old.subs)
			require.Equal(t, 1, ch.Size())
			require.Len(t, old.messageQueue, 2)
			old.captureUpgradeMessages()
			old.clearExecution()
			require.True(t, stopped)
			require.NoError(t, old.TransferUpgradeState(next))
			require.Len(t, next.messageQueue, 1, "only ordinary mailbox data may survive")
			require.Equal(t, "ordinary", next.messageQueue[0].Topic)
			if bounded {
				require.EqualValues(t, 1, lease.calls.Load(), "retired producer reservation must release exactly once")
				require.NotContains(t, next.messageQueueItems, "producer@1")
				require.NotContains(t, next.messageQueueBytes, "producer@1")
				require.NotContains(t, next.messageQueueItemLimits, "producer@1")
				require.NotContains(t, next.messageQueueLimits, "producer@1")
				require.NotContains(t, next.messageQueueOverflowed, "producer@1")
				require.NotContains(t, next.messageQueueDiscarded, "producer@1")
			}
		})
	}
}

func TestUpgradeMailboxMovesOwnershipOnce(t *testing.T) {
	old, next := newCDCRegressionProcess(t), newCDCRegressionProcess(t)
	defer old.Close()
	defer next.Close()
	lease, staleLease := &cdcTestLease{}, &cdcTestLease{}
	old.enqueueMessage(queuedMessage{Topic: "data", Payloads: payload.Payloads{payload.NewString("kept")}, Lease: lease, MaxItems: 1})
	old.enqueueMessage(queuedMessage{Topic: "timer", Payloads: payload.Payloads{NewSubscriptionFramePayload(&SubscriptionFrame{})}, Lease: staleLease})
	require.NoError(t, old.TransferUpgradeState(next))
	require.Empty(t, old.messageQueue)
	require.Len(t, next.messageQueue, 1)
	require.Zero(t, lease.calls.Load(), "transferring must not release a live reservation")
	require.EqualValues(t, 1, staleLease.calls.Load(), "old producer frames are discarded once")
	require.NoError(t, old.TransferUpgradeState(next), "a second handoff must not duplicate messages")
	next.clearMessageQueue()
	require.EqualValues(t, 1, lease.calls.Load())
	require.EqualValues(t, 1, staleLease.calls.Load())
}

func TestUpgradeMailboxPreservesLocalBudgetAndTerminalOrder(t *testing.T) {
	old, next := newCDCRegressionProcess(t), newCDCRegressionProcess(t)
	defer old.Close()
	defer next.Close()
	for i := 0; i < 2; i++ {
		old.enqueueMessage(queuedMessage{Topic: "data", Payloads: payload.Payloads{payload.NewString("value")}, MaxItems: 1, MaxBytes: 8, PayloadBytes: 4})
	}
	require.Len(t, old.messageQueue, 2, "overflow is represented by a terminal behind the admitted message")
	require.NoError(t, old.TransferUpgradeState(next))
	require.Len(t, next.messageQueue, 2)
	require.False(t, isOverflowTerminal(next.messageQueue[0].Payloads), "terminal cannot overtake admitted data")
	require.True(t, isOverflowTerminal(next.messageQueue[1].Payloads))
	require.Equal(t, 1, next.messageQueueItems["data"])
	require.EqualValues(t, 4, next.messageQueueBytes["data"])
	_, overflowed := next.messageQueueOverflowed["data"]
	require.True(t, overflowed)
}

func TestUpgradeMailboxDetachesLuaPayloads(t *testing.T) {
	old, next := newCDCRegressionProcess(t), newCDCRegressionProcess(t)
	defer old.Close()
	defer next.Close()
	table := old.State().NewTable()
	table.RawSetString("index", lua.LInteger(7))
	pls := payload.Payloads{payload.NewString("prefix"), payload.NewPayload(table, payload.Lua)}
	old.enqueueMessage(queuedMessage{Topic: "data", Payloads: pls})
	require.NoError(t, old.TransferUpgradeState(next))
	copy := next.messageQueue[0].Payloads[1].Data().(*lua.LTable)
	require.NotSame(t, table, copy)
	require.Same(t, table, pls[1].Data(), "handoff must not mutate an exported payload slice")
	copy.RawSetString("index", lua.LInteger(8))
	require.Equal(t, lua.LInteger(7), table.RawGetString("index"))
}

type incompatibleUpgradeProcess struct{}

func (*incompatibleUpgradeProcess) Init(context.Context, string, payload.Payloads) error { return nil }
func (*incompatibleUpgradeProcess) Step([]process.Event, *process.StepOutput) error      { return nil }
func (*incompatibleUpgradeProcess) Close()                                               {}

func TestUpgradeMailboxRejectsIncompatibleReplacement(t *testing.T) {
	old := newCDCRegressionProcess(t)
	defer old.Close()
	lease := &cdcTestLease{}
	old.enqueueMessage(queuedMessage{Topic: "data", Payloads: payload.Payloads{payload.NewString("kept")}, Lease: lease})
	require.Error(t, old.TransferUpgradeState(&incompatibleUpgradeProcess{}))
	require.Len(t, old.messageQueue, 1, "failed transfer leaves ownership with the old incarnation")
	require.Zero(t, lease.calls.Load())
	old.clearMessageQueue()
	require.EqualValues(t, 1, lease.calls.Load())
}

func TestUpgradeRejectsLateProducerFramesWithoutMailbox(t *testing.T) {
	old, next := newCDCRegressionProcess(t), newCDCRegressionProcess(t)
	defer old.Close()
	defer next.Close()
	staleEpoch := next.Epoch()
	old.epoch.Store(staleEpoch + 1) // old clearExecution has retired this epoch
	require.NoError(t, old.TransferUpgradeState(next))
	ch, subID, gen, err := next.SubscribeRouted("timer", 1)
	require.NoError(t, err)
	frame := &SubscriptionFrame{Epoch: staleEpoch, SubID: subID, Gen: gen.Load(), Payloads: payload.Payloads{payload.NewPayload(lua.LString("stale"), payload.Lua)}}
	next.enqueueMessage(queuedMessage{Topic: "timer", Payloads: payload.Payloads{NewSubscriptionFramePayload(frame)}})
	next.flushMessageQueue(next.subs)
	require.Zero(t, ch.Size(), "a late old frame must not match the replacement's recreated subscription")
}

func TestUpgradeReplaysBufferedEnvelopeThroughReplacementHandler(t *testing.T) {
	old, next := newCDCRegressionProcess(t), newCDCRegressionProcess(t)
	defer old.Close()
	defer next.Close()
	sub, err := old.subs.add("data", 1)
	require.NoError(t, err)
	sub.retainOnUpgrade = true
	old.SetTopicHandler("data", func(context.Context, *lua.LState, pid.PID, string, []payload.Payload) lua.LValue {
		return lua.LString("old handler")
	})
	old.enqueueMessage(queuedMessage{Topic: "data", Payloads: payload.Payloads{payload.NewPayload(lua.LInteger(42), payload.Lua)}})
	old.flushMessageQueue(old.subs)
	require.Empty(t, old.messageQueue)
	old.captureUpgradeMessages()
	old.clearExecution()
	require.NoError(t, old.TransferUpgradeState(next))
	newSub, err := next.subs.add("data", 1)
	require.NoError(t, err)
	next.SetTopicHandler("data", func(_ context.Context, _ *lua.LState, _ pid.PID, _ string, pls []payload.Payload) lua.LValue {
		require.Equal(t, lua.LInteger(42), pls[0].Data(), "new handler receives the original envelope, not the old decoded value")
		return lua.LString("new handler")
	})
	next.flushMessageQueue(next.subs)
	result := newSub.channel.Receive(nil, nil)
	defer ReleaseResult(result)
	require.Equal(t, lua.LString("new handler"), result.Updates[0].GetResult()[0])
}

func TestUpgradeCapturesReceiveYieldBeforeChannelUpdates(t *testing.T) {
	proc := newCDCRegressionProcess(t)
	defer proc.Close()
	LoadModuleDef(proc.State(), ChannelModule)
	proc.queue.Drain()
	sub, err := proc.subs.add("data", 1)
	require.NoError(t, err)
	sub.retainOnUpgrade = true
	proc.enqueueMessage(queuedMessage{Topic: "data", Payloads: payload.Payloads{payload.NewPayload(lua.LString("unread"), payload.Lua)}})
	proc.flushMessageQueue(proc.subs)

	// A blocked local sender makes the buffered receive yield channel updates.
	// Another ready coroutine can upgrade before those updates are applied.
	sender := proc.createTask(proc.state.NewFunction(func(*lua.LState) int { return 0 }))
	result := sub.channel.Send(sender.Thread(), lua.LString("local"), nil)
	proc.updateChannelRefs(proc.channels, result.Block, result.Release)
	ReleaseResult(result)
	PushChannel(proc.State(), sub.channel)
	channel := proc.State().Get(-1)
	proc.State().Pop(1)
	readerFn, err := proc.State().LoadString(`local ch = ...; return ch:receive()`)
	require.NoError(t, err)
	reader := proc.createTask(readerFn)
	reader.ResumeWith(channel)
	proc.State().SetGlobal("upgrade_request", &UpgradeRequest{})
	upgradeFn, err := proc.State().LoadString(`coroutine.yield(upgrade_request)`)
	require.NoError(t, err)
	upgrader := proc.createTask(upgradeFn)
	_, err = proc.vmStep(reader, upgrader)
	require.NoError(t, err)
	require.NotNil(t, proc.upgradeRequest)
	proc.captureUpgradeMessages()
	require.Len(t, proc.upgradeMessages, 1)
	require.Equal(t, lua.LString("unread"), proc.upgradeMessages[0].Payloads[0].Data())
}

func TestUpgradeMailboxSurvivesRepeatedTransfer(t *testing.T) {
	old, middle, final := newCDCRegressionProcess(t), newCDCRegressionProcess(t), newCDCRegressionProcess(t)
	defer old.Close()
	defer middle.Close()
	defer final.Close()
	for i := 1; i <= 5; i++ {
		old.enqueueMessage(queuedMessage{Topic: "data", Payloads: payload.Payloads{payload.NewPayload(lua.LInteger(i), payload.Lua)}})
	}
	for _, pair := range [][2]*Process{{old, middle}, {middle, final}} {
		from, to := pair[0], pair[1]
		sub, err := from.subs.add("data", 1)
		require.NoError(t, err)
		sub.retainOnUpgrade = true
		from.flushMessageQueue(from.subs)
		from.captureUpgradeMessages()
		from.clearExecution()
		require.NoError(t, from.TransferUpgradeState(to))
		if to == middle {
			middle.enqueueMessage(queuedMessage{Topic: "data", Payloads: payload.Payloads{payload.NewPayload(lua.LInteger(6), payload.Lua)}})
		}
	}
	require.Len(t, final.messageQueue, 6)
	for i, qm := range final.messageQueue {
		require.Equal(t, lua.LInteger(i+1), qm.Payloads[0].Data())
	}
}
