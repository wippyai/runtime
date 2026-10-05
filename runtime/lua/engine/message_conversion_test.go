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
