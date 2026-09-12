// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	"github.com/wippyai/runtime/system/eventbus"
	systemkv "github.com/wippyai/runtime/system/kv"
)

func newParticipantTestInventory(t testing.TB, max int) (*participantInventory, *systemkv.Service) {
	t.Helper()
	bus := eventbus.NewBus()
	engine := systemkv.NewService("participants", bus, nil)
	if _, err := engine.Start(context.Background()); err != nil {
		t.Fatalf("start kv engine: %v", err)
	}
	t.Cleanup(func() {
		_ = engine.Stop(context.Background())
		bus.Stop()
	})
	inventory, err := newParticipantInventory(engine, engine.Get, max)
	if err != nil {
		t.Fatalf("new participant inventory: %v", err)
	}
	return inventory, engine
}

func TestParticipantInventoryEnrollIdempotentAndConflictingIncarnation(t *testing.T) {
	inventory, _ := newParticipantTestInventory(t, 4)
	ctx := context.Background()

	if err := inventory.enroll(ctx, "node-1", "inc-1"); err != nil {
		t.Fatalf("first enrollment: %v", err)
	}
	if err := inventory.enroll(ctx, "node-1", "inc-1"); err != nil {
		t.Fatalf("same enrollment must be idempotent: %v", err)
	}
	if err := inventory.enroll(ctx, "node-1", "inc-2"); !errors.Is(err, ErrParticipantIncarnationConflict) {
		t.Fatalf("different incarnation error = %v, want conflict", err)
	}

	members, check, err := inventory.readSnapshot()
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if len(members) != 1 || members[pid.NodeID("node-1")] != "inc-1" {
		t.Fatalf("members = %#v", members)
	}
	if check.Key != participantsKey || check.Kind != kvapi.TxnCheck || check.Cond != kvapi.CondVersion || check.Expect == 0 {
		t.Fatalf("inventory check = %+v", check)
	}
}

func TestParticipantInventoryConcurrentEnrollmentsPreserveMembers(t *testing.T) {
	inventory, _ := newParticipantTestInventory(t, 32)
	const count = 20
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- inventory.enroll(context.Background(), pid.NodeID("node"+strconv.Itoa(i)), "inc-"+strconv.Itoa(i))
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent enrollment: %v", err)
		}
	}

	members, _, err := inventory.readSnapshot()
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if len(members) != count {
		t.Fatalf("members count = %d, want %d: %#v", len(members), count, members)
	}
}

func TestParticipantInventoryMissingAndMalformedRecordFailClosed(t *testing.T) {
	inventory, engine := newParticipantTestInventory(t, 4)
	if _, _, err := inventory.readSnapshot(); !errors.Is(err, kvapi.ErrKeyNotFound) {
		t.Fatalf("missing snapshot error = %v, want ErrKeyNotFound", err)
	}
	if _, err := engine.Set(participantsKey, []byte{0xc1}); err != nil {
		t.Fatalf("write malformed record: %v", err)
	}
	if _, _, err := inventory.readSnapshot(); err == nil {
		t.Fatal("malformed inventory unexpectedly accepted")
	}
}

func TestParticipantInventorySnapshotIsIndependentAndCheckBlocksStalePut(t *testing.T) {
	inventory, engine := newParticipantTestInventory(t, 4)
	if err := inventory.enroll(context.Background(), "node-1", "inc-1"); err != nil {
		t.Fatalf("enroll node-1: %v", err)
	}
	members, check, err := inventory.readSnapshot()
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	members["node-1"] = "mutated"
	fresh, _, err := inventory.readSnapshot()
	if err != nil {
		t.Fatalf("read fresh snapshot: %v", err)
	}
	if fresh["node-1"] != "inc-1" {
		t.Fatalf("snapshot mutation leaked into inventory: %#v", fresh)
	}

	if err := inventory.enroll(context.Background(), "node-2", "inc-2"); err != nil {
		t.Fatalf("enroll node-2: %v", err)
	}
	committed, err := engine.Txn([]kvapi.TxnOp{
		check,
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: pendingKey("stale"), Value: []byte("pending")},
	})
	if err != nil {
		t.Fatalf("stale reservation txn: %v", err)
	}
	if committed {
		t.Fatal("stale inventory check unexpectedly committed reservation")
	}
	if _, err := engine.Get(pendingKey("stale")); !errors.Is(err, kvapi.ErrKeyNotFound) {
		t.Fatalf("stale reservation key error = %v", err)
	}
}

