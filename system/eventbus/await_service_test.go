// SPDX-License-Identifier: MPL-2.0

package eventbus

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/event"
)

func TestAwaitService_BasicAwait(t *testing.T) {
	bus := NewBus()
	defer bus.Stop()

	svc := NewAwaitService(bus)
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

func TestAwaitService_RejectEvent(t *testing.T) {
	bus := NewBus()
	defer bus.Stop()

	svc := NewAwaitService(bus)
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

func TestAwaitService_Timeout(t *testing.T) {
	bus := NewBus()
	defer bus.Stop()

	svc := NewAwaitService(bus)
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

func TestAwaitService_ConcurrentAwaiters(t *testing.T) {
	bus := NewBus()
	defer bus.Stop()

	svc := NewAwaitService(bus)
	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("failed to start service: %v", err)
	}
	defer func() { _ = svc.Stop() }()

	const count = 10
	var wg sync.WaitGroup
	results := make([]event.AwaitResult, count)

	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			path := "test/path/" + string(rune('a'+idx))
			results[idx] = svc.Await(ctx, "test", "factory.(accept|reject)", path, 5*time.Second)
		}(i)
	}

	time.Sleep(20 * time.Millisecond)

	for i := 0; i < count; i++ {
		path := "test/path/" + string(rune('a'+i))
		bus.Send(ctx, event.Event{
			System: "test",
			Kind:   "factory.accept",
			Path:   path,
		})
	}

	wg.Wait()

	for i, result := range results {
		if !result.Accepted {
			t.Errorf("awaiter %d: expected accepted, got rejected", i)
		}
		if result.Error != nil {
			t.Errorf("awaiter %d: unexpected error: %v", i, result.Error)
		}
	}
}

func TestAwaitService_PathRouting(t *testing.T) {
	bus := NewBus()
	defer bus.Stop()

	svc := NewAwaitService(bus)
	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("failed to start service: %v", err)
	}
	defer func() { _ = svc.Stop() }()

	result1Ch := make(chan event.AwaitResult, 1)
	result2Ch := make(chan event.AwaitResult, 1)

	go func() {
		result1Ch <- svc.Await(ctx, "test", "factory.(accept|reject)", "path/one", 5*time.Second)
	}()
	go func() {
		result2Ch <- svc.Await(ctx, "test", "factory.(accept|reject)", "path/two", 5*time.Second)
	}()

	time.Sleep(20 * time.Millisecond)

	bus.Send(ctx, event.Event{
		System: "test",
		Kind:   "factory.accept",
		Path:   "path/two",
	})

	select {
	case <-result1Ch:
		t.Error("path/one should not have received event yet")
	case <-time.After(50 * time.Millisecond):
	}

	select {
	case result := <-result2Ch:
		if !result.Accepted {
			t.Error("path/two should be accepted")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("path/two should have received event")
	}

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

func TestAwaitService_ContextCancellation(t *testing.T) {
	bus := NewBus()
	defer bus.Stop()

	svc := NewAwaitService(bus)
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

func TestAwaitService_PrepareThenSend(t *testing.T) {
	bus := NewBus()
	defer bus.Stop()

	svc := NewAwaitService(bus)
	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("failed to start service: %v", err)
	}
	defer func() { _ = svc.Stop() }()

	waiter, err := svc.Prepare(ctx, "test", "factory.(accept|reject)", "path/prepared", time.Second)
	if err != nil {
		t.Fatalf("prepare failed: %v", err)
	}

	bus.Send(ctx, event.Event{
		System: "test",
		Kind:   "factory.accept",
		Path:   "path/prepared",
	})

	result := waiter.Wait()
	if !result.Accepted {
		t.Fatalf("expected accepted result, got %#v", result)
	}
	if result.Error != nil {
		t.Fatalf("expected no error, got %v", result.Error)
	}
}

func TestAwaitService_PrepareAllowsReplyBeforeWait(t *testing.T) {
	bus := NewBus()
	defer bus.Stop()

	svc := NewAwaitService(bus)
	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("failed to start service: %v", err)
	}
	defer func() { _ = svc.Stop() }()

	waiter, err := svc.Prepare(ctx, "test", "factory.(accept|reject)", "path/early", time.Second)
	if err != nil {
		t.Fatalf("prepare failed: %v", err)
	}

	bus.Send(ctx, event.Event{
		System: "test",
		Kind:   "factory.accept",
		Path:   "path/early",
	})

	time.Sleep(10 * time.Millisecond)

	result := waiter.Wait()
	if !result.Accepted {
		t.Fatalf("expected accepted result, got %#v", result)
	}
}

func TestAwaitService_PrepareCloseReleasesPath(t *testing.T) {
	bus := NewBus()
	defer bus.Stop()

	svc := NewAwaitService(bus)
	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("failed to start service: %v", err)
	}
	defer func() { _ = svc.Stop() }()

	waiter1, err := svc.Prepare(ctx, "test", "factory.(accept|reject)", "path/reuse", time.Second)
	if err != nil {
		t.Fatalf("first prepare failed: %v", err)
	}
	waiter1.Close()

	waiter2, err := svc.Prepare(ctx, "test", "factory.(accept|reject)", "path/reuse", time.Second)
	if err != nil {
		t.Fatalf("second prepare failed: %v", err)
	}

	bus.Send(ctx, event.Event{
		System: "test",
		Kind:   "factory.accept",
		Path:   "path/reuse",
	})

	result := waiter2.Wait()
	if !result.Accepted {
		t.Fatalf("expected accepted result on reused path, got %#v", result)
	}
}
