// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"strconv"
	"strings"
	"testing"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
)

// A slow consumer on an externally-owned bounded channel must receive every
// producer message in order, then observe the channel closed (EOF), with zero
// loss — even when the backlog exceeds the buffer capacity and a terminal is
// queued behind the backlog in the same mailbox.
//
// Without the stalled-channel set, removing overflow-close lets the terminal
// (queued after msg1..msgN) close the channel before the retained backlog is
// delivered; the next flush sees the channel closed and drops the backlog.
func TestDeliverMessage_TerminalDoesNotOvertakeRetainedData(t *testing.T) {
	const topic = "ws@order"
	const bufCap = 2
	const total = 8

	proto, err := lua.CompileString(`
		local engine_ch = receive_channel()
		local got = {}
		while true do
			local v, ok = engine_ch:receive()
			if not ok then break end
			got[#got + 1] = v
		end
		return table.concat(got, ",")
	`, "ws_order.lua")
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	wsCh := NewChannel(bufCap)

	binder := func(l *lua.LState) error {
		l.SetGlobal("receive_channel", lua.LGoFunc(func(s *lua.LState) int {
			p := GetProcess(s)
			if p == nil {
				s.RaiseError("no process")
				return 0
			}
			if err := p.SubscribeExisting(topic, wsCh); err != nil {
				s.RaiseError("subscribe: %v", err)
				return 0
			}
			PushChannel(s, wsCh)
			return 1
		}))
		return nil
	}

	proc := mustNewProcess(t,
		WithProto(proto),
		WithModuleBinder(func(l *lua.LState) error {
			LoadModuleDef(l, ChannelModule)
			return nil
		}),
		WithModuleBinder(binder),
	)
	defer proc.Close()

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}

	procPID := pid.PID{Host: "h", UniqID: "u"}
	procPID = procPID.Precomputed()

	var out process.StepOutput

	// First step: run to the receive loop's first park on the empty ws channel.
	out.Reset()
	if err := proc.Step(nil, &out); err != nil {
		t.Fatalf("initial step: %v", err)
	}
	if got := proc.LiveSubscriptionCount(); got != 1 {
		t.Fatalf("subscription must be live, got %d", got)
	}

	// Enqueue total normal (non-frame) messages followed by a single terminal,
	// all in one mailbox batch while the consumer is parked. The buffer holds
	// only bufCap, so total-bufCap messages are retained, with the terminal
	// queued behind them.
	msgs := make([]*relay.Message, 0, total+1)
	for i := 0; i < total; i++ {
		msgs = append(msgs, &relay.Message{
			Topic:    topic,
			Payloads: payload.Payloads{payload.NewPayload(lua.LString(strconv.Itoa(i)), payload.Lua)},
		})
	}
	msgs = append(msgs, &relay.Message{
		Topic:    topic,
		Payloads: payload.Payloads{payload.NewTerminal()},
	})

	out.Reset()
	events := []process.Event{{Type: process.EventMessage, Data: &relay.Package{Source: procPID, Messages: msgs}}}
	if err := proc.Step(events, &out); err != nil {
		t.Fatalf("delivery step: %v", err)
	}

	// Drain steps until the process completes. The slow consumer receives one
	// value per step iteration as flush frees buffer slots.
	const maxSteps = 200
	for i := 0; i < maxSteps && out.Status() != process.StepDone; i++ {
		out.Reset()
		if err := proc.Step(nil, &out); err != nil {
			t.Fatalf("drain step %d: %v", i, err)
		}
	}
	if out.Status() != process.StepDone {
		t.Fatalf("process did not complete: status=%v", out.Status())
	}

	wantParts := make([]string, total)
	for i := 0; i < total; i++ {
		wantParts[i] = strconv.Itoa(i)
	}
	want := strings.Join(wantParts, ",")

	res := out.Result()
	if res == nil {
		t.Fatal("nil result")
	}
	got := resultDataString(res.Data())
	if got != want {
		t.Fatalf("consumer lost or reordered messages:\n got=%q\nwant=%q", got, want)
	}

	if got := proc.LiveSubscriptionCount(); got != 0 {
		t.Fatalf("subscription not reclaimed after terminal close, got %d", got)
	}
}

func resultDataString(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case lua.LString:
		return string(s)
	default:
		return ""
	}
}
