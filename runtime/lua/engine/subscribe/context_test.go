package subscribe

import (
	"context"
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
)

func TestGetLayerContext(t *testing.T) {
	// Test with UnitOfWork without layer context
	parentCtx := context.Background()
	state := lua.NewState()
	defer state.Close()
	uw, _ := engine.NewUnitOfWork(parentCtx, state)
	defer uw.Close()

	if ctx := getLayerContext(uw); ctx != nil {
		t.Errorf("expected nil context for UnitOfWork without layer context")
	}

	// Test with UnitOfWork with layer context
	ensureLayerContext(uw)
	if ctx := getLayerContext(uw); ctx == nil {
		t.Errorf("expected non-nil context for UnitOfWork with layer context")
	}
}

func TestEnsureLayerContext(t *testing.T) {
	// Test with nil UnitOfWork
	if ctx := ensureLayerContext(nil); ctx != nil {
		t.Errorf("expected nil context for nil UnitOfWork")
	}

	// Test creating new layer context
	parentCtx := context.Background()
	state := lua.NewState()
	defer state.Close()
	uw, _ := engine.NewUnitOfWork(parentCtx, state)
	defer uw.Close()

	ctx := ensureLayerContext(uw)
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}

	if ctx.subs == nil {
		t.Error("expected non-nil subscription context")
	}

	if ctx.messageQueue == nil {
		t.Error("expected non-nil message queue")
	}

	// Test getting existing layer context
	ctx2 := ensureLayerContext(uw)
	if ctx2 != ctx {
		t.Error("expected same context instance")
	}
}

func TestPublish(t *testing.T) {
	parentCtx := context.Background()
	state := lua.NewState()
	defer state.Close()
	uw, ctx := engine.NewUnitOfWork(parentCtx, state)
	defer uw.Close()

	// Test publishing a message
	topic := "test_topic"
	values := []lua.LValue{lua.LString("value1"), lua.LNumber(42)}
	err := Publish(ctx, topic, values...)
	if err != nil {
		t.Errorf("expected no error from Publish, got %v", err)
	}

	// Process scheduled functions
	uw.Tasks().Wait(ctx, false)

	// Check that message was queued
	length, err := QueueLength(ctx)
	if err != nil {
		t.Errorf("expected no error from QueueLength, got %v", err)
	}
	if length != 1 {
		t.Errorf("expected queue length 1, got %d", length)
	}

	// Test publishing with cancelled context
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	err = Publish(cancelledCtx, topic, values...)
	if err == nil {
		t.Error("expected error from Publish with cancelled context")
	}

	// Test publishing without UnitOfWork
	err = Publish(context.Background(), topic, values...)
	if err == nil {
		t.Error("expected error from Publish without UnitOfWork")
	}
}

func TestRelease(t *testing.T) {
	parentCtx := context.Background()
	state := lua.NewState()
	defer state.Close()
	uw, ctx := engine.NewUnitOfWork(parentCtx, state)
	defer uw.Close()

	// Test releasing a subscription
	topic := "test_topic"
	err := Release(ctx, topic)
	if err != nil {
		t.Errorf("expected no error from Release, got %v", err)
	}

	// Process scheduled functions
	uw.Tasks().Wait(ctx, false)

	// Check that unsubscription was queued
	length, err := QueueLength(ctx)
	if err != nil {
		t.Errorf("expected no error from QueueLength, got %v", err)
	}
	if length != 1 {
		t.Errorf("expected queue length 1, got %d", length)
	}

	// Test releasing without UnitOfWork
	err = Release(context.Background(), topic)
	if err == nil {
		t.Error("expected error from Release without UnitOfWork")
	}
}

func TestSlots(t *testing.T) {
	parentCtx := context.Background()
	state := lua.NewState()
	defer state.Close()
	uw, ctx := engine.NewUnitOfWork(parentCtx, state)
	defer uw.Close()

	// Test slots for non-existent topic
	topic := "non_existent_topic"
	slots, err := Slots(ctx, topic)
	if err == nil {
		t.Error("expected error from Slots for non-existent topic")
	}
	if slots != 0 {
		t.Errorf("expected 0 slots, got %d", slots)
	}

	// Test slots without UnitOfWork
	slots, err = Slots(context.Background(), topic)
	if err == nil {
		t.Error("expected error from Slots without UnitOfWork")
	}
	if slots != 0 {
		t.Errorf("expected 0 slots, got %d", slots)
	}
}

