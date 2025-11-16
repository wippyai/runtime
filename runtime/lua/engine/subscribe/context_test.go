package subscribe

import (
	"context"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
)

func TestGetLayerContext(t *testing.T) {
	// Test with UnitOfWork without layer context
	parentCtx := newTestContext()
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
	parentCtx := newTestContext()
	state := lua.NewState()
	defer state.Close()
	uw, _ := engine.NewUnitOfWork(parentCtx, state)
	defer uw.Close()

	ctx := ensureLayerContext(uw)
	require.NotNil(t, ctx, "expected non-nil context")
	require.NotNil(t, ctx.subs, "expected non-nil subscription context")
	require.NotNil(t, ctx.messageQueue, "expected non-nil message queue")

	// Test getting existing layer context
	ctx2 := ensureLayerContext(uw)
	if ctx2 != ctx {
		t.Error("expected same context instance")
	}
}

func TestPublish(t *testing.T) {
	parentCtx := newTestContext()
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
	_, err = uw.Tasks().Wait(ctx, false)
	require.NoError(t, err)

	// Check that message was queued
	length, err := QueueLength(ctx)
	if err != nil {
		t.Errorf("expected no error from QueueLength, got %v", err)
	}
	if length != 1 {
		t.Errorf("expected queue length 1, got %d", length)
	}

	// Test publishing with canceled context
	cancelledCtx, cancel := context.WithCancel(ctxapi.NewRootContext())
	cancel()
	err = Publish(cancelledCtx, topic, values...)
	if err == nil {
		t.Error("expected error from Publish with canceled context")
	}

	// Test publishing without UnitOfWork
	err = Publish(newTestContext(), topic, values...)
	if err == nil {
		t.Error("expected error from Publish without UnitOfWork")
	}
}

func TestRelease(t *testing.T) {
	parentCtx := newTestContext()
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
	_, err = uw.Tasks().Wait(ctx, false)
	require.NoError(t, err)

	// Check that unsubscription was queued
	length, err := QueueLength(ctx)
	if err != nil {
		t.Errorf("expected no error from QueueLength, got %v", err)
	}
	if length != 1 {
		t.Errorf("expected queue length 1, got %d", length)
	}

	// Test releasing without UnitOfWork
	err = Release(newTestContext(), topic)
	if err == nil {
		t.Error("expected error from Release without UnitOfWork")
	}
}

func TestSlots(t *testing.T) {
	parentCtx := newTestContext()
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
	slots, err = Slots(newTestContext(), topic)
	if err == nil {
		t.Error("expected error from Slots without UnitOfWork")
	}
	if slots != 0 {
		t.Errorf("expected 0 slots, got %d", slots)
	}
}

func TestExists(t *testing.T) {
	parentCtx := newTestContext()
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

	// Test exists with canceled context
	cancelledCtx, cancel := context.WithCancel(ctxapi.NewRootContext())
	cancel()
	_, err = Exists(cancelledCtx, topic)
	require.Error(t, err)

	// Test exists without UnitOfWork
	_, err = Exists(newTestContext(), topic)
	require.Error(t, err)
}

func TestQueueLength(t *testing.T) {
	parentCtx := newTestContext()
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
	_, err = uw.Tasks().Wait(ctx, false)
	require.NoError(t, err)

	length, err = QueueLength(ctx)
	if err != nil {
		t.Errorf("expected no error from QueueLength, got %v", err)
	}
	if length != 1 {
		t.Errorf("expected queue length 1, got %d", length)
	}

	// Test queue length without UnitOfWork
	length, err = QueueLength(newTestContext())
	if err == nil {
		t.Error("expected error from QueueLength without UnitOfWork")
	}
	if length != 0 {
		t.Errorf("expected 0 length, got %d", length)
	}
}

func TestLayerContextOperations(t *testing.T) {
	parentCtx := newTestContext()
	state := lua.NewState()
	defer state.Close()
	uw, ctx := engine.NewUnitOfWork(parentCtx, state)
	defer uw.Close()

	// Ensure layer context exists
	lCtx := ensureLayerContext(uw)
	require.NotNil(t, lCtx, "failed to create layer context")
	require.NotNil(t, lCtx.subs, "subscription context should not be nil")
	require.NotNil(t, lCtx.messageQueue, "message queue should not be nil")

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
	_, err = uw.Tasks().Wait(ctx, false)
	require.NoError(t, err)

	if lCtx.messageQueue.Len() != 1 {
		t.Errorf("expected queue length 1, got %d", lCtx.messageQueue.Len())
	}

	// Test adding unsubscription to queue
	err = Release(ctx, topic)
	if err != nil {
		t.Errorf("expected no error from Release, got %v", err)
	}

	// Process scheduled functions
	_, err = uw.Tasks().Wait(ctx, false)
	require.NoError(t, err)

	if lCtx.messageQueue.Len() != 2 {
		t.Errorf("expected queue length 2, got %d", lCtx.messageQueue.Len())
	}
}
