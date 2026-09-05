// SPDX-License-Identifier: MPL-2.0
package engine

import (
	"encoding/binary"
	"os"
	"testing"

	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	secapi "github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/actor"
	secsys "github.com/wippyai/runtime/system/security"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
)

func newMessagingActor(t testing.TB) (*ActorProcess, pid.PID, pid.PID) {
	t.Helper()
	ctx, frame := ctxapi.OpenFrameContext(ctxapi.NewRootContext())
	t.Cleanup(func() { _ = frame.Close() })
	self := pid.PID{Node: "local", Host: "actors", UniqID: "counter"}
	sender := pid.PID{Node: "local", Host: "actors", UniqID: "client"}
	if err := secapi.SetActor(ctx, secapi.Actor{ID: "indexer"}); err != nil {
		t.Fatal(err)
	}
	if err := secapi.SetScope(ctx, secsys.NewScope([]secapi.Policy{actorReplyPolicy{sender: self.String(), target: sender.String()}})); err != nil {
		t.Fatal(err)
	}
	if err := runtimeapi.SetFramePID(ctx, self); err != nil {
		t.Fatal(err)
	}
	rt, err := wasmrt.NewWithConfig(ctx, &wasmrt.Config{CloseOnContextDone: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close(ctx) })
	if err := rt.RegisterHost(actor.NewHost()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/actor.wasm")
	if err != nil {
		t.Fatal(err)
	}
	mod, err := rt.LoadComponent(ctx, data)
	if err != nil {
		t.Fatal(err)
	}
	if err := mod.Compile(ctx); err != nil {
		t.Fatal(err)
	}
	p := NewActorProcess(NewProcess(mod, "", wasmapi.WASIConfig{}, wasmapi.LimitsConfig{}, nil), actor.DefaultLimits(), nil)
	t.Cleanup(p.Close)
	if err := p.Init(ctx, "run", nil); err != nil {
		t.Fatal(err)
	}
	return p, self, sender
}

func TestActorGuestMessaging(t *testing.T) {
	p, self, sender := newMessagingActor(t)
	var out process.StepOutput
	step := func(events ...process.Event) {
		t.Helper()
		out.Reset()
		if err := p.Step(events, &out); err != nil {
			t.Fatal(err)
		}
	}
	step()
	if !out.IsIdle() || out.Count() != 0 {
		t.Fatalf("receive should park locally: %v", out.Status())
	}
	for i := uint64(1); i <= 3; i++ {
		pkg := relay.NewPackage(sender, self, "increment")
		event, err := p.EventAdmission().AdmitEvent(process.Event{Type: process.EventMessage, Data: pkg})
		if err != nil {
			t.Fatal(err)
		}
		step(event)
		if out.Count() != 1 {
			t.Fatalf("want one send, status %v", out.Status())
		}
		y := out.Yields()[0]
		cmd, ok := y.Cmd.(*process.SendCmd)
		if !ok {
			t.Fatalf("unexpected command %T", y.Cmd)
		}
		if cmd.From.String() != self.String() || cmd.To.String() != sender.String() || cmd.Topic != "count" {
			t.Fatalf("wrong envelope: %+v", cmd)
		}
		bytes, ok := cmd.Payloads[0].Data().([]byte)
		if !ok || len(bytes) != 8 || binary.LittleEndian.Uint64(bytes) != i {
			t.Fatalf("state not retained: %v", bytes)
		}
		step(process.Event{Type: process.EventYieldComplete, Tag: y.Tag, Data: process.SendResult{}})
		if !out.IsIdle() {
			t.Fatalf("expected mailbox wait, got %v", out.Status())
		}
	}
	pkg := relay.NewPackage(sender, self, "stop")
	e, err := p.EventAdmission().AdmitEvent(process.Event{Type: process.EventMessage, Data: pkg})
	if err != nil {
		t.Fatal(err)
	}
	step(e)
	if !out.IsDone() {
		t.Fatalf("stop did not exit: %v", out.Status())
	}
}

type actorReplyPolicy struct{ sender, target string }

func (actorReplyPolicy) ID() registry.ID { return registry.ParseID("test:reply") }
func (p actorReplyPolicy) Evaluate(principal secapi.Actor, action, resource string, meta attrs.Bag) secapi.Result {
	if principal.ID == "indexer" && action == "process.send" && resource == p.target && meta["pid"] == p.sender {
		return secapi.Allow
	}
	return secapi.Deny
}

// BenchmarkActorGuestRoundTrip includes the actual guest, ingress copying,
// Canonical ABI and two Asyncify resumptions. Dispatcher/network transport and
// scheduler routing are deliberately measured separately.
func BenchmarkActorGuestRoundTrip(b *testing.B) {
	p, self, sender := newMessagingActor(b)
	var out process.StepOutput
	if err := p.Step(nil, &out); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		pkg := relay.NewPackage(sender, self, "increment")
		event, err := p.EventAdmission().AdmitEvent(process.Event{Type: process.EventMessage, Data: pkg})
		if err != nil {
			b.Fatal(err)
		}
		out.Reset()
		if err = p.Step([]process.Event{event}, &out); err != nil {
			b.Fatal(err)
		}
		if out.Count() != 1 {
			b.Fatal("missing reply")
		}
		tag := out.Yields()[0].Tag
		out.Reset()
		if err = p.Step([]process.Event{{Type: process.EventYieldComplete, Tag: tag, Data: process.SendResult{}}}, &out); err != nil {
			b.Fatal(err)
		}
		if !out.IsIdle() {
			b.Fatal("actor did not park")
		}
	}
}

