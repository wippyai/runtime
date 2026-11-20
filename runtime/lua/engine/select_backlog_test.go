// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/topology"
)

func BenchmarkSelectUnrelatedMailbox(b *testing.B) {
	p := &Process{subs: newTestSubscribeCtx()}
	sub, err := p.subs.add("control", 0)
	if err != nil {
		b.Fatal(err)
	}
	for i := range 4096 {
		p.enqueueMessage(queuedMessage{Topic: []string{"a", "b"}[i%2]})
	}
	p.flushMessageQueue(p.subs, nil) // scheduler delivery before Lua runs
	b.ReportAllocs()
	for b.Loop() {
		p.prepareChannelReceive(sub.channel)
	}
}

func TestSelectFindsAdmittedEventBehindMixedBacklog(t *testing.T) {
	p := newSubscriptionMatrixProcess(t)
	p.ctx = payload.WithTranscoder(ctxapi.NewRootContext(), createInboxTestTranscoder())
	defer p.Close()
	ch := NewChannel(0)
	require.NoError(t, p.SubscribeExisting(topology.TopicEvents, ch))
	for i := range 4096 {
		p.enqueueMessage(queuedMessage{Topic: []string{"a", "b"}[i%2]})
	}
	for _, value := range []string{"first", "second"} {
		p.enqueueMessage(queuedMessage{Topic: topology.TopicEvents, Payloads: payload.Payloads{payload.NewPayload(value, payload.Golang)}})
	}
	for _, want := range []string{"first", "second"} {
		p.prepareChannelReceive(ch)
		require.True(t, ch.CanReceive())
		require.Equal(t, lua.LString(want), ch.buffer.Remove(ch.buffer.Front()))
	}
	p.prepareChannelReceive(ch)
	require.False(t, ch.CanReceive())
	require.Len(t, p.messageQueue, 4096)
	for i, qm := range p.messageQueue {
		require.Equal(t, []string{"a", "b"}[i%2], qm.Topic)
	}
}

func TestSelectRechecksRoutesAfterSubscriptionChanges(t *testing.T) {
	p := newSubscriptionMatrixProcess(t)
	defer p.Close()
	p.ctx = payload.WithTranscoder(ctxapi.NewRootContext(), createInboxTestTranscoder())
	inbox, err := p.Subscribe(topology.TopicInbox, 0)
	require.NoError(t, err)
	enqueue := func(value string) {
		p.enqueueMessage(queuedMessage{Topic: "work", Payloads: payload.Payloads{payload.NewPayload(value, payload.Golang)}})
	}
	receive := func(ch *Channel, value string) {
		t.Helper()
		p.prepareChannelReceive(ch)
		require.True(t, ch.CanReceive())
		require.Equal(t, lua.LString(value), ch.buffer.Remove(ch.buffer.Front()))
	}
	enqueue("exact")
	p.flushMessageQueue(p.subs, nil)
	exact, err := p.Subscribe("work", 0)
	require.NoError(t, err)
	p.prepareChannelReceive(inbox)
	require.False(t, inbox.CanReceive())
	receive(exact, "exact")

	enqueue("fallback")
	p.flushMessageQueue(p.subs, nil)
	p.prepareChannelReceive(inbox)
	require.False(t, inbox.CanReceive())
	require.True(t, p.UnsubscribeChannel(exact))
	receive(inbox, "fallback")

	enqueue("admitted later")
	receive(inbox, "admitted later")
}

func TestSelectRechecksLinkDownAfterHandlerChangesTrapLinks(t *testing.T) {
	p := newSubscriptionMatrixProcess(t)
	defer p.Close()
	p.SetTrapLinks(true)
	_, err := p.Subscribe(topology.TopicEvents, 0)
	require.NoError(t, err)
	work, err := p.Subscribe("work", 0)
	require.NoError(t, err)
	p.SetTopicHandler("work", func(context.Context, *lua.LState, pid.PID, string, []payload.Payload) lua.LValue {
		p.SetTrapLinks(false)
		return nil
	})
	p.enqueueMessage(queuedMessage{Topic: topology.TopicEvents, Payloads: payload.Payloads{payload.New(&topology.ExitEvent{Kind: topology.LinkDown})}})
	p.enqueueMessage(queuedMessage{Topic: "work"})
	p.flushMessageQueue(p.subs, nil)
	require.NoError(t, p.linkDownError)
	p.prepareChannelReceive(work)
	require.Error(t, p.linkDownError)
}

