// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"strconv"
	"testing"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/topology"
)

// queueOutdated appends an OUTDATED event to the process message queue.
func queueOutdated(proc *Process, sources ...registry.ID) {
	proc.messageQueue = append(proc.messageQueue, queuedMessage{
		Topic:    topology.TopicEvents,
		Source:   testPID(),
		Payloads: []payload.Payload{payload.New(&topology.OutdatedEvent{Sources: sources})},
	})
}

// TestUpgradableOption covers the per-instance upgradable flag: default false,
// settable, and independent of trap_links.
func TestUpgradableOption(t *testing.T) {
	proc := mustNewProcess(t, WithScript("return 1", "test.lua"))
	defer proc.Close()

	if proc.IsUpgradable() {
		t.Fatal("upgradable should default to false")
	}
	if proc.IsTrapLinks() {
		t.Fatal("trap_links should default to false")
	}

	proc.SetUpgradable(true)
	if !proc.IsUpgradable() {
		t.Fatal("upgradable should be true after SetUpgradable(true)")
	}
	if proc.IsTrapLinks() {
		t.Fatal("setting upgradable must not affect trap_links")
	}

	proc.SetTrapLinks(true)
	if !proc.IsUpgradable() {
		t.Fatal("setting trap_links must not affect upgradable")
	}

	proc.SetUpgradable(false)
	if proc.IsUpgradable() {
		t.Fatal("upgradable should be false after SetUpgradable(false)")
	}
}

// TestDeliverOutdatedUpgradable verifies an OUTDATED event on TopicEvents is
// delivered to the events channel when upgradable, surfacing kind + sources.
func TestDeliverOutdatedUpgradable(t *testing.T) {
	proc := mustNewProcess(t, WithScript("return 1", "test.lua"))
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	proc.SetUpgradable(true)

	ch, err := proc.Subscribe(topology.TopicEvents, 10)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	ev := &topology.OutdatedEvent{Sources: []registry.ID{
		registry.NewID("app", "worker"),
		registry.NewID("app", "lib"),
	}}
	proc.messageQueue = append(proc.messageQueue, queuedMessage{
		Topic:    topology.TopicEvents,
		Source:   testPID(),
		Payloads: []payload.Payload{payload.New(ev)},
	})

	proc.flushMessageQueue(proc.subs)

	result := ch.Receive(nil, nil)
	if result == nil {
		t.Fatal("expected events channel to receive OUTDATED")
	}
	updates := result.GetUpdates()
	if len(updates) != 1 {
		t.Fatalf("expected one update, got %d", len(updates))
	}
	val := updates[0].GetResult()[0]
	ReleaseResult(result)

	tbl, ok := val.(*lua.LTable)
	if !ok {
		t.Fatalf("expected a table, got %T", val)
	}
	if kind := tbl.RawGetString("kind").String(); kind != topology.Outdated {
		t.Fatalf("expected kind %q, got %q", topology.Outdated, kind)
	}
	sources, ok := tbl.RawGetString("sources").(*lua.LTable)
	if !ok {
		t.Fatal("expected sources to be a table")
	}
	if got := sources.RawGetInt(1).String(); got != "app:worker" {
		t.Fatalf("expected sources[1]=app:worker, got %q", got)
	}
	if got := sources.RawGetInt(2).String(); got != "app:lib" {
		t.Fatalf("expected sources[2]=app:lib, got %q", got)
	}
}

