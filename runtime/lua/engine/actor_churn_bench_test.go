// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
)

func BenchmarkActorMessageBurst(b *testing.B) {
	for _, size := range []int{1, 64, 256, 1024} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			p := newChurnReceiver(b, 0)
			values := make([]payload.Payloads, size)
			events := make([]process.Event, size)
			for i := range values {
				values[i] = payload.Payloads{payload.NewPayload(lua.LInteger(i+1), payload.Lua)}
			}
			var out process.StepOutput
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				p.State().SetGlobal("processed", lua.LInteger(0))
				for j := range events {
					msg := relay.AcquireMessage()
					msg.Topic = "work"
					msg.Payloads = values[j]
					pkg := relay.AcquirePackage()
					pkg.Messages = append(pkg.Messages, msg)
					events[j] = process.Event{Type: process.EventMessage, Data: pkg}
				}
				out.Reset()
				if err := p.Step(events, &out); err != nil {
					b.Fatal(err)
				}
				clear(events)
			}
			b.StopTimer()
			requireCount := fmt.Sprint(size)
			if p.State().GetGlobal("processed").String() != requireCount || len(p.messageQueue) != 0 {
				b.Fatal("burst was not fully consumed")
			}
			b.ReportMetric(float64(size), "messages/op")
		})
	}
}

// This includes admission, scheduler wakeup and Lua delivery, with one live
// actor and one in-flight message. Every input is checked and acknowledged.
func BenchmarkActorScheduledDelivery(b *testing.B) {
	for _, selectMode := range []bool{false, true} {
		b.Run(fmt.Sprintf("select=%t", selectMode), func(b *testing.B) {
			receive, check := "inbox:receive()", "value"
			if selectMode {
				receive = "channel.select{inbox:case_receive(), stop:case_receive()}"
				check = "value.value"
			}
			script := fmt.Sprintf(`
				local inbox, stop = channel.new(0), channel.new(0)
				subscribe("work", inbox)
				ready()
				for i = 1, %d do
					local value = %s
					assert(%s == "hello")
					received()
				end
				return %d
			`, b.N+1, receive, check, b.N+1)
			ack, ready := make(chan struct{}, 1), make(chan struct{}, 1)
			p := mustNewProcess(b, WithScript(script, "actor_scheduled.lua"), WithModuleBinder(func(l *lua.LState) error {
				LoadModuleDef(l, ChannelModule)
				loadPubSubGlobals(l)
				l.SetGlobal("ready", l.NewFunction(func(_ *lua.LState) int { ready <- struct{}{}; return 0 }))
				l.SetGlobal("received", l.NewFunction(func(_ *lua.LState) int { ack <- struct{}{}; return 0 }))
				return nil
			}))
			ctx, fc := ctxapi.OpenFrameContext(ctxapi.NewRootContext())
			ctx = payload.WithTranscoder(ctx, createInboxTestTranscoder())
			ex := newBenchExecutor(newMockRegistry(), 1)
			ex.Start()
			defer func() { ex.Stop(); ctxapi.ReleaseFrameContext(fc) }()
			result, allowSuccess := make(chan error, 1), make(chan struct{})
			releaseSuccess := sync.OnceFunc(func() { close(allowSuccess) })
			defer releaseSuccess()
			pid := newTestPID("actor-churn")
			go func() {
				r, err := ex.Execute(ctx, pid, &mockProcess{p}, "", nil)
				if err == nil {
					err = r.Error
				}
				if err == nil && fmt.Sprint(r.Value.Data()) != fmt.Sprint(b.N+1) {
					err = fmt.Errorf("unexpected iteration count: %v", r.Value.Data())
				}
				if err == nil {
					<-allowSuccess // do not race the last acknowledgement
				}
				result <- err
			}()
			timeout := time.NewTimer(30 * time.Second)
			defer timeout.Stop()
			select {
			case <-ready:
			case err := <-result:
				b.Fatal(err)
			case <-timeout.C:
				b.Fatal("actor did not subscribe")
			}
			values := payload.Payloads{payload.New("hello")}
			deliver := func() {
				pkg := relay.NewPackage(testPID(), pid, "work", values...)
				if err := ex.sched.SendContext(context.Background(), pkg); err != nil {
					relay.ReleasePackage(pkg)
					b.Fatal(err)
				}
				select {
				case <-ack:
				case err := <-result:
					b.Fatal(err)
				case <-timeout.C:
					b.Fatal("actor did not acknowledge delivery")
				}
			}
			deliver()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				deliver()
			}
			b.StopTimer()
			releaseSuccess()
			if err := <-result; err != nil {
				b.Fatal(err)
			}
		})
	}
}
