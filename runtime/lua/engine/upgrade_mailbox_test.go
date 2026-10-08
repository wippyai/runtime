// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
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
