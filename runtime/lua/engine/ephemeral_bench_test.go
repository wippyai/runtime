// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"testing"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
)

// BenchmarkRouter_RegisterValueClose measures the steady-state hot path:
// allocate a channel, register, deliver a value+close frame, channel closes
// and the router entry is gone. This is the per-iteration cost of every
// time.after call in the scheduler / job_worker loop pattern.
func BenchmarkRouter_RegisterValueClose(b *testing.B) {
	proto, err := lua.CompileString(`
		local hold = channel.new(1)
		subscribe("hold", hold)
		hold:receive()
		return "ok"
	`, "bench.lua")
	if err != nil {
		b.Fatal(err)
	}
	proc, err := NewProcess(WithProto(proto))
	if err != nil {
		b.Fatal(err)
	}
	defer proc.Close()
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		b.Fatal(err)
	}
	LoadModuleDef(proc.State(), ChannelModule)
	loadPubSubGlobals(proc.State())

	// Drive the script until it parks on hold:receive().
	var output process.StepOutput
	for i := 0; i < 50; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			b.Fatal(err)
		}
		if output.Status() == process.StepIdle {
			break
		}
	}

	payloadStr := payload.NewPayload(lua.LString("tick"), payload.Lua)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch := NewChannel(1)
		chID, epoch, _ := proc.RegisterEphemeral(ch, nil, nil, OverflowDrop)
		frame := &EphemeralFrame{
			Epoch:    epoch,
			ChID:     chID,
			Gen:      0,
			HasValue: true,
			Close:    true,
			Payloads: payload.Payloads{payloadStr},
		}
		events := []process.Event{{
			Type: process.EventMessage,
			Data: &relay.Package{
				Messages: []*relay.Message{{
					Topic:    TopicEphemeral,
					Payloads: payload.Payloads{NewEphemeralFramePayload(frame)},
				}},
			},
		}}
		output.Reset()
		if err := proc.Step(events, &output); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()

	if got := proc.router.Load().size(); got != 0 {
		b.Errorf("router should be empty after bench, got %d", got)
	}
}

// BenchmarkRouter_RouteOnlyValue measures the route-only cost, isolating
// the per-frame work from the register/close work. Pre-allocates one
// long-lived entry and pumps value-only frames through it.
func BenchmarkRouter_RouteOnlyValue(b *testing.B) {
	proto, err := lua.CompileString(`
		local hold = channel.new(1)
		subscribe("hold", hold)
		hold:receive()
		return "ok"
	`, "bench.lua")
	if err != nil {
		b.Fatal(err)
	}
	proc, err := NewProcess(WithProto(proto))
	if err != nil {
		b.Fatal(err)
	}
	defer proc.Close()
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		b.Fatal(err)
	}
	LoadModuleDef(proc.State(), ChannelModule)
	loadPubSubGlobals(proc.State())

	var output process.StepOutput
	for i := 0; i < 50; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			b.Fatal(err)
		}
		if output.Status() == process.StepIdle {
			break
		}
	}

	// One long-lived entry with a 16-slot buffer so the router never
	// overflows during the bench (OverflowDrop on full is also free).
	ch := NewChannel(16)
	chID, epoch, _ := proc.RegisterEphemeral(ch, nil, nil, OverflowDrop)
	payloadStr := payload.NewPayload(lua.LString("x"), payload.Lua)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		frame := &EphemeralFrame{
			Epoch:    epoch,
			ChID:     chID,
			HasValue: true,
			Payloads: payload.Payloads{payloadStr},
		}
		events := []process.Event{{
			Type: process.EventMessage,
			Data: &relay.Package{
				Messages: []*relay.Message{{
					Topic:    TopicEphemeral,
					Payloads: payload.Payloads{NewEphemeralFramePayload(frame)},
				}},
			},
		}}
		output.Reset()
		if err := proc.Step(events, &output); err != nil {
			b.Fatal(err)
		}
		// Drain the channel so the buffer doesn't fill and trigger
		// the overflow path.
		for ch.buffer.Len() > 0 {
			ch.buffer.Remove(ch.buffer.Front())
		}
	}
}
