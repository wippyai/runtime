// SPDX-License-Identifier: MPL-2.0

package process_test

import (
	"fmt"
	"testing"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	procapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/lua/engine"
	processmod "github.com/wippyai/runtime/runtime/lua/modules/process"
)

// Measure the public mailbox paths on a long-lived actor, excluding startup.
// Unlike generic engine subscriptions, these retain unread messages on upgrade.
func BenchmarkProcessMailbox(b *testing.B) {
	for _, inbox := range []bool{false, true} {
		for _, burst := range []int{1, 64} {
			for _, selectMode := range []bool{false, true} {
				b.Run(fmt.Sprintf("inbox=%t/burst=%d/select=%t", inbox, burst, selectMode), func(b *testing.B) {
					subscribe := `process.listen("work")`
					value := "value"
					if inbox {
						subscribe = "process.inbox()"
						value = "value:data()"
					}
					receive := "inbox:receive()"
					if selectMode {
						receive = "channel.select{inbox:case_receive(), stop:case_receive()}.value"
					}
					p, err := engine.NewProcess(engine.WithScript(fmt.Sprintf(`
						return { main = function()
							local inbox, stop = %s, channel.new(0)
							processed = 0
							while true do
								local value = %s
								assert(%s == processed + 1, "message lost or reordered")
								processed = processed + 1
							end
						end }
					`, subscribe, receive, value), "mailbox_bench.lua"), engine.WithModuleBinder(func(l *lua.LState) error {
						engine.LoadModuleDef(l, engine.ChannelModule)
						mod, _ := processmod.Module.Build()
						l.SetGlobal("process", mod)
						return nil
					}))
					if err != nil {
						b.Fatal(err)
					}
					ctx, frame := ctxapi.OpenFrameContext(security.SetStrictMode(ctxapi.NewRootContext(), false))
					defer ctxapi.ReleaseFrameContext(frame)
					defer p.Close()
					if err := p.Init(ctx, "main", nil); err != nil {
						b.Fatal(err)
					}
					var out procapi.StepOutput
					if err := p.Step(nil, &out); err != nil {
						b.Fatal(err)
					}
					values := make([]payload.Payloads, burst)
					events := make([]procapi.Event, burst)
					for i := range values {
						values[i] = payload.Payloads{payload.NewPayload(lua.LInteger(i+1), payload.Lua)}
					}
					self := pid.PID{Host: "bench", UniqID: "mailbox"}
					self = self.Precomputed()
					deliver := func() {
						p.State().SetGlobal("processed", lua.LInteger(0))
						for j := range events {
							pkg := relay.AcquirePackage()
							pkg.Source = self
							pkg.Target = self
							msg := relay.AcquireMessage()
							msg.Topic, msg.Payloads = "work", values[j]
							pkg.Messages = append(pkg.Messages, msg)
							events[j] = procapi.Event{Type: procapi.EventMessage, Data: pkg}
						}
						out.Reset()
						if err := p.Step(events, &out); err != nil {
							b.Fatal(err)
						}
						clear(events)
						if out.Status() != procapi.StepIdle {
							b.Fatalf("mailbox failed: %v", out.Status())
						}
					}
					deliver() // Warm pools and mailbox backing storage.
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						deliver()
					}
					b.StopTimer()
					if p.State().GetGlobal("processed").String() != fmt.Sprint(burst) {
						b.Fatal("burst was not fully consumed")
					}
					b.ReportMetric(float64(burst), "messages/op")
				})
			}
		}
	}
}