func TestSelectInterleavedDataRetainsOrderAcrossRouteChange(t *testing.T) {
	p := newSubscriptionMatrixProcess(t)
	defer p.Close()
	a, err := p.Subscribe("a", 0)
	require.NoError(t, err)
	_, err = p.Subscribe("b", 0)
	require.NoError(t, err)
	for i := range 256 {
		p.enqueueMessage(queuedMessage{Topic: []string{"a", "b"}[i%2], Payloads: payload.Payloads{payload.NewPayload(lua.LInteger(i), payload.Lua)}})
	}
	p.canIndexSelect = true
	p.flushMessageQueue(p.subs, nil)
	for i := range 64 {
		p.prepareChannelReceive(a)
		require.Equal(t, lua.LInteger(i*2), a.buffer.Remove(a.buffer.Front()))
	}
	require.NotNil(t, p.selectData)
	inbox, err := p.Subscribe(topology.TopicInbox, 0)
	require.NoError(t, err)
	p.prepareChannelReceive(inbox)
	require.Nil(t, p.selectData)
	require.Len(t, p.messageQueue, 192)
	var previous uint64
	for _, qm := range p.messageQueue {
		require.Greater(t, qm.sequence, previous)
		previous = qm.sequence
	}
}

func BenchmarkSelectDrainInterleavedMailbox(b *testing.B) {
	for _, size := range []int{256, 1024, 4096} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			p := &Process{subs: newTestSubscribeCtx(), messageQueue: make([]queuedMessage, 0, size)}
			a, _ := p.subs.add("a", 0)
			c, _ := p.subs.add("b", 0)
			messages := payload.Payloads{payload.NewPayload(lua.LTrue, payload.Lua)}
			b.ReportAllocs()
			for b.Loop() {
				for i := range size {
					p.enqueueMessage(queuedMessage{Topic: []string{"a", "b"}[i%2], Payloads: messages})
				}
				p.canIndexSelect = true
				p.flushMessageQueue(p.subs, nil)
				for _, sub := range []*subscription{a, c} {
					for range size / 2 {
						p.prepareChannelReceive(sub.channel)
						sub.channel.buffer.Remove(sub.channel.buffer.Front())
					}
				}
				p.finishSelectDataBatch()
			}
		})
	}
}

type unsubscribingTranscoder struct {
	payload.Transcoder
	call func()
}

func (tc *unsubscribingTranscoder) Transcode(v payload.Payload, f payload.Format) (payload.Payload, error) {
	tc.call()
	return payload.NewPayload(lua.LInteger(1), payload.Lua), nil
}

func TestSelectDataConversionCanUnsubscribeProducer(t *testing.T) {
	p := newSubscriptionMatrixProcess(t)
	defer p.Close()
	defer p.finishSelectDataBatch()
	spare, e := p.Subscribe("bounded", 1)
	require.NoError(t, e)
	ch, e := p.Subscribe("a", 0)
	require.NoError(t, e)
	_, e = p.Subscribe("b", 0)
	require.NoError(t, e)
	p.enqueueMessage(queuedMessage{Topic: "bounded", MaxItems: 1, Payloads: payload.Payloads{payload.NewPayload(lua.LTrue, payload.Lua)}})
	p.flushMessageQueue(p.subs, nil)
	p.ctx = payload.WithTranscoder(ctxapi.NewRootContext(), &unsubscribingTranscoder{call: func() { p.UnsubscribeChannel(spare) }})
	for i := range 256 {
		p.enqueueMessage(queuedMessage{Topic: []string{"a", "b"}[i%2], Payloads: payload.Payloads{payload.NewString("convert")}})
	}
	p.canIndexSelect = true
	p.flushMessageQueue(p.subs, nil)
	p.prepareChannelReceive(ch)
	require.True(t, ch.CanReceive())
}

func TestSelectDataBatchPreservesUnreadMessagesOnUpgrade(t *testing.T) {
	p := newChurnProcess(t, `
    local a,b = channel.new(0),channel.new(1)
    subscribe("a",a)
    subscribe("b",b)
    local cases = {a:case_receive()}
    for i=1,5 do assert(channel.select(cases).value == (i-1)*2) end
    assert(batch_active())
    coroutine.yield(upgrade_request)
  `)
	for _, name := range []string{"a", "b"} {
		s, ok := p.subs.get(name)
		require.True(t, ok)
		s.retainOnUpgrade = true
	}
	p.state.SetGlobal("batch_active", p.state.NewFunction(func(l *lua.LState) int { l.Push(lua.LBool(p.selectData != nil)); return 1 }))
	p.state.SetGlobal("upgrade_request", &UpgradeRequest{})
	var events []process.Event
	for i := range 256 {
		events = append(events, churnMessage([]string{"a", "b"}[i%2], payload.NewPayload(lua.LInteger(i), payload.Lua)))
	}
	var out process.StepOutput
	require.NoError(t, p.Step(events, &out))
	require.Equal(t, process.StepUpgrade, out.Status())
	require.Nil(t, p.selectData)
	next := newCDCRegressionProcess(t)
	defer next.Close()
	require.NoError(t, p.TransferUpgradeState(next))
	require.Len(t, next.messageQueue, 251)
	idx := 0
	for i := range 256 {
		if i%2 == 0 && i < 10 {
			continue
		}
		require.Equal(t, lua.LInteger(i), next.messageQueue[idx].Payloads[0].Data())
		idx++
	}
}
