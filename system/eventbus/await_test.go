package eventbus

import (
	"context"
	"errors"
	"testing"
	"time"

	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/event"
)

func TestAwaiter_WaitFor_Accept(t *testing.T) {
	bus := NewBus()
	defer bus.Stop()

	ctx := context.Background()
	path := "test:function"

	// Start waiting in goroutine
	resultCh := make(chan AwaitResult, 1)
	go func() {
		result := Await(ctx, bus, "function", "function.(accept|reject)", path)
		resultCh <- result
	}()

	// Give awaiter time to subscribe
	time.Sleep(10 * time.Millisecond)

	// Send accept event
	bus.Send(ctx, event.Event{
		System: "function",
		Kind:   "function.accept",
		Path:   path,
	})

	select {
	case result := <-resultCh:
		if !result.Accepted {
			t.Error("expected accepted=true")
		}
		if result.Error != nil {
			t.Errorf("unexpected error: %v", result.Error)
		}
		if result.Event.Path != path {
			t.Errorf("expected path %s, got %s", path, result.Event.Path)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestAwaiter_WaitFor_Reject(t *testing.T) {
	bus := NewBus()
	defer bus.Stop()

	ctx := context.Background()
	path := "test:function"
	expectedErr := errors.New("validation failed")

	resultCh := make(chan AwaitResult, 1)
	go func() {
		result := Await(ctx, bus, "function", "function.(accept|reject)", path)
		resultCh <- result
	}()

	time.Sleep(10 * time.Millisecond)

	bus.Send(ctx, event.Event{
		System: "function",
		Kind:   "function.reject",
		Path:   path,
		Data:   expectedErr,
	})

	select {
	case result := <-resultCh:
		if result.Accepted {
			t.Error("expected accepted=false")
		}
		if result.Error == nil {
			t.Error("expected error from rejection")
		}
		if result.Error.Error() != expectedErr.Error() {
			t.Errorf("expected error %v, got %v", expectedErr, result.Error)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestAwaiter_WaitFor_Timeout(t *testing.T) {
	bus := NewBus()
	defer bus.Stop()

	ctx := context.Background()
	path := "test:function"

	result := AwaitWithTimeout(ctx, bus, "function", "function.(accept|reject)", path, 50*time.Millisecond)

	if result.Accepted {
		t.Error("expected accepted=false on timeout")
	}

	var apiErr apierror.Error
	if v, ok := result.Error.(apierror.Error); ok {
		apiErr = v
	}
	if apiErr == nil || apiErr.Kind() != apierror.Timeout {
		t.Errorf("expected apierror.Error with Timeout, got %T: %v", result.Error, result.Error)
	}
}

func TestAwaiter_WaitFor_IgnoresOtherPaths(t *testing.T) {
	bus := NewBus()
	defer bus.Stop()

	ctx := context.Background()
	targetPath := "test:target"
	otherPath := "test:other"

	resultCh := make(chan AwaitResult, 1)
	go func() {
		result := AwaitWithTimeout(ctx, bus, "function", "function.(accept|reject)", targetPath, 200*time.Millisecond)
		resultCh <- result
	}()

	time.Sleep(10 * time.Millisecond)

	// Send event for different path - should be ignored
	bus.Send(ctx, event.Event{
		System: "function",
		Kind:   "function.accept",
		Path:   otherPath,
	})

	// Send event for target path
	bus.Send(ctx, event.Event{
		System: "function",
		Kind:   "function.accept",
		Path:   targetPath,
	})

	select {
	case result := <-resultCh:
		if !result.Accepted {
			t.Error("expected accepted=true")
		}
		if result.Event.Path != targetPath {
			t.Errorf("expected path %s, got %s", targetPath, result.Event.Path)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestAwaiter_WaitFor_ContextCanceled(t *testing.T) {
	bus := NewBus()
	defer bus.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	path := "test:function"

	resultCh := make(chan AwaitResult, 1)
	go func() {
		result := Await(ctx, bus, "function", "function.(accept|reject)", path)
		resultCh <- result
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case result := <-resultCh:
		if result.Accepted {
			t.Error("expected accepted=false on cancel")
		}
		if !errors.Is(result.Error, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", result.Error)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestIsAcceptKind(t *testing.T) {
	tests := []struct {
		kind     event.Kind
		expected bool
	}{
		{"accept", true},
		{"function.accept", true},
		{"registry.accept", true},
		{"factory.accept", true},
		{"env.accepted", true},
		{"custom.accept", true},
		{"reject", false},
		{"function.reject", false},
		{"create", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			if got := isAcceptKind(tt.kind); got != tt.expected {
				t.Errorf("isAcceptKind(%q) = %v, want %v", tt.kind, got, tt.expected)
			}
		})
	}
}

func TestAwaiter_Prepare_Wait_NoRace(t *testing.T) {
	bus := NewBus()
	defer bus.Stop()

	ctx := context.Background()
	path := "test:function"

	awaiter := NewAwaiter(bus, "function", "function.(accept|reject)")

	// Prepare BEFORE sending - this is the correct pattern
	waiter, err := awaiter.Prepare(ctx, path)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	// Now send the event - waiter is already listening
	bus.Send(ctx, event.Event{
		System: "function",
		Kind:   "function.accept",
		Path:   path,
	})

	// Wait should receive the event
	result := waiter.Wait()
	if !result.Accepted {
		t.Errorf("expected accepted=true, got error: %v", result.Error)
	}
	if result.Event.Path != path {
		t.Errorf("expected path %s, got %s", path, result.Event.Path)
	}
}

func TestAwaiter_Prepare_Close_Explicit(t *testing.T) {
	bus := NewBus()
	defer bus.Stop()

	ctx := context.Background()
	path := "test:function"

	awaiter := NewAwaiter(bus, "function", "function.(accept|reject)")

	waiter, err := awaiter.Prepare(ctx, path)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	// Close without waiting - should not panic or leak
	waiter.Close()

	// Double close should be safe
	waiter.Close()
}
