// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
)

// Keep one actor alive throughout the measurement. Creation and compilation
// are not part of its per-message allocation or delivery-work budget.
func newChurnReceiver(tb testing.TB, capacity int) *Process {
	tb.Helper()
	return newChurnProcess(tb, fmt.Sprintf(`
		local inbox = channel.new(%d)
		subscribe("work", inbox)
		processed = 0
		while true do
			local value = inbox:receive()
			assert(value == processed + 1, "message reordered or lost")
			processed = processed + 1
			value = nil
		end
	`, capacity))
}

func newChurnProcess(tb testing.TB, script string) *Process {
	tb.Helper()
	proto, err := lua.CompileString(script, "actor_churn.lua")
	require.NoError(tb, err)
	p := mustNewProcess(tb, WithProto(proto))
	ctx, fc := ctxapi.OpenFrameContext(ctxapi.NewRootContext())
	require.NoError(tb, p.Init(ctx, "", nil))
	LoadModuleDef(p.State(), ChannelModule)
	loadPubSubGlobals(p.State())
	tb.Cleanup(func() { p.Close(); ctxapi.ReleaseFrameContext(fc) })
	var out process.StepOutput
	require.NoError(tb, p.Step(nil, &out))
	require.Equal(tb, process.StepIdle, out.Status())
	return p
}

func churnMessage(topic string, pl payload.Payload) process.Event {
	return process.Event{Type: process.EventMessage, Data: relay.NewPackage(testPID(), testPID(), topic, pl)}
}

func TestDeliveredMailboxClearsBackingArray(t *testing.T) {
	for _, capacity := range []int{0, 64} {
		t.Run(fmt.Sprint(capacity), func(t *testing.T) {
			p := newChurnReceiver(t, capacity)
			events := make([]process.Event, 64)
			for i := range events {
				events[i] = churnMessage("work", payload.NewPayload(lua.LInteger(i+1), payload.Lua))
			}
			var out process.StepOutput
			require.NoError(t, p.Step(events, &out))
			require.Equal(t, "64", p.State().GetGlobal("processed").String())
			require.Empty(t, p.messageQueue)
			for _, qm := range p.messageQueue[:cap(p.messageQueue)] {
				require.Equal(t, queuedMessage{}, qm, "delivered payload remains in backing array")
			}
			for _, value := range p.mainTask.resumeBuf {
				require.Nil(t, value, "idle task retains a consumed resume argument")
			}
		})
	}
}

func TestMailboxCompactionKeepsUndeliveredMessages(t *testing.T) {
	p := newChurnReceiver(t, 1)
	kept := payload.NewPayload(lua.LString("not subscribed yet"), payload.Lua)
	var out process.StepOutput
	require.NoError(t, p.Step([]process.Event{
		churnMessage("work", payload.NewPayload(lua.LInteger(1), payload.Lua)),
		churnMessage("later", kept),
	}, &out))
	require.Len(t, p.messageQueue, 1)
	require.Equal(t, "later", p.messageQueue[0].Topic)
	require.Equal(t, kept, p.messageQueue[0].Payloads[0])
	for _, qm := range p.messageQueue[1:cap(p.messageQueue)] {
		require.Equal(t, queuedMessage{}, qm)
	}
}

type countedActorPayload struct {
	calls *int
	value lua.LValue
}

func (p countedActorPayload) Format() payload.Format {
	*p.calls++
	return payload.Lua
}

func (p countedActorPayload) Data() any { return p.value }

func TestActorBurstDeliveryWorkIsLinear(t *testing.T) {
	for _, size := range []int{64, 256, 1024} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			p := newChurnReceiver(t, 0)
			calls := 0
			events := make([]process.Event, size)
			for i := range events {
				events[i] = churnMessage("work", countedActorPayload{calls: &calls, value: lua.LInteger(i + 1)})
			}
			var out process.StepOutput
			require.NoError(t, p.Step(events, &out))
			require.Equal(t, fmt.Sprint(size), p.State().GetGlobal("processed").String())
			require.LessOrEqual(t, calls, size*16, "a burst must not rescan its whole tail for every receive")
			t.Logf("messages=%d payload format checks=%d", size, calls)
		})
	}
}

