package process_test

import (
	"context"
	"testing"

	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"

	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/runtime"
)

func TestOnCompleteAggregation(t *testing.T) {
	// Serve with a base context.
	ctx := context.Background()

	var sum int

	// Define first callback.
	cb1 := func(_ pubsub.PID, _ *runtime.Result) {
		sum++
	}

	// Attach the first onComplete callback.
	ctx = process.WithAddedOnComplete(ctx, cb1)

	// Define a second callback.
	cb2 := func(_ pubsub.PID, _ *runtime.Result) {
		sum += 2
	}

	// Aggregate the second callback.
	ctx = process.WithAddedOnComplete(ctx, cb2)

	// Retrieve the aggregated onComplete.
	onComplete := process.GetOnComplete(ctx)
	if onComplete == nil {
		t.Fatal("Expected aggregated onComplete callback, got nil")
	}

	// Create a dummy pid and runtime.Result.
	dummyPID := pubsub.PID{
		Host:   "test",
		ID:     registry.ID{Name: "dummy"}, // Use a simple string, since the type isn't crucial here.
		UniqID: "dummy",
	}
	dummyResult := &runtime.Result{}

	// Invoke the aggregated callback.
	onComplete(dummyPID, dummyResult)

	// Both callbacks should be executed: 1 + 2 = 3.
	if sum != 3 {
		t.Fatalf("Expected sum to be 3, got %d", sum)
	}
}

func TestOnStartAggregation(t *testing.T) {
	ctx := context.Background()
	sum := 0

	cb1 := func(_ pubsub.PID, _ process.Process) { sum++ }
	ctx = process.WithAddedOnStart(ctx, cb1)

	cb2 := func(_ pubsub.PID, _ process.Process) { sum += 2 }
	ctx = process.WithAddedOnStart(ctx, cb2)

	onStart := process.GetOnStart(ctx)
	if onStart == nil {
		t.Fatal("Expected aggregated onStart callback, got nil")
	}

	dummyPID := pubsub.PID{Host: "test", ID: registry.ID{Name: "dummy"}, UniqID: "dummy"}
	var dummyProc process.Process // No need to initialize, we just need the type.

	onStart(dummyPID, dummyProc)

	if sum != 3 {
		t.Fatalf("Expected sum to be 3, got %d", sum)
	}
}

func TestNoCallbacks(t *testing.T) {
	ctx := context.Background()

	onComplete := process.GetOnComplete(ctx)
	if onComplete != nil {
		t.Error("Expected nil OnComplete, got non-nil")
	}

	onStart := process.GetOnStart(ctx)
	if onStart != nil {
		t.Error("Expected nil OnStart, got non-nil")
	}
}

func TestSingleCallbacks(t *testing.T) {
	ctx := context.Background()
	var completeCalled, startCalled bool

	cb1 := func(_ pubsub.PID, _ *runtime.Result) { completeCalled = true }
	ctx = process.WithAddedOnComplete(ctx, cb1)

	cb2 := func(_ pubsub.PID, _ process.Process) { startCalled = true }
	ctx = process.WithAddedOnStart(ctx, cb2)

	onComplete := process.GetOnComplete(ctx)
	onStart := process.GetOnStart(ctx)

	dummyPID := pubsub.PID{Host: "test", ID: registry.ID{Name: "dummy"}, UniqID: "dummy"}
	dummyResult := &runtime.Result{}
	var dummyProc process.Process

	onComplete(dummyPID, dummyResult)
	onStart(dummyPID, dummyProc)

	if !completeCalled {
		t.Error("OnComplete callback was not called")
	}
	if !startCalled {
		t.Error("OnStart callback was not called")
	}
}