func TestActorGuestIdentityAndEmptyTryReceive(t *testing.T) {
	p, self, sender := newMessagingActor(t)
	var out process.StepOutput
	if err := p.Step(nil, &out); err != nil {
		t.Fatal(err)
	}
	event, err := p.EventAdmission().AdmitEvent(process.Event{Type: process.EventMessage, Data: relay.NewPackage(sender, self, "probe")})
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err = p.Step([]process.Event{event}, &out); err != nil {
		t.Fatal(err)
	}
	if out.Count() != 1 {
		t.Fatal("missing identity reply")
	}
	cmd := out.Yields()[0].Cmd.(*process.SendCmd)
	if cmd.Topic != "identity" || string(cmd.Payloads[0].Data().([]byte)) != self.String() {
		t.Fatalf("wrong identity: %+v", cmd)
	}
}

func TestActorGuestDeniedSendAndQueuedMessage(t *testing.T) {
	p, self, sender := newMessagingActor(t)
	var out process.StepOutput
	if err := p.Step(nil, &out); err != nil {
		t.Fatal(err)
	}
	admit := func(topic string) process.Event {
		e, err := p.EventAdmission().AdmitEvent(process.Event{Type: process.EventMessage, Data: relay.NewPackage(sender, self, topic)})
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	out.Reset()
	if err := p.Step([]process.Event{admit("deny")}, &out); err != nil {
		t.Fatal(err)
	}
	if out.Count() != 1 {
		t.Fatal("guest failed to recover from policy denial")
	}
	y := out.Yields()[0]
	if cmd := y.Cmd.(*process.SendCmd); cmd.To.String() != sender.String() || cmd.Topic != "count" {
		t.Fatal("denied send escaped")
	}
	// Receive a message while the guest is waiting for its send to complete.
	out.Reset()
	if err := p.Step([]process.Event{admit("increment")}, &out); err != nil {
		t.Fatal(err)
	}
	if out.Count() != 0 {
		t.Fatal("message resumed unrelated pending send")
	}
	out.Reset()
	if err := p.Step([]process.Event{{Type: process.EventYieldComplete, Tag: y.Tag, Data: process.SendResult{}}}, &out); err != nil {
		t.Fatal(err)
	}
	if out.Count() != 1 {
		t.Fatal("queued message lost after send completion")
	}
	cmd := out.Yields()[0].Cmd.(*process.SendCmd)
	if binary.LittleEndian.Uint64(cmd.Payloads[0].Data().([]byte)) != 2 {
		t.Fatal("state or queued message lost")
	}
}
