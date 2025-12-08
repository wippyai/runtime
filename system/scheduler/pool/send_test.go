package pool

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
)

// idleProcess goes into StepIdle state and waits for a message before completing.
// Messages are received via events with EventMessage type.
type idleProcess struct {
	received  []*relay.Package
	mu        sync.Mutex
	stepCount int
}

func (p *idleProcess) Init(ctx context.Context, method string, input payload.Payloads) error {
	return nil
}

func (p *idleProcess) Step(events []process.Event, out *process.StepOutput) error {
	p.mu.Lock()
	p.stepCount++
	count := p.stepCount
	p.mu.Unlock()

	// First step: go idle and wait for message
	if count == 1 {
		out.Idle()
		return nil
	}

	// Collect messages from events
	for _, ev := range events {
		if ev.Type == process.EventMessage {
			if pkg, ok := ev.Data.(*relay.Package); ok {
				p.mu.Lock()
				p.received = append(p.received, pkg)
				p.mu.Unlock()
			}
		}
	}

	// After receiving message, complete
	out.Done(nil)
	return nil
}

func (p *idleProcess) Close() {}

func (p *idleProcess) getReceived() []*relay.Package {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make([]*relay.Package, len(p.received))
	copy(result, p.received)
	return result
}

// multiIdleProcess goes idle N times before completing.
// Messages are received via events with EventMessage type.
type multiIdleProcess struct {
	idleTimes int
	received  []*relay.Package
	mu        sync.Mutex
	stepCount int
}

func (p *multiIdleProcess) Init(ctx context.Context, method string, input payload.Payloads) error {
	return nil
}

func (p *multiIdleProcess) Step(events []process.Event, out *process.StepOutput) error {
	p.mu.Lock()
	p.stepCount++
	count := p.stepCount
	idleTimes := p.idleTimes
	p.mu.Unlock()

	// On even steps (after waking from idle), collect messages from events
	if count%2 == 0 {
		for _, ev := range events {
			if ev.Type == process.EventMessage {
				if pkg, ok := ev.Data.(*relay.Package); ok {
					p.mu.Lock()
					p.received = append(p.received, pkg)
					p.mu.Unlock()
				}
			}
		}
	}

	// Alternate between idle and continue until we've received enough messages
	if count <= idleTimes*2 && count%2 == 1 {
		out.Idle()
		return nil
	}
	if count > idleTimes*2 {
		out.Done(nil)
		return nil
	}
	// Even step: continue after receiving message
	out.Continue()
	return nil
}

func (p *multiIdleProcess) Close() {}

func (p *multiIdleProcess) getReceived() []*relay.Package {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make([]*relay.Package, len(p.received))
	copy(result, p.received)
	return result
}

// Send tests for Inline pool

