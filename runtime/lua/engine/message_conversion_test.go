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
	"github.com/wippyai/runtime/api/topology"
)

type deliveryCountingTranscoder struct {
	payload.Transcoder
	calls int
}

func (tc *deliveryCountingTranscoder) Transcode(pl payload.Payload, format payload.Format) (payload.Payload, error) {
	tc.calls++
	return tc.Transcoder.Transcode(pl, format)
}

func TestDeliverMessage_DoesNotConvertBlockedPayloads(t *testing.T) {
	for _, capacity := range []int{0, 1} {
		t.Run(strconv.Itoa(capacity), func(t *testing.T) {
			proc := newSubscriptionMatrixProcess(t)
			defer proc.Close()
			tc := &deliveryCountingTranscoder{Transcoder: createInboxTestTranscoder()}
			proc.ctx = payload.WithTranscoder(ctxapi.NewRootContext(), tc)
			ch := NewChannel(capacity)
			require.NoError(t, proc.SubscribeExisting("data", ch))
			if capacity > 0 {
				ch.buffer.PushBack(lua.LString("seed"))
			}
			for i := range 2 {
				proc.enqueueMessage(queuedMessage{Topic: "data", Payloads: payload.Payloads{payload.NewPayload(map[string]any{"index": i}, payload.Golang)}})
			}
			for range 10 {
				proc.flushMessageQueue(proc.subs)
			}
			require.Zero(t, tc.calls, "blocked retries must not construct Lua values")
			require.Len(t, proc.messageQueue, 2)
			if capacity > 0 {
				ch.buffer.Remove(ch.buffer.Front())
			}
			for i := range 2 {
				if capacity == 0 {
					result := ch.Receive(proc.state, nil)
					ReleaseResult(result)
				}
				proc.flushMessageQueue(proc.subs)
				require.Equal(t, i+1, tc.calls)
				if capacity > 0 {
					got := ch.buffer.Remove(ch.buffer.Front()).(*lua.LTable)
					require.Equal(t, lua.LInteger(i), got.RawGetString("index"))
				}
			}
			require.Empty(t, proc.messageQueue)
		})
	}
}

func TestDeliverMessage_BlockedTopicHandlerStillRuns(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		proc := newSubscriptionMatrixProcess(t)
		ch, _ := addMatrixSubscription(t, proc, "handled")
		ch.buffer.PushBack(lua.LString("seed"))
		calls := 0
		proc.SetTopicHandler("handled", func(context.Context, *lua.LState, pid.PID, string, []payload.Payload) lua.LValue {
			calls++
			return nil
		})
		pls := payload.Payloads{payload.NewPayload("value", payload.Golang)}
		if terminal {
			pls = append(pls, payload.NewTerminal())
		}
		require.False(t, proc.deliverMessage(proc.subs, queuedMessage{Topic: "handled", Payloads: pls}))
		require.Equal(t, 1, calls)
		require.Equal(t, terminal, ch.IsClosed())
		proc.Close()
	}
}

func TestDeliverMessage_BlockedHandlerValueStillRetries(t *testing.T) {
	proc := newSubscriptionMatrixProcess(t)
	defer proc.Close()
	ch, _ := addMatrixSubscription(t, proc, "handled")
	ch.buffer.PushBack(lua.LString("seed"))
	calls := 0
	proc.SetTopicHandler("handled", func(context.Context, *lua.LState, pid.PID, string, []payload.Payload) lua.LValue {
		calls++
		return lua.LString("handled")
	})
	qm := queuedMessage{Topic: "handled", Payloads: payload.Payloads{payload.NewPayload("value", payload.Golang)}}
	for range 3 {
		require.True(t, proc.deliverMessage(proc.subs, qm))
	}
	require.Equal(t, 3, calls)
	ch.buffer.Remove(ch.buffer.Front())
	require.False(t, proc.deliverMessage(proc.subs, qm))
	require.Equal(t, 4, calls)
	require.Equal(t, lua.LString("handled"), ch.buffer.Remove(ch.buffer.Front()))
}

func TestDeliverMessage_FormatterWaitsForChannel(t *testing.T) {
	for _, capacity := range []int{0, 1} {
		for _, fallback := range []bool{false, true} {
			t.Run(strconv.Itoa(capacity)+"/fallback="+strconv.FormatBool(fallback), func(t *testing.T) {
				proc := newSubscriptionMatrixProcess(t)
				defer proc.Close()
				topic := "request"
				if fallback {
					topic = topology.TopicInbox
				}
				ch := NewChannel(capacity)
				require.NoError(t, proc.SubscribeExisting(topic, ch))
				if capacity > 0 {
					ch.buffer.PushBack(lua.LString("seed"))
				}
				calls := 0
				source := pid.PID{Host: "sender", UniqID: "1"}
				proc.setTopicHandler(topic, func(_ context.Context, _ *lua.LState, from pid.PID, deliveredTopic string, _ []payload.Payload) lua.LValue {
					calls++
					require.Equal(t, source, from)
					require.Equal(t, "request", deliveredTopic)
					return lua.LString("formatted")
				}, true)
				qm := queuedMessage{Source: source, Topic: "request", Payloads: payload.Payloads{payload.NewString("value")}}
				for range 10 {
					require.True(t, proc.deliverMessage(proc.subs, qm))
				}
				require.Zero(t, calls, "a blocked formatter must not construct discarded values")
				if capacity == 0 {
					ReleaseResult(ch.Receive(proc.mainTask.Thread(), nil))
				} else {
					ch.buffer.Remove(ch.buffer.Front())
				}
				require.False(t, proc.deliverMessage(proc.subs, qm))
				require.Equal(t, 1, calls)
				if capacity == 0 {
					require.Equal(t, []lua.LValue{lua.LString("formatted"), lua.LTrue}, proc.mainTask.Resumed)
				} else {
					require.Equal(t, lua.LString("formatted"), ch.buffer.Front().Value)
				}
			})
		}
	}
}

