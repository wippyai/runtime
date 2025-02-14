package process_test

import (
	"context"
	"github.com/ponyruntime/pony/api/registry"
	"testing"

	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/runtime"
)

func TestOnCompleteAggregation(t *testing.T) {
	// Start with a base context.
	ctx := context.Background()

	var sum int

	// Define first callback.
	cb1 := func(pid process.PID, result *runtime.Result) {
		sum += 1
	}

	// Attach the first onComplete callback.
	ctx = process.WithOnComplete(ctx, cb1)

	// Define a second callback.
	cb2 := func(pid process.PID, result *runtime.Result) {
		sum += 2
	}

	// Aggregate the second callback.
	ctx = process.WithOnComplete(ctx, cb2)

	// Retrieve the aggregated onComplete.
	onComplete := process.GetOnComplete(ctx)
	if onComplete == nil {
		t.Fatal("Expected aggregated onComplete callback, got nil")
	}

	// Create a dummy PID and runtime.Result.
	dummyPID := process.PID{
		Host: "test",
		ID:   registry.ID{Name: "dummy"}, // Use a simple string, since the type isn't crucial here.
		Name: "dummy",
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

	cb1 := func(pid process.PID, proc process.Process) { sum += 1 }
	ctx = process.WithOnStart(ctx, cb1)

	cb2 := func(pid process.PID, proc process.Process) { sum += 2 }
	ctx = process.WithOnStart(ctx, cb2)

	onStart := process.GetOnStart(ctx)
	if onStart == nil {
		t.Fatal("Expected aggregated onStart callback, got nil")
	}

	dummyPID := process.PID{Host: "test", ID: registry.ID{Name: "dummy"}, Name: "dummy"}
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

	cb1 := func(pid process.PID, result *runtime.Result) { completeCalled = true }
	ctx = process.WithOnComplete(ctx, cb1)

	cb2 := func(pid process.PID, proc process.Process) { startCalled = true }
	ctx = process.WithOnStart(ctx, cb2)

	onComplete := process.GetOnComplete(ctx)
	onStart := process.GetOnStart(ctx)

	dummyPID := process.PID{Host: "test", ID: registry.ID{Name: "dummy"}, Name: "dummy"}
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
