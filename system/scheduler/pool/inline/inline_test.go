package inline

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
)

type mockProcess struct {
	mu      sync.Mutex
	latency time.Duration
}

func (p *mockProcess) Init(_ context.Context, _ string, _ payload.Payloads) error {
	return nil
}

func (p *mockProcess) Step(_ []process.Event, out *process.StepOutput) error {
	p.mu.Lock()
	latency := p.latency
	p.mu.Unlock()

	if latency > 0 {
		time.Sleep(latency)
	}

	out.Done(nil)
	return nil
}

func (p *mockProcess) Close() {}

type mockDispatcher struct{}

func (d *mockDispatcher) Dispatch(dispatcher.Command) dispatcher.Handler { return nil }

func newMockFactory(latency time.Duration) process.FactoryFunc {
	return func() (process.Process, error) {
		return &mockProcess{latency: latency}, nil
	}
}

func newErrorFactory() process.FactoryFunc {
	return func() (process.Process, error) {
		return nil, errors.New("factory error")
	}
}

func TestInlineBasic(t *testing.T) {
	p, err := New(newMockFactory(0), &mockDispatcher{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Stop()

	p.Start()

	result, err := p.Call(context.Background(), "test", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("result error: %v", result.Error)
	}
}

func TestInlineMultipleCalls(t *testing.T) {
	p, err := New(newMockFactory(0), &mockDispatcher{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Stop()
	p.Start()

	for i := 0; i < 100; i++ {
		_, err := p.Call(context.Background(), "test", nil)
		if err != nil {
			t.Fatalf("Call %d: %v", i, err)
		}
	}
}

func TestInlineFactoryError(t *testing.T) {
	_, err := New(newErrorFactory(), &mockDispatcher{})
	if err == nil {
		t.Fatal("expected factory error")
	}
}

func TestInlineStop(t *testing.T) {
	p, err := New(newMockFactory(0), &mockDispatcher{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p.Start()

	// Call before stop should work
	_, err = p.Call(context.Background(), "test", nil)
	if err != nil {
		t.Fatalf("Call before stop: %v", err)
	}

	// Stop the pool
	p.Stop()

	// Call after stop should fail
	_, err = p.Call(context.Background(), "test", nil)
	if err == nil {
		t.Fatal("expected error after stop")
	}
}
