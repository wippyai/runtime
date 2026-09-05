// SPDX-License-Identifier: MPL-2.0
package actor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
)

func messageEvent(topic string, data []byte) process.Event {
	return process.Event{Type: process.EventMessage, Data: relay.NewPackage(pid.PID{Node: "local", Host: "actors", UniqID: "sender"}, pid.PID{}, topic, payload.NewPayload(data, payload.Bytes))}
}

func admit(t testing.TB, m *Mailbox, e process.Event) process.Event {
	t.Helper()
	out, err := m.AdmitEvent(e)
	if err != nil {
		relay.ReleasePackage(e.Data.(*relay.Package))
		t.Fatal(err)
	}
	return out
}

func TestMailboxRingAndOwnership(t *testing.T) {
	m := NewMailbox(Limits{Capacity: 3, Bytes: 4096, MessageBytes: 1024})
	defer m.Close()
	for round := 0; round < 10; round++ {
		for i := 0; i < 3; i++ {
			data := []byte{byte(i)}
			e := admit(t, m, messageEvent(fmt.Sprint(i), data))
			data[0] = 99
			m.Deliver(e)
		}
		e := messageEvent("overflow", nil)
		if _, err := m.AdmitEvent(e); !errors.Is(err, ErrOverloaded) {
			t.Fatalf("overload: %v", err)
		}
		relay.ReleasePackage(e.Data.(*relay.Package))
		for i := 0; i < 3; i++ {
			got, err := m.Take()
			if err != nil || got == nil || got.Topic != fmt.Sprint(i) || got.Payloads[0].Data[0] != byte(i) {
				t.Fatalf("order/ownership: %+v %v", got, err)
			}
		}
		if m.Ready() || m.count != 0 || m.bytes != 0 {
			t.Fatal("retained admission charge")
		}
	}
}

func TestMailboxIngressBudgetAndDiscard(t *testing.T) {
	m := NewMailbox(Limits{Capacity: 2, Bytes: 4096, MessageBytes: 1024})
	first := admit(t, m, messageEvent("one", nil))
	second := admit(t, m, messageEvent("two", nil))
	e := messageEvent("three", nil)
	if _, err := m.AdmitEvent(e); !errors.Is(err, ErrOverloaded) {
		t.Fatal(err)
	}
	relay.ReleasePackage(e.Data.(*relay.Package))
	first.Data.(process.EventDiscarder).DiscardEvent()
	first.Data.(process.EventDiscarder).DiscardEvent()
	m.Deliver(second)
	last := admit(t, m, messageEvent("three", nil))
	m.Close()
	last.Data.(process.EventDiscarder).DiscardEvent()
	if m.count != 0 || m.bytes != 0 {
		t.Fatalf("leaked charge: %d %d", m.count, m.bytes)
	}
	if _, err := m.Take(); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}

func TestMailboxRejectBatchAtomically(t *testing.T) {
	m := NewMailbox(DefaultLimits())
	defer m.Close()
	e := messageEvent("valid", []byte("data"))
	pkg := e.Data.(*relay.Package)
	pkg.AddMessage("invalid", payload.NewPayload([]byte("{"), payload.JSON))
	if _, err := m.AdmitEvent(e); !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("malformed JSON: %v", err)
	}
	if m.count != 0 || m.bytes != 0 || m.Ready() {
		t.Fatal("partial admission")
	}
	if len(pkg.Messages) != 2 {
		t.Fatal("rejected package must remain caller owned")
	}
	relay.ReleasePackage(pkg)
}

func TestMailboxConcurrentAdmission(t *testing.T) {
	m := NewMailbox(Limits{Capacity: 128, Bytes: 1 << 20, MessageBytes: 1024})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 16; j++ {
				e := admit(t, m, messageEvent("data", nil))
				m.Deliver(e)
			}
		}()
	}
	wg.Wait()
	for i := 0; i < 128; i++ {
		msg, err := m.Take()
		if err != nil || msg == nil {
			t.Fatalf("lost message %d: %v", i, err)
		}
	}
	if m.count != 0 || m.bytes != 0 {
		t.Fatal("leaked charge")
	}
	m.Close()
}

func BenchmarkMailboxRoundTrip(b *testing.B) {
	for _, depth := range []int{1, 128, 4096} {
		b.Run(fmt.Sprint(depth), func(b *testing.B) {
			m := NewMailbox(Limits{Capacity: depth, Bytes: 16 << 20, MessageBytes: 1 << 20})
			defer m.Close()
			data := make([]byte, 64)
			for i := 0; i < depth-1; i++ {
				m.Deliver(admit(b, m, messageEvent("data", data)))
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m.Deliver(admit(b, m, messageEvent("data", data)))
				if _, err := m.Take(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestMailboxInvalidPayloads(t *testing.T) {
	cases := []struct {
		p           payload.Payload
		want        error
		name, topic string
	}{
		{name: "reserved", topic: "@pid/events", p: payload.NewString("x"), want: ErrInvalidMessage},
		{name: "empty topic", topic: "", p: payload.NewString("x"), want: ErrInvalidMessage},
		{name: "bad utf8", topic: "data", p: payload.NewPayload([]byte{0xff}, payload.String), want: ErrInvalidMessage},
		{name: "non-UTF8 json", topic: "data", p: payload.NewPayload([]byte{34, 255, 34}, payload.JSON), want: ErrInvalidMessage},
		{name: "bad json", topic: "data", p: payload.NewPayload([]byte("{"), payload.JSON), want: ErrInvalidMessage},
		{name: "object handle", topic: "data", p: payload.New(map[string]any{"x": 1}), want: ErrUnsupportedPayload},
		{name: "too large", topic: "data", p: payload.NewPayload(make([]byte, 1024), payload.Bytes), want: ErrTooLarge},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMailbox(Limits{Capacity: 2, Bytes: 4096, MessageBytes: 1024})
			defer m.Close()
			e := messageEvent(tt.topic, nil)
			pkg := e.Data.(*relay.Package)
			pkg.Messages[0].Payloads = payload.Payloads{tt.p}
			defer relay.ReleasePackage(pkg)
			if _, err := m.AdmitEvent(e); !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
			if m.count != 0 || m.bytes != 0 {
				t.Fatal("rejected payload retained budget")
			}
		})
	}
}

func TestReceiveRetainsMessageAcrossRingReuseAndClose(t *testing.T) {
	m := NewMailbox(Limits{Capacity: 1, Bytes: 4096, MessageBytes: 1024})
	defer m.Close()
	ctx := WithMailbox(context.Background(), m)
	host := NewHost()
	m.Deliver(admit(t, m, messageEvent("first", []byte("owned"))))
	first, err := host.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		m.Deliver(admit(t, m, messageEvent("next", []byte("replacement"))))
		next, err := host.Receive(ctx)
		if err != nil || next.Topic != "next" {
			t.Fatalf("receive after reuse: %+v %v", next, err)
		}
		next.Payloads[0].Data[0] = '!'
	}
	if m.count != 0 || m.bytes != 0 || m.Ready() {
		t.Fatal("receive retained queue charge")
	}
	if _, err := host.Receive(ctx); !errors.Is(err, errSchedulerRequired) {
		t.Fatalf("empty mailbox without scheduler: %v", err)
	}
	m.Close()
	if first.Topic != "first" || string(first.Payloads[0].Data) != "owned" {
		t.Fatalf("retained message changed: %+v", first)
	}
	if _, err := host.Receive(ctx); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed mailbox: %v", err)
	}
}