func TestNewParticipantInventoryRejectsInvalidConfiguration(t *testing.T) {
	bus := eventbus.NewBus()
	engine := systemkv.NewService("participants-config", bus, nil)
	if _, err := engine.Start(context.Background()); err != nil {
		t.Fatalf("start kv engine: %v", err)
	}
	defer func() {
		_ = engine.Stop(context.Background())
		bus.Stop()
	}()
	for _, max := range []int{0, -1} {
		if _, err := newParticipantInventory(engine, engine.Get, max); err == nil {
			t.Fatalf("maxParticipants=%d unexpectedly accepted", max)
		}
	}
}

func TestParticipantInventoryTransactionErrorIsNotRetried(t *testing.T) {
	bus := eventbus.NewBus()
	base := systemkv.NewService("participants-txn-error", bus, nil)
	if _, err := base.Start(context.Background()); err != nil {
		t.Fatalf("start kv engine: %v", err)
	}
	defer func() {
		_ = base.Stop(context.Background())
		bus.Stop()
	}()
	initial, err := encode(map[pid.NodeID]string{"node-1": "inc-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base.Set(participantsKey, initial); err != nil {
		t.Fatalf("seed inventory: %v", err)
	}

	uncertain := errors.New("transaction outcome uncertain")
	wrapped := &participantTxnErrorEngine{Engine: base, err: uncertain}
	inventory, err := newParticipantInventory(wrapped, wrapped.Get, 4)
	if err != nil {
		t.Fatal(err)
	}
	got := inventory.enroll(context.Background(), "node-2", "inc-2")
	if !errors.Is(got, uncertain) {
		t.Fatalf("enroll error = %v, want transaction error", got)
	}
	if wrapped.calls != 1 {
		t.Fatalf("Txn calls = %d, want 1", wrapped.calls)
	}
}

func TestParticipantInventoryCanceledDuringReadDoesNotWrite(t *testing.T) {
	bus := eventbus.NewBus()
	base := systemkv.NewService("participants-read-cancel", bus, nil)
	if _, err := base.Start(context.Background()); err != nil {
		t.Fatalf("start kv engine: %v", err)
	}
	defer func() {
		_ = base.Stop(context.Background())
		bus.Stop()
	}()

	started := make(chan struct{})
	release := make(chan struct{})
	value, err := encode(map[pid.NodeID]string{})
	if err != nil {
		t.Fatal(err)
	}
	read := func(key string) (kvapi.Entry, error) {
		close(started)
		<-release
		return kvapi.Entry{Key: key, Value: value, Version: 1}, nil
	}
	wrapped := &participantTxnCounter{Engine: base}
	inventory, err := newParticipantInventory(wrapped, read, 4)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- inventory.enroll(ctx, "node-1", "inc-1") }()
	<-started
	cancel()
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("enroll error = %v, want context.Canceled", err)
	}
	if wrapped.calls != 0 {
		t.Fatalf("Txn calls = %d, want 0 after cancellation during read", wrapped.calls)
	}
}

type participantTxnCounter struct {
	kvapi.Engine
	calls int
}

func (e *participantTxnCounter) Txn(ops []kvapi.TxnOp) (bool, error) {
	e.calls++
	return e.Engine.Txn(ops)
}

type participantTxnErrorEngine struct {
	kvapi.Engine
	err   error
	calls int
}

func (e *participantTxnErrorEngine) Txn(_ []kvapi.TxnOp) (bool, error) {
	e.calls++
	return false, e.err
}
