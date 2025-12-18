package await

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/system/eventbus"
)

func TestService_BasicAwait(t *testing.T) {
	bus := eventbus.NewBus()
	defer bus.Stop()

	svc := NewService(bus)
	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("failed to start service: %v", err)
	}
	defer func() { _ = svc.Stop() }()

	// Start awaiting in goroutine
	resultCh := make(chan event.AwaitResult, 1)
	go func() {
		resultCh <- svc.Await(ctx, "test", "response.(accept|reject)", "test/path", 5*time.Second)
	}()

	// Give time for subscription to be created
	time.Sleep(10 * time.Millisecond)

	// Send accept event
	bus.Send(ctx, event.Event{
		System: "test",
		Kind:   "response.accept",
		Path:   "test/path",
	})

	select {
	case result := <-resultCh:
		if !result.Accepted {
			t.Error("expected accepted result")
		}
		if result.Error != nil {
			t.Errorf("unexpected error: %v", result.Error)
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for result")
	}
}

func TestService_RejectEvent(t *testing.T) {
	bus := eventbus.NewBus()
	defer bus.Stop()

	svc := NewService(bus)
	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("failed to start service: %v", err)
	}
	defer func() { _ = svc.Stop() }()

	resultCh := make(chan event.AwaitResult, 1)
	go func() {
		resultCh <- svc.Await(ctx, "test", "response.(accept|reject)", "test/path", 5*time.Second)
	}()

	time.Sleep(10 * time.Millisecond)

	bus.Send(ctx, event.Event{
		System: "test",
		Kind:   "response.reject",
		Path:   "test/path",
	})

	select {
	case result := <-resultCh:
		if result.Accepted {
			t.Error("expected rejected result")
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for result")
	}
}

func TestService_Timeout(t *testing.T) {
	bus := eventbus.NewBus()
	defer bus.Stop()

	svc := NewService(bus)
	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("failed to start service: %v", err)
	}
	defer func() { _ = svc.Stop() }()

	result := svc.Await(ctx, "test", "response.(accept|reject)", "test/path", 50*time.Millisecond)
	if result.Error == nil {
		t.Error("expected timeout error")
	}
}

func TestService_ConcurrentAwaiters(t *testing.T) {
	bus := eventbus.NewBus()
	defer bus.Stop()

	svc := NewService(bus)
	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("failed to start service: %v", err)
	}
	defer func() { _ = svc.Stop() }()

	const count = 10
	var wg sync.WaitGroup
	results := make([]event.AwaitResult, count)

	// Start multiple concurrent awaiters
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			path := "test/path/" + string(rune('a'+idx))
			results[idx] = svc.Await(ctx, "test", "factory.(accept|reject)", path, 5*time.Second)
		}(i)
	}

	// Give time for subscriptions
	time.Sleep(20 * time.Millisecond)

	// Send accept events for all paths
	for i := 0; i < count; i++ {
		path := "test/path/" + string(rune('a'+i))
		bus.Send(ctx, event.Event{
			System: "test",
			Kind:   "factory.accept",
			Path:   path,
		})
	}

	wg.Wait()

	// Verify all succeeded
	for i, result := range results {
		if !result.Accepted {
			t.Errorf("awaiter %d: expected accepted, got rejected", i)
		}
		if result.Error != nil {
			t.Errorf("awaiter %d: unexpected error: %v", i, result.Error)
		}
	}
}

func TestService_PathRouting(t *testing.T) {
	bus := eventbus.NewBus()
	defer bus.Stop()

	svc := NewService(bus)
	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("failed to start service: %v", err)
	}
	defer func() { _ = svc.Stop() }()

	// Two awaiters for different paths
	result1Ch := make(chan event.AwaitResult, 1)
	result2Ch := make(chan event.AwaitResult, 1)

	go func() {
		result1Ch <- svc.Await(ctx, "test", "factory.(accept|reject)", "path/one", 5*time.Second)
	}()
	go func() {
		result2Ch <- svc.Await(ctx, "test", "factory.(accept|reject)", "path/two", 5*time.Second)
	}()

	time.Sleep(20 * time.Millisecond)

	// Send event for path/two first
	bus.Send(ctx, event.Event{
		System: "test",
		Kind:   "factory.accept",
		Path:   "path/two",
	})

	// path/one should not receive it
	select {
	case <-result1Ch:
		t.Error("path/one should not have received event yet")
	case <-time.After(50 * time.Millisecond):
		// expected
	}

	// Verify path/two received it
	select {
	case result := <-result2Ch:
		if !result.Accepted {
			t.Error("path/two should be accepted")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("path/two should have received event")
	}

	// Now send for path/one
	bus.Send(ctx, event.Event{
		System: "test",
		Kind:   "factory.accept",
		Path:   "path/one",
	})

	select {
	case result := <-result1Ch:
		if !result.Accepted {
			t.Error("path/one should be accepted")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("path/one should have received event")
	}
}

func TestService_ContextCancellation(t *testing.T) {
	bus := eventbus.NewBus()
	defer bus.Stop()

	svc := NewService(bus)
	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("failed to start service: %v", err)
	}
	defer func() { _ = svc.Stop() }()

	cancelCtx, cancel := context.WithCancel(ctx)

	resultCh := make(chan event.AwaitResult, 1)
	go func() {
		resultCh <- svc.Await(cancelCtx, "test", "factory.(accept|reject)", "test/path", 5*time.Second)
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case result := <-resultCh:
		if !errors.Is(result.Error, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", result.Error)
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for cancellation")
	}
}