func TestInlineSend(t *testing.T) {
	var proc *idleProcess
	factory := func() (process.Process, error) {
		proc = &idleProcess{}
		return proc, nil
	}

	pool, err := NewInline(factory, &mockDispatcher{})
	if err != nil {
		t.Fatalf("NewInline: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	// Run Call in a goroutine since it will block on StepIdle
	resultCh := make(chan error, 1)
	go func() {
		_, err := pool.Call(testContext(), "test", nil)
		resultCh <- err
	}()

	// Wait for process to enter idle state
	time.Sleep(10 * time.Millisecond)

	// Send message to the pool using the known test PID
	pkg := &relay.Package{Target: relay.PID{UniqID: "test-pid"}}
	err = pool.Send(pkg)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Wait for Call to complete
	select {
	case err := <-resultCh:
		if err != nil {
			t.Fatalf("Call: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for Call to complete")
	}

	// Verify message was received
	received := proc.getReceived()
	if len(received) != 1 {
		t.Fatalf("expected 1 message, got %d", len(received))
	}
}

func TestInlineSendToNonExistent(t *testing.T) {
	pool, err := NewInline(newMockFactory(0), &mockDispatcher{})
	if err != nil {
		t.Fatalf("NewInline: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	// Send to non-existent PID should return error
	pkg := &relay.Package{Target: relay.PID{UniqID: "nonexistent"}}
	err = pool.Send(pkg)
	if err != process.ErrProcessNotFound {
		t.Fatalf("expected ErrProcessNotFound, got %v", err)
	}
}

// Send tests for Static pool

func TestStaticSend(t *testing.T) {
	procCh := make(chan *idleProcess, 1)
	factory := func() (process.Process, error) {
		p := &idleProcess{}
		select {
		case procCh <- p:
		default:
		}
		return p, nil
	}

	pool, err := NewStatic(factory, &mockDispatcher{}, Config{Workers: 1})
	if err != nil {
		t.Fatalf("NewStatic: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	// Run Call in a goroutine since it will block on StepIdle
	resultCh := make(chan error, 1)
	go func() {
		_, err := pool.Call(testContext(), "test", nil)
		resultCh <- err
	}()

	// Wait for process to enter idle state
	time.Sleep(20 * time.Millisecond)

	// Send message to the pool using the known test PID
	pkg := &relay.Package{Target: relay.PID{UniqID: "test-pid"}}
	err = pool.Send(pkg)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Wait for Call to complete
	select {
	case err := <-resultCh:
		if err != nil {
			t.Fatalf("Call: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for Call to complete")
	}

	// Get the process and verify message was received
	proc := <-procCh
	received := proc.getReceived()
	if len(received) != 1 {
		t.Fatalf("expected 1 message, got %d", len(received))
	}
}

func TestStaticSendToNonExistent(t *testing.T) {
	pool, err := NewStatic(newMockFactory(0), &mockDispatcher{}, Config{Workers: 1})
	if err != nil {
		t.Fatalf("NewStatic: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	pkg := &relay.Package{Target: relay.PID{UniqID: "nonexistent"}}
	err = pool.Send(pkg)
	if err != process.ErrProcessNotFound {
		t.Fatalf("expected ErrProcessNotFound, got %v", err)
	}
}

func TestStaticSendMultipleWorkers(t *testing.T) {
	var mu sync.Mutex
	procs := make([]*idleProcess, 0, 4)
	factory := func() (process.Process, error) {
		p := &idleProcess{}
		mu.Lock()
		procs = append(procs, p)
		mu.Unlock()
		return p, nil
	}

	pool, err := NewStatic(factory, &mockDispatcher{}, Config{Workers: 4})
	if err != nil {
		t.Fatalf("NewStatic: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	// Start 4 concurrent calls with unique PIDs
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		pid := fmt.Sprintf("pid-%d", i+1)
		go func(p string) {
			defer wg.Done()
			pool.Call(testContextWithPID(p), "test", nil)
		}(pid)
	}

	// Wait for all processes to enter idle state
	time.Sleep(50 * time.Millisecond)

	// Send messages to each active execution
	for i := 1; i <= 4; i++ {
		pkg := &relay.Package{Target: relay.PID{UniqID: fmt.Sprintf("pid-%d", i)}}
		err = pool.Send(pkg)
		if err != nil {
			t.Fatalf("Send to %d: %v", i, err)
		}
	}

	wg.Wait()

	// Verify all processes received exactly one message
	mu.Lock()
	totalReceived := 0
	for _, p := range procs {
		totalReceived += len(p.getReceived())
	}
	mu.Unlock()

	if totalReceived != 4 {
		t.Fatalf("expected 4 total messages, got %d", totalReceived)
	}
}

// Send tests for Lazy pool

func TestLazySend(t *testing.T) {
	var proc *idleProcess
	var mu sync.Mutex
	factory := func() (process.Process, error) {
		mu.Lock()
		defer mu.Unlock()
		proc = &idleProcess{}
		return proc, nil
	}

	pool, err := NewLazy(factory, &mockDispatcher{}, LazyConfig{MaxWorkers: 4})
	if err != nil {
		t.Fatalf("NewLazy: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	// Run Call in a goroutine since it will block on StepIdle
	resultCh := make(chan error, 1)
	go func() {
		_, err := pool.Call(testContext(), "test", nil)
		resultCh <- err
	}()

	// Wait for process to enter idle state
	time.Sleep(20 * time.Millisecond)

	// Send message to the pool using the known test PID
	pkg := &relay.Package{Target: relay.PID{UniqID: "test-pid"}}
	err = pool.Send(pkg)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Wait for Call to complete
	select {
	case err := <-resultCh:
		if err != nil {
			t.Fatalf("Call: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for Call to complete")
	}

	// Verify message was received
	mu.Lock()
	received := proc.getReceived()
	mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 message, got %d", len(received))
	}
}

func TestLazySendToNonExistent(t *testing.T) {
	pool, err := NewLazy(newMockFactory(0), &mockDispatcher{}, LazyConfig{MaxWorkers: 4})
	if err != nil {
		t.Fatalf("NewLazy: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	pkg := &relay.Package{Target: relay.PID{UniqID: "nonexistent"}}
	err = pool.Send(pkg)
	if err != process.ErrProcessNotFound {
		t.Fatalf("expected ErrProcessNotFound, got %v", err)
	}
}

func TestLazySendAfterCompletion(t *testing.T) {
	pool, err := NewLazy(newMockFactory(0), &mockDispatcher{}, LazyConfig{MaxWorkers: 4})
	if err != nil {
		t.Fatalf("NewLazy: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	// Complete a call
	_, err = pool.Call(testContext(), "test", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	// Send to completed execution should return ErrProcessNotFound
	pkg := &relay.Package{Target: relay.PID{UniqID: "1"}}
	err = pool.Send(pkg)
	if err != process.ErrProcessNotFound {
		t.Fatalf("expected ErrProcessNotFound, got %v", err)
	}
}

// Stress tests for Send

func TestSendStressInline(t *testing.T) {
	const numIterations = 100

	for i := 0; i < numIterations; i++ {
		var proc *idleProcess
		factory := func() (process.Process, error) {
			proc = &idleProcess{}
			return proc, nil
		}

		pool, err := NewInline(factory, &mockDispatcher{})
		if err != nil {
			t.Fatalf("iteration %d: NewInline: %v", i, err)
		}

		pid := fmt.Sprintf("pid-%d", i)
		done := make(chan struct{})
		go func(p string) {
			pool.Call(testContextWithPID(p), "test", nil)
			close(done)
		}(pid)

		time.Sleep(time.Millisecond)
		pkg := &relay.Package{Target: relay.PID{UniqID: pid}}
		_ = pool.Send(pkg)

		select {
		case <-done:
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("iteration %d: timeout", i)
		}

		pool.Stop()
	}
}

func TestSendStressStatic(t *testing.T) {
	const numWorkers = 4
	const numCalls = 100

	factory := func() (process.Process, error) {
		return &idleProcess{}, nil
	}

	pool, err := NewStatic(factory, &mockDispatcher{}, Config{Workers: numWorkers})
	if err != nil {
		t.Fatalf("NewStatic: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	var wg sync.WaitGroup
	var nextPID atomic.Uint64

	for i := 0; i < numCalls; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			pid := fmt.Sprintf("pid-%d", nextPID.Add(1))
			doneCh := make(chan struct{})
			go func(p string) {
				pool.Call(testContextWithPID(p), "test", nil)
				close(doneCh)
			}(pid)

			time.Sleep(time.Millisecond)
			pkg := &relay.Package{Target: relay.PID{UniqID: pid}}
			_ = pool.Send(pkg)

			select {
			case <-doneCh:
			case <-time.After(time.Second):
				t.Error("timeout waiting for call")
			}
		}()
	}

	wg.Wait()
}

func TestSendStressLazy(t *testing.T) {
	const numCalls = 100

	factory := func() (process.Process, error) {
		return &idleProcess{}, nil
	}

	pool, err := NewLazy(factory, &mockDispatcher{}, LazyConfig{MaxWorkers: 8})
	if err != nil {
		t.Fatalf("NewLazy: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	var wg sync.WaitGroup
	var nextPID atomic.Uint64

	for i := 0; i < numCalls; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			pid := fmt.Sprintf("pid-%d", nextPID.Add(1))
			doneCh := make(chan struct{})
			go func(p string) {
				pool.Call(testContextWithPID(p), "test", nil)
				close(doneCh)
			}(pid)

			time.Sleep(5 * time.Millisecond)
			pkg := &relay.Package{Target: relay.PID{UniqID: pid}}
			_ = pool.Send(pkg)

			select {
			case <-doneCh:
			case <-time.After(time.Second):
				t.Error("timeout waiting for call")
			}
		}()
	}

	wg.Wait()
}
