// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
)

// TestLockService_SendReapsOnExitEvent covers the relay receiver: a process-exit
// event on the topology topic reaps that holder's locks and leaves others.
func TestLockService_SendReapsOnExitEvent(t *testing.T) {
	s := newLockSvc(t)
	holder := pid.PID{Node: "n1", Host: "h", UniqID: "x"}
	other := pid.PID{Node: "n1", Host: "h", UniqID: "y"}
	mustAcquire(t, s, "L1", holder)
	mustAcquire(t, s, "L2", other)

	pkg := relay.NewPackage(holder, s.self, topology.TopicEvents, payload.New(&topology.ExitEvent{From: holder}))
	if err := s.Send(pkg); err != nil {
		t.Fatalf("send exit event: %v", err)
	}

	if _, ok, _ := s.Holder("L1"); ok {
		t.Fatalf("L1 of the exited holder should be reaped")
	}
	if _, ok, _ := s.Holder("L2"); !ok {
		t.Fatalf("L2 of a live holder must remain")
	}
}

// TestLockService_SendIgnoresUnrelated covers the topic filter and the payload
// type assertion: a non-events topic, or a non-ExitEvent payload, must not reap.
func TestLockService_SendIgnoresUnrelated(t *testing.T) {
	s := newLockSvc(t)
	holder := pid.PID{Node: "n1", Host: "h", UniqID: "x"}
	mustAcquire(t, s, "L", holder)

	wrongTopic := relay.NewPackage(holder, s.self, relay.Topic("other.topic"), payload.New(&topology.ExitEvent{From: holder}))
	if err := s.Send(wrongTopic); err != nil {
		t.Fatalf("send wrong topic: %v", err)
	}
	if _, ok, _ := s.Holder("L"); !ok {
		t.Fatalf("lock wrongly reaped from a non-events topic")
	}

	wrongPayload := relay.NewPackage(holder, s.self, topology.TopicEvents, payload.New([]byte("not-an-exit-event")))
	if err := s.Send(wrongPayload); err != nil {
		t.Fatalf("send non-exit payload: %v", err)
	}
	if _, ok, _ := s.Holder("L"); !ok {
		t.Fatalf("lock wrongly reaped from a non-ExitEvent payload")
	}
}

// TestLockService_ContextRoundTrip covers WithLockService/GetLockService for both
// the app-context-present and absent paths.
func TestLockService_ContextRoundTrip(t *testing.T) {
	s := newLockSvc(t)

	bare := context.Background()
	if GetLockService(bare) != nil {
		t.Fatalf("GetLockService without app context must be nil")
	}
	if WithLockService(bare, s) != bare {
		t.Fatalf("WithLockService without app context must return ctx unchanged")
	}

	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	ctx = WithLockService(ctx, s)
	if got := GetLockService(ctx); got != s {
		t.Fatalf("GetLockService = %v, want the stored service", got)
	}
}
