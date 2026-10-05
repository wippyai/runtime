// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/topology"
)

// TestMonitorDownIsDeliveredWithoutFailingTheProcess verifies that losing a
// monitored process to a departed node reaches the monitor as an event and
// leaves a process that does not trap links running: a monitor is not a link.
func TestMonitorDownIsDeliveredWithoutFailingTheProcess(t *testing.T) {
	proc := mustNewProcess(t, WithScript("return 1", "test.lua"))
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	ch, err := proc.Subscribe(topology.TopicEvents, 10)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}
	proc.messageQueue = append(proc.messageQueue, queuedMessage{
		Topic:    topology.TopicEvents,
		Source:   topology.SystemPID,
		Payloads: []payload.Payload{payload.New(&topology.ExitEvent{From: testPID(), Kind: topology.MonitorDown})},
	})
	proc.flushMessageQueue(proc.subs)

	if err := proc.LinkDownError(); err != nil {
		t.Fatalf("a monitor failed the process: %v", err)
	}
	result := ch.Receive(nil, nil)
	if result == nil {
		t.Fatal("monitor down was not delivered to the events channel")
	}
	updates := result.GetUpdates()
	if len(updates) != 1 {
		t.Fatalf("expected one update, got %d", len(updates))
	}
	ReleaseResult(result)
}