func TestSetTopicHandlerResetsDeliveryPolicy(t *testing.T) {
	proc := newSubscriptionMatrixProcess(t)
	defer proc.Close()
	ch, _ := addMatrixSubscription(t, proc, "handled")
	ch.buffer.PushBack(lua.LString("seed"))
	proc.setTopicHandler("handled", func(context.Context, *lua.LState, pid.PID, string, []payload.Payload) lua.LValue {
		t.Fatal("replaced formatter must not run")
		return lua.LNil
	}, true)
	calls := 0
	proc.SetTopicHandler("handled", func(context.Context, *lua.LState, pid.PID, string, []payload.Payload) lua.LValue {
		calls++
		return nil
	})
	handler, exists := proc.GetTopicHandler("handled")
	require.True(t, exists)
	require.NotNil(t, handler)
	require.False(t, proc.deliverMessage(proc.subs, queuedMessage{Topic: "handled", Payloads: payload.Payloads{payload.NewString("value")}}))
	require.Equal(t, 1, calls, "a replacement filter must keep eager consumption semantics")
}

func TestDeferredFormatterPreservesTerminalOrdering(t *testing.T) {
	for _, combined := range []bool{false, true} {
		t.Run(strconv.FormatBool(combined), func(t *testing.T) {
			proc := newSubscriptionMatrixProcess(t)
			defer proc.Close()
			ch, _ := addMatrixSubscription(t, proc, "handled")
			ch.buffer.PushBack(lua.LString("seed"))
			calls := 0
			proc.setTopicHandler("handled", func(context.Context, *lua.LState, pid.PID, string, []payload.Payload) lua.LValue {
				calls++
				return lua.LString("formatted")
			}, true)
			pls := payload.Payloads{payload.NewTerminal()}
			if combined {
				pls = append(payload.Payloads{payload.NewString("value")}, pls...)
			}
			proc.enqueueMessage(queuedMessage{Topic: "handled", Payloads: pls})
			proc.flushMessageQueue(proc.subs)
			require.Zero(t, calls)
			if combined {
				require.False(t, ch.IsClosed(), "terminal must wait for its data")
				require.Len(t, proc.messageQueue, 1)
				ReleaseResult(ch.Receive(nil, nil))
				proc.flushMessageQueue(proc.subs)
				require.Equal(t, 1, calls)
				require.Equal(t, lua.LString("formatted"), ch.buffer.Front().Value)
			} else {
				require.Equal(t, lua.LString("seed"), ch.buffer.Front().Value)
			}
			require.True(t, ch.IsClosed())
			require.Empty(t, proc.messageQueue)
		})
	}
}

func TestDeferredFormatterPreservesFramedAndClosedDelivery(t *testing.T) {
	for _, closed := range []bool{false, true} {
		t.Run(strconv.FormatBool(closed), func(t *testing.T) {
			proc := newSubscriptionMatrixProcess(t)
			defer proc.Close()
			ch, sub := addMatrixSubscription(t, proc, "handled")
			ch.buffer.PushBack(lua.LString("seed"))
			calls := 0
			proc.setTopicHandler("handled", func(context.Context, *lua.LState, pid.PID, string, []payload.Payload) lua.LValue {
				calls++
				return lua.LString("formatted")
			}, true)
			qm := subscriptionFrameMessage("handled", proc.epoch.Load(), sub.id, sub.gen.Load(), payload.Payloads{payload.NewString("value")})
			if closed {
				ReleaseResult(ch.Close(nil))
				qm.Payloads = payload.Payloads{payload.NewString("value")}
			}
			require.False(t, proc.deliverMessage(proc.subs, qm), "framed overflow and closed-channel messages must not be retained")
			require.Equal(t, 1, calls)
		})
	}
}

func BenchmarkDeliverMessage_Blocked(b *testing.B) {
	state := lua.NewState()
	defer state.Close()
	proc := &Process{state: state, ctx: payload.WithTranscoder(ctxapi.NewRootContext(), createInboxTestTranscoder()), subs: newTestSubscribeCtx()}
	ch := NewChannel(1)
	ch.buffer.PushBack(lua.LString("seed"))
	if _, err := proc.subs.addExisting("data", ch); err != nil {
		b.Fatal(err)
	}
	qm := queuedMessage{Topic: "data", Payloads: payload.Payloads{payload.NewPayload(map[string]any{"entries": []any{map[string]any{"name": "entry", "value": 42}}}, payload.Golang)}}
	b.ReportAllocs()
	for b.Loop() {
		if !proc.deliverMessage(proc.subs, qm) {
			b.Fatal("blocked message was dropped")
		}
	}
}