func TestBufferedChannelBindingsReuseResults(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	LoadModuleDef(l, ChannelModule)
	ch := NewChannel(1)
	PushChannel(l, ch)
	allocs := testing.AllocsPerRun(1000, func() {
		l.SetTop(0)
		PushChannel(l, ch)
		l.Push(lua.LTrue)
		channelSend(l)
		l.SetTop(0)
		PushChannel(l, ch)
		channelReceive(l)
	})
	// The list node still allocates. Race builds also randomly discard pooled
	// objects, so use a budget below the original 11 allocations, not an exact
	// identity/reuse promise that sync.Pool deliberately does not make.
	require.Less(t, allocs, float64(5), "immediate operations must return their pooled results")
	t.Logf("buffered send/receive: %.2f allocations", allocs)
	require.Equal(t, lua.LTrue, l.Get(-2))
	require.Equal(t, lua.LTrue, l.Get(-1))
}

func TestTaskBuffersReleaseConsumedValues(t *testing.T) {
	task := NewTask(nil, nil)
	defer task.Close()
	task.ResumeWith(lua.LString("first"), lua.LString("obsolete"))
	task.ResumeWith(task.Resumed[:1]...)
	require.Equal(t, lua.LString("first"), task.Resumed[0], "aliased input must survive clearing")
	require.Nil(t, task.resumeBuf[:cap(task.resumeBuf)][1])
	task.ResumeWith()
	for _, v := range task.resumeBuf[:cap(task.resumeBuf)] {
		require.Nil(t, v)
	}
}

func TestTaskCloseClearsPooledBuffers(t *testing.T) {
	task := NewTask(nil, nil)
	task.ResumeWith(lua.LString("previous actor"))
	task.retBuf = append(task.retBuf, lua.LString("previous result"))
	task.retBuf = task.retBuf[:0]
	task.Close()
	for _, buf := range [][]lua.LValue{task.resumeBuf, task.retBuf} {
		for _, v := range buf[:cap(buf)] {
			require.Nil(t, v)
		}
	}
}

func TestTaskShorterYieldClearsReturnBufferTail(t *testing.T) {
	p := mustNewProcess(t, WithScript(`
		local first, second = coroutine.yield("first", "obsolete")
		assert(first == "first" and second == "obsolete", "aliased arguments were cleared early")
		coroutine.yield("next")
		while true do coroutine.yield() end
	`, "yield_buffer.lua"))
	ctx, fc := ctxapi.OpenFrameContext(ctxapi.NewRootContext())
	defer ctxapi.ReleaseFrameContext(fc)
	defer p.Close()
	require.NoError(t, p.Init(ctx, "", nil))
	tasks, err := p.vmStep()
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	task := tasks[0]
	task.Resumed = task.Yielded // exercise resume arguments aliasing retBuf
	_, err = p.vmStep(task)
	require.NoError(t, err)
	require.Equal(t, lua.LString("next"), task.Yielded[0])
	for _, v := range task.retBuf[len(task.Yielded):cap(task.retBuf)] {
		require.Nil(t, v, "shorter yield retains obsolete return values")
	}
}

func TestActorBurstRechecksSubscriptionAndHandler(t *testing.T) {
	for _, change := range []string{"replace", "handler"} {
		t.Run(change, func(t *testing.T) {
			p := newChurnProcess(t, `
				local inbox = channel.new(0)
				subscribe("work", inbox)
				processed = 0
				while true do
					local value = inbox:receive()
					assert(value == processed + 1)
					processed = processed + 1
					if processed == 1 then inbox = change(inbox) end
				end
			`)
			calls := 0
			p.State().SetGlobal("change", p.State().NewFunction(func(l *lua.LState) int {
				if change == "replace" {
					old := checkChannel(l, 1)
					require.True(t, p.UnsubscribeChannel(old))
					replacement := NewChannel(0)
					require.NoError(t, p.SubscribeExisting("work", replacement))
					PushChannel(l, replacement)
				} else {
					p.SetTopicHandler("work", func(ctx context.Context, l *lua.LState, _ pid.PID, _ string, pls []payload.Payload) lua.LValue {
						calls++
						return PayloadsToLua(ctx, l, pls)
					})
					l.Push(l.Get(1))
				}
				return 1
			}))
			events := make([]process.Event, 32)
			for i := range events {
				events[i] = churnMessage("work", payload.NewPayload(lua.LInteger(i+1), payload.Lua))
			}
			var out process.StepOutput
			require.NoError(t, p.Step(events, &out))
			require.Equal(t, "32", p.State().GetGlobal("processed").String())
			require.Empty(t, p.messageQueue)
			if change == "handler" {
				require.GreaterOrEqual(t, calls, 31)
			}
			for _, qm := range p.messageQueue[:cap(p.messageQueue)] {
				require.Equal(t, queuedMessage{}, qm)
			}
		})
	}
}