// TestDeliverOutdatedDroppedWhenNotUpgradable verifies an OUTDATED event is
// dropped silently for a non-upgradable process and never terminates it.
func TestDeliverOutdatedDroppedWhenNotUpgradable(t *testing.T) {
	proc := mustNewProcess(t, WithScript("return 1", "test.lua"))
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	// Default: not upgradable.
	ch, err := proc.Subscribe(topology.TopicEvents, 10)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	ev := &topology.OutdatedEvent{Sources: []registry.ID{registry.NewID("app", "worker")}}
	proc.messageQueue = append(proc.messageQueue, queuedMessage{
		Topic:    topology.TopicEvents,
		Source:   testPID(),
		Payloads: []payload.Payload{payload.New(ev)},
	})

	proc.flushMessageQueue(proc.subs)

	if len(proc.messageQueue) != 0 {
		t.Fatalf("expected OUTDATED to be dropped, queue has %d", len(proc.messageQueue))
	}
	if proc.LinkDownError() != nil {
		t.Fatalf("OUTDATED must never arm termination, got %v", proc.LinkDownError())
	}
	if proc.pendingOutdated != nil {
		t.Fatal("non-upgradable process must not retain a pending OUTDATED")
	}
	if n := ch.Size(); n != 0 {
		t.Fatalf("expected events channel empty, got %d buffered", n)
	}
}

// TestOutdatedCoalescedStaysO1 verifies repeated OUTDATED events for an
// upgradable non-consumer collapse to a single pending slot (unioned sources),
// so N invalidations never grow the retained queue.
func TestOutdatedCoalescedStaysO1(t *testing.T) {
	proc := mustNewProcess(t, WithScript("return 1", "test.lua"))
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	proc.SetUpgradable(true) // never subscribes to events()

	const n = 25
	for i := 0; i < n; i++ {
		// Each invalidation has a distinct source plus a shared one.
		queueOutdated(proc,
			registry.NewID("app", "w"+strconv.Itoa(i)),
			registry.NewID("app", "shared"),
		)
		proc.flushMessageQueue(proc.subs)
	}

	if len(proc.messageQueue) != 0 {
		t.Fatalf("retained queue must stay empty, got %d entries", len(proc.messageQueue))
	}
	if proc.pendingOutdated == nil {
		t.Fatal("expected a single coalesced pending OUTDATED")
	}
	// Union: n distinct workers + 1 shared, deduplicated.
	if got := len(proc.pendingOutdated.Sources); got != n+1 {
		t.Fatalf("expected %d unioned sources, got %d", n+1, got)
	}
}

// TestOutdatedDeliveredAfterSubscribe verifies an OUTDATED that arrives before
// the process subscribes to events() is retained and delivered once the
// subscription appears (retain-then-flush-on-subscribe).
func TestOutdatedDeliveredAfterSubscribe(t *testing.T) {
	proc := mustNewProcess(t, WithScript("return 1", "test.lua"))
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	proc.SetUpgradable(true)

	// Arrives before any events() subscription exists.
	queueOutdated(proc, registry.NewID("app", "worker"))
	proc.flushMessageQueue(proc.subs)

	if proc.pendingOutdated == nil {
		t.Fatal("OUTDATED before subscribe must be retained as pending")
	}
	if len(proc.messageQueue) != 0 {
		t.Fatalf("pending OUTDATED must not sit in the queue, got %d", len(proc.messageQueue))
	}

	// Subscription appears; next flush must deliver the retained event.
	ch, err := proc.Subscribe(topology.TopicEvents, 10)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}
	proc.flushMessageQueue(proc.subs)

	if proc.pendingOutdated != nil {
		t.Fatal("pending OUTDATED must clear after delivery")
	}
	result := ch.Receive(nil, nil)
	if result == nil {
		t.Fatal("expected events channel to receive the retained OUTDATED")
	}
	val := result.GetUpdates()[0].GetResult()[0]
	ReleaseResult(result)
	tbl, ok := val.(*lua.LTable)
	if !ok {
		t.Fatalf("expected a table, got %T", val)
	}
	if kind := tbl.RawGetString("kind").String(); kind != topology.Outdated {
		t.Fatalf("expected kind %q, got %q", topology.Outdated, kind)
	}
	if src := tbl.RawGetString("sources").(*lua.LTable).RawGetInt(1).String(); src != "app:worker" {
		t.Fatalf("expected sources[1]=app:worker, got %q", src)
	}
}