func TestExists(t *testing.T) {
	parentCtx := context.Background()
	state := lua.NewState()
	defer state.Close()
	uw, ctx := engine.NewUnitOfWork(parentCtx, state)
	defer uw.Close()

	// Ensure layer context exists
	_ = ensureLayerContext(uw)

	// Test exists for non-existent topic
	topic := "non_existent_topic"
	exists, err := Exists(ctx, topic)
	if err != nil {
		t.Errorf("expected no error from Exists, got %v", err)
	}
	if exists {
		t.Error("expected topic to not exist")
	}

	// Test exists with cancelled context
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	exists, err = Exists(cancelledCtx, topic)
	if err == nil {
		t.Error("expected error from Exists with cancelled context")
	}

	// Test exists without UnitOfWork
	exists, err = Exists(context.Background(), topic)
	if err == nil {
		t.Error("expected error from Exists without UnitOfWork")
	}
}

func TestQueueLength(t *testing.T) {
	parentCtx := context.Background()
	state := lua.NewState()
	defer state.Close()
	uw, ctx := engine.NewUnitOfWork(parentCtx, state)
	defer uw.Close()

	// Ensure layer context exists
	_ = ensureLayerContext(uw)

	// Test initial queue length
	length, err := QueueLength(ctx)
	if err != nil {
		t.Errorf("expected no error from QueueLength, got %v", err)
	}
	if length != 0 {
		t.Errorf("expected initial queue length 0, got %d", length)
	}

	// Test queue length after publishing
	topic := "test_topic"
	values := []lua.LValue{lua.LString("value1")}
	err = Publish(ctx, topic, values...)
	if err != nil {
		t.Errorf("expected no error from Publish, got %v", err)
	}

	// Process scheduled functions
	uw.Tasks().Wait(ctx, false)

	length, err = QueueLength(ctx)
	if err != nil {
		t.Errorf("expected no error from QueueLength, got %v", err)
	}
	if length != 1 {
		t.Errorf("expected queue length 1, got %d", length)
	}

	// Test queue length without UnitOfWork
	length, err = QueueLength(context.Background())
	if err == nil {
		t.Error("expected error from QueueLength without UnitOfWork")
	}
	if length != 0 {
		t.Errorf("expected 0 length, got %d", length)
	}
}

func TestLayerContextOperations(t *testing.T) {
	parentCtx := context.Background()
	state := lua.NewState()
	defer state.Close()
	uw, ctx := engine.NewUnitOfWork(parentCtx, state)
	defer uw.Close()

	// Ensure layer context exists
	lCtx := ensureLayerContext(uw)
	if lCtx == nil {
		t.Fatal("failed to create layer context")
	}

	// Test subscription context
	if lCtx.subs == nil {
		t.Error("subscription context should not be nil")
	}

	// Test message queue
	if lCtx.messageQueue == nil {
		t.Error("message queue should not be nil")
	}

	// Test initial queue length
	if lCtx.messageQueue.Len() != 0 {
		t.Errorf("expected initial queue length 0, got %d", lCtx.messageQueue.Len())
	}

	// Test adding messages to queue
	topic := "test_topic"
	values := []lua.LValue{lua.LString("value1"), lua.LNumber(42)}
	err := Publish(ctx, topic, values...)
	if err != nil {
		t.Errorf("expected no error from Publish, got %v", err)
	}

	// Process scheduled functions
	uw.Tasks().Wait(ctx, false)

	if lCtx.messageQueue.Len() != 1 {
		t.Errorf("expected queue length 1, got %d", lCtx.messageQueue.Len())
	}

	// Test adding unsubscription to queue
	err = Release(ctx, topic)
	if err != nil {
		t.Errorf("expected no error from Release, got %v", err)
	}

	// Process scheduled functions
	uw.Tasks().Wait(ctx, false)

	if lCtx.messageQueue.Len() != 2 {
		t.Errorf("expected queue length 2, got %d", lCtx.messageQueue.Len())
	}
}