func TestActorBurstTerminalFollowsData(t *testing.T) {
	p := newChurnProcess(t, `
		local inbox = channel.new(0)
		subscribe("work", inbox)
		for i = 1, 32 do
			local value, ok = inbox:receive()
			assert(ok and value == i, "terminal overtook data")
		end
		local value, ok = inbox:receive()
		assert(not ok, "terminal not delivered")
		return 32
	`)
	events := make([]process.Event, 33)
	for i := range events[:32] {
		events[i] = churnMessage("work", payload.NewPayload(lua.LInteger(i+1), payload.Lua))
	}
	events[32] = churnMessage("work", payload.NewTerminal())
	var out process.StepOutput
	require.NoError(t, p.Step(events, &out))
	require.Equal(t, process.StepDone, out.Status())
	require.Empty(t, p.messageQueue)
}

func TestActorBurstRetainsTailAcrossSteps(t *testing.T) {
	for _, unsubscribe := range []bool{false, true} {
		t.Run(fmt.Sprintf("unsubscribe=%t", unsubscribe), func(t *testing.T) {
			p := newChurnProcess(t, fmt.Sprintf(`
				local inbox, gate = channel.new(0), channel.new(0)
				subscribe("work", inbox)
				subscribe("gate", gate)
				processed = 0
				while true do
					local value = inbox:receive()
					assert(value == processed + 1)
					processed = processed + 1
					if processed == 1 then
						if %t then unsubscribe(inbox) end
						gate:receive()
						if %t then
							inbox = channel.new(0)
							subscribe("work", inbox)
						end
					end
				end
			`, unsubscribe, unsubscribe))
			events := make([]process.Event, 32)
			for i := range events {
				events[i] = churnMessage("work", payload.NewPayload(lua.LInteger(i+1), payload.Lua))
			}
			var out process.StepOutput
			require.NoError(t, p.Step(events, &out))
			require.Equal(t, "1", p.State().GetGlobal("processed").String())
			require.Len(t, p.messageQueue, 31)
			require.False(t, p.messageBatchActive)
			require.Empty(t, p.messageBatchTopic)
			for _, qm := range p.messageQueue[len(p.messageQueue):cap(p.messageQueue)] {
				require.Equal(t, queuedMessage{}, qm)
			}
			out.Reset()
			require.NoError(t, p.Step([]process.Event{churnMessage("gate", payload.NewPayload(lua.LTrue, payload.Lua))}, &out))
			require.Equal(t, "32", p.State().GetGlobal("processed").String())
			require.Empty(t, p.messageQueue)
		})
	}
}

func TestActorBurstRestoresMailboxOnEarlyExit(t *testing.T) {
	for _, exit := range []string{"error", "return", "upgrade"} {
		t.Run(exit, func(t *testing.T) {
			action := map[string]string{
				"error":   `error("consumer failed")`,
				"return":  `return 1`,
				"upgrade": `coroutine.yield(upgrade_request)`,
			}[exit]
			p := newChurnProcess(t, `
				local inbox = channel.new(0)
				subscribe("work", inbox)
				assert(inbox:receive() == 1)
				`+action)
			p.State().SetGlobal("upgrade_request", &UpgradeRequest{})
			p.messageQueue = make([]queuedMessage, 0, 64)
			base := p.messageQueue[:cap(p.messageQueue)]
			events := make([]process.Event, 32)
			for i := range events {
				events[i] = churnMessage("work", payload.NewPayload(lua.LInteger(i+1), payload.Lua))
			}
			var out process.StepOutput
			err := p.Step(events, &out)
			if exit == "error" {
				require.ErrorContains(t, err, "consumer failed")
			} else {
				require.NoError(t, err)
			}
			if exit == "upgrade" {
				require.Equal(t, process.StepUpgrade, out.Status())
			} else {
				require.Equal(t, process.StepDone, out.Status())
			}
			require.Len(t, p.messageQueue, 31)
			require.Equal(t, 64, cap(p.messageQueue), "early exit must restore the original capacity")
			require.Same(t, &base[0], &p.messageQueue[0])
			require.False(t, p.messageBatchActive)
			require.Empty(t, p.messageBatchTopic)
			for i, qm := range p.messageQueue {
				require.Equal(t, "work", qm.Topic)
				require.Equal(t, lua.LInteger(i+2), qm.Payloads[0].Data())
			}
			for _, qm := range base[len(p.messageQueue):] {
				require.Equal(t, queuedMessage{}, qm, "vacated slots must not retain delivered values")
			}
		})
	}
}

