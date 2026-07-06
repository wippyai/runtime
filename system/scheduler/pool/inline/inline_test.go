// SPDX-License-Identifier: MPL-2.0

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
	closeFn func()
	stepErr error
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
	return p.stepErr
}

func (p *mockProcess) Close() {
	if p.closeFn != nil {
		p.closeFn()
	}
}

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

func TestInlineReplacesRequestingProcess(t *testing.T) {
	created := 0
	closed := 0
	var events []string
	factory := func() (process.Process, error) {
		created++
		n := created
		switch n {
		case 1:
			events = append(events, "create1")
		case 2:
			events = append(events, "create2")
		}
		return &mockProcess{
			stepErr: process.ErrProcessReplacementRequested,
			closeFn: func() {
				closed++
				if n == 1 {
					events = append(events, "close1")
				}
			},
		}, nil
	}
	p, err := New(factory, &mockDispatcher{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Stop()

	result, err := p.Call(context.Background(), "test", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("result error: %v", result.Error)
	}
	if created != 2 {
		t.Fatalf("created = %d, want 2", created)
	}
	if closed != 1 {
		t.Fatalf("closed = %d, want 1", closed)
	}
	if len(events) < 3 || events[0] != "create1" || events[1] != "close1" || events[2] != "create2" {
		t.Fatalf("events = %v, want create1 close1 create2", events)
	}
}

func TestInlineReplacementFactoryErrorIsRecoverable(t *testing.T) {
	wantErr := errors.New("replacement failed")
	calls := 0
	factory := func() (process.Process, error) {
		calls++
		switch calls {
		case 1:
			return &mockProcess{stepErr: process.ErrProcessReplacementRequested}, nil
		case 2, 3:
			return nil, wantErr
		default:
			return &mockProcess{}, nil
		}
	}
	p, err := New(factory, &mockDispatcher{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Stop()

	result, err := p.Call(context.Background(), "test", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("result error: %v", result.Error)
	}

	result, err = p.Call(context.Background(), "test", nil)
	if err != nil {
		t.Fatalf("Call after failed replacement: %v", err)
	}
	if !errors.Is(result.Error, wantErr) {
		t.Fatalf("result error = %v, want %v", result.Error, wantErr)
	}

	result, err = p.Call(context.Background(), "test", nil)
	if err != nil {
		t.Fatalf("Call after recovery: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("recovery result error: %v", result.Error)
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