func TestActorBurstRechecksClosedChannel(t *testing.T) {
	p := newChurnProcess(t, `
		local inbox, gate = channel.new(0), channel.new(0)
		subscribe("work", inbox)
		subscribe("gate", gate)
		assert(inbox:receive() == 1)
		inbox:close()
		gate:receive()
		inbox = channel.new(0)
		subscribe("work", inbox)
		for i = 3, 32 do
			assert(inbox:receive() == i, "retained suffix reordered or lost")
		end
		return 32
	`)
	p.messageQueue = make([]queuedMessage, 0, 64)
	base := p.messageQueue[:cap(p.messageQueue)]
	events := make([]process.Event, 32)
	for i := range events {
		events[i] = churnMessage("work", payload.NewPayload(lua.LInteger(i+1), payload.Lua))
	}
	var out process.StepOutput
	require.NoError(t, p.Step(events, &out))
	require.Equal(t, process.StepIdle, out.Status())
	// General delivery consumes the first send to the closed channel and
	// removes its subscription. Later unmatched messages remain available for
	// a future subscriber; batching must preserve that existing behavior.
	require.Len(t, p.messageQueue, 30)
	require.Equal(t, 64, cap(p.messageQueue))
	require.Same(t, &base[0], &p.messageQueue[0])
	require.False(t, p.messageBatchActive)
	require.Empty(t, p.messageBatchTopic)
	_, subscribed := p.subs.match("work")
	require.False(t, subscribed)
	for i, qm := range p.messageQueue {
		require.Equal(t, lua.LInteger(i+3), qm.Payloads[0].Data())
	}
	for _, qm := range base[len(p.messageQueue):] {
		require.Equal(t, queuedMessage{}, qm)
	}
	out.Reset()
	require.NoError(t, p.Step([]process.Event{churnMessage("gate", payload.NewPayload(lua.LTrue, payload.Lua))}, &out))
	require.Equal(t, process.StepDone, out.Status())
	require.Empty(t, p.messageQueue)
	for _, qm := range base {
		require.Equal(t, queuedMessage{}, qm)
	}
}

func TestActorBurstKeepsComplexTrafficOnGeneralPath(t *testing.T) {
	for _, kind := range []string{"mixed", "system", "handler", "items", "bytes", "frame", "terminal", "fallback"} {
		t.Run(kind, func(t *testing.T) {
			p := newChurnReceiver(t, 0)
			p.messageQueue = []queuedMessage{
				{Topic: "work", Payloads: payload.Payloads{payload.New(1)}},
				{Topic: "work", Payloads: payload.Payloads{payload.New(2)}},
			}
			switch kind {
			case "mixed":
				p.messageQueue[1].Topic = "other"
			case "system":
				p.messageQueue[0].Topic = "@work"
				p.messageQueue[1].Topic = "@work"
				require.NoError(t, p.SubscribeExisting("@work", NewChannel(0)))
			case "handler":
				p.SetTopicHandler("work", func(context.Context, *lua.LState, pid.PID, string, []payload.Payload) lua.LValue {
					return lua.LNil
				})
			case "items":
				p.messageQueue[1].MaxItems = 32
			case "bytes":
				p.messageQueue[1].MaxBytes = 1024
			case "frame":
				p.messageQueue[1].Payloads = payload.Payloads{NewSubscriptionFramePayload(&SubscriptionFrame{})}
			case "terminal":
				p.messageQueue[1].Payloads = payload.Payloads{payload.NewTerminal()}
			case "fallback":
				p.messageQueue[0].Topic = "other"
				p.messageQueue[1].Topic = "other"
				require.NoError(t, p.SubscribeExisting(topology.TopicInbox, NewChannel(0)))
			}
			require.Nil(t, p.beginMessageBatch())
			require.False(t, p.messageBatchActive)
		})
	}
}

func TestChannelResultReleaseClearsReferences(t *testing.T) {
	r := acquireResult()
	ch := NewChannel(1)
	r.Block = append(r.Block, ch)
	r.Release = append(r.Release, ch)
	u := acquireTaskUpdate()
	u.setResult1(lua.LString("old result"))
	r.Updates = append(r.Updates, u)
	ReleaseResult(r)
	for _, channels := range [][]*Channel{r.Block, r.Release} {
		for _, channel := range channels[:cap(channels)] {
			require.Nil(t, channel)
		}
	}
	for _, update := range r.Updates[:cap(r.Updates)] {
		require.Nil(t, update)
	}
}
