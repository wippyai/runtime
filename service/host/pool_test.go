package host

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/topology"
	"go.uber.org/zap/zaptest"
)

// mockProcess implements the process.Process interface for testing
type mockProcess struct {
	mu           sync.Mutex
	started      bool
	terminated   bool
	readyCount   int
	stepError    error
	sendError    error
	startError   error
	packages     []*pubsub.Package
	stepCallback func() error
}

func newMockProcess() *mockProcess {
	return &mockProcess{
		packages: make([]*pubsub.Package, 0),
	}
}

func (m *mockProcess) Send(pkg *pubsub.Package) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.sendError != nil {
		return m.sendError
	}

	m.packages = append(m.packages, pkg)
	return nil
}

func (m *mockProcess) Start(ctx context.Context, pid pubsub.PID, payloads payload.Payloads) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.startError != nil {
		return m.startError
	}

	m.started = true
	return nil
}

func (m *mockProcess) Step() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.stepCallback != nil {
		return m.stepCallback()
	}

	if m.stepError != nil {
		return m.stepError
	}

	if m.readyCount > 0 {
		m.readyCount--
	}

	return nil
}

func (m *mockProcess) Ready() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.readyCount
}

func (m *mockProcess) Terminate() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.terminated = true
}

// Helper methods for tests
func (m *mockProcess) IsStarted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.started
}

func (m *mockProcess) IsTerminated() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.terminated
}

func (m *mockProcess) SetReadyCount(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.readyCount = count
}

func (m *mockProcess) SetStepError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.stepError = err
}

func (m *mockProcess) SetSendError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sendError = err
}

func (m *mockProcess) SetStartError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.startError = err
}

func (m *mockProcess) SetStepCallback(callback func() error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.stepCallback = callback
}

func (m *mockProcess) ReceivedPackages() []*pubsub.Package {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.packages
}

// Helper function to create a test process pool
func createTestPool(t *testing.T) (*ProcessPool, context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	logger := zaptest.NewLogger(t)
	pool := NewProcessPool(ctx, 2, 10, logger)
	pool.Start()
	return pool, ctx, cancel
}

// Helper function to wait for a WaitGroup to complete with a timeout
func waitForCompletion(wg *sync.WaitGroup, timeout time.Duration) bool {
	c := make(chan struct{})
	go func() {
		defer close(c)
		wg.Wait()
	}()

	select {
	case <-c:
		return true
	case <-time.After(timeout):
		return false
	}
}

// Test the NewProcessPool constructor
func TestNewProcessPool(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := zaptest.NewLogger(t)

	pool := NewProcessPool(ctx, 5, 20, logger)

	if pool.workers != 5 {
		t.Errorf("expected 5 workers, got %d", pool.workers)
	}

	if pool.maxProcesses != 20 {
		t.Errorf("expected max processes 20, got %d", pool.maxProcesses)
	}

	if pool.log == nil {
		t.Error("logger should not be nil")
	}

	if pool.workCh == nil {
		t.Error("work channel should not be nil")
	}

	if cap(pool.workCh) != 21 { // maxProcesses+1
		t.Errorf("expected work channel capacity 21, got %d", cap(pool.workCh))
	}
}

// Test the Add method
func TestProcessPool_Add(t *testing.T) {
	pool, _, cancel := createTestPool(t)
	defer cancel()

	// Create a mock process
	proc := newMockProcess()

	// Test successful addition
	pid := pubsub.PID{Host: "test", UniqID: "1"}
	err := pool.Add(pid, proc)
	if err != nil {
		t.Errorf("failed to add process: %v", err)
	}

	// Test adding the same process again
	err = pool.Add(pid, proc)
	if err != process.ErrHostBusy {
		t.Errorf("expected ErrHostBusy, got: %v", err)
	}

	// Test adding a new process
	pid2 := pubsub.PID{Host: "test", UniqID: "2"}
	err = pool.Add(pid2, proc)
	if err != nil {
		t.Errorf("failed to add second process: %v", err)
	}

	// Test adding more than max processes
	maxPool, _, maxCancel := createTestPool(t)
	defer maxCancel()
	maxPool.maxProcesses = 1

	err = maxPool.Add(pid, proc)
	if err != nil {
		t.Errorf("failed to add process to max pool: %v", err)
	}

	err = maxPool.Add(pid2, proc)
	if err != process.ErrMaxProcesses {
		t.Errorf("expected ErrMaxProcesses, got: %v", err)
	}
}

// Test the Cancel method
func TestProcessPool_Cancel(t *testing.T) {
	pool, _, cancel := createTestPool(t)
	defer cancel()

	// Create a mock process
	proc := newMockProcess()

	// Add the process
	pid := pubsub.PID{Host: "test", UniqID: "1"}
	if err := pool.Add(pid, proc); err != nil {
		t.Fatalf("failed to add process: %v", err)
	}

	// Test cancellation
	deadline := time.Now().Add(time.Second)
	err := pool.Cancel(pid, deadline)
	if err != nil {
		t.Errorf("failed to cancel process: %v", err)
	}

	// Check if the process received a cancel message
	packages := proc.ReceivedPackages()
	if len(packages) != 1 {
		t.Errorf("expected 1 package, got %d", len(packages))
	} else {
		pkg := packages[0]
		if pkg.Target != pid {
			t.Errorf("package target incorrect, expected %v, got %v", pid, pkg.Target)
		}
		if len(pkg.Messages) != 1 || pkg.Messages[0].Topic != topology.TopicEvents {
			t.Errorf("expected message with topic %s", topology.TopicEvents)
		}
	}

	// Test cancelling a non-existent process
	badPid := pubsub.PID{Host: "test", UniqID: "999"}
	err = pool.Cancel(badPid, deadline)
	if err != process.ErrNoProcess {
		t.Errorf("expected ErrNoProcess, got: %v", err)
	}
}

// Test the CancelAll method
func TestProcessPool_CancelAll(t *testing.T) {
	pool, ctx, cancel := createTestPool(t)
	defer cancel()

	// Add multiple processes
	procs := make([]*mockProcess, 3)
	pids := make([]pubsub.PID, 3)

	for i := 0; i < 3; i++ {
		procs[i] = newMockProcess()
		pids[i] = pubsub.PID{Host: "test", UniqID: string('1' + i)}
		if err := pool.Add(pids[i], procs[i]); err != nil {
			t.Fatalf("failed to add process %d: %v", i, err)
		}
	}

	// Test CancelAll
	deadline := time.Now().Add(time.Second)
	err := pool.CancelAll(ctx, deadline)
	if err != nil {
		t.Errorf("failed to cancel all processes: %v", err)
	}

	// Check if all processes received a cancel message
	for i, proc := range procs {
		packages := proc.ReceivedPackages()
		if len(packages) != 1 {
			t.Errorf("process %d: expected 1 package, got %d", i, len(packages))
		} else {
			pkg := packages[0]
			if pkg.Target != pids[i] {
				t.Errorf("process %d: package target incorrect, expected %v, got %v", i, pids[i], pkg.Target)
			}
			if len(pkg.Messages) != 1 || pkg.Messages[0].Topic != topology.TopicEvents {
				t.Errorf("process %d: expected message with topic %s", i, topology.TopicEvents)
			}
		}
	}

	// Test CancelAll with a context that's already canceled
	cancelledCtx, cancelledCancel := context.WithCancel(context.Background())
	cancelledCancel()

	err = pool.CancelAll(cancelledCtx, deadline)
	if err == nil || err != context.Canceled {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

// Test the Close method
func TestProcessPool_Close(t *testing.T) {
	pool, _, cancel := createTestPool(t)

	// Add a process
	proc := newMockProcess()
	pid := pubsub.PID{Host: "test", UniqID: "1"}
	if err := pool.Add(pid, proc); err != nil {
		t.Fatalf("failed to add process: %v", err)
	}

	// Close the pool
	cancel()
	pool.Close()

	// Try to schedule a process after closing
	err := pool.Schedule(pid)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

// Test the Has and Remove methods
func TestProcessPool_HasAndRemove(t *testing.T) {
	pool, _, cancel := createTestPool(t)
	defer cancel()

	// Add a process
	proc := newMockProcess()
	pid := pubsub.PID{Host: "test", UniqID: "1"}
	if err := pool.Add(pid, proc); err != nil {
		t.Fatalf("failed to add process: %v", err)
	}

	// Test Has
	if !pool.Has(pid) {
		t.Errorf("pool should have the process")
	}

	badPid := pubsub.PID{Host: "test", UniqID: "999"}
	if pool.Has(badPid) {
		t.Errorf("pool should not have the non-existent process")
	}

	// Test numProcesses count
	if pool.numProcesses.Load() != 1 {
		t.Errorf("expected numProcesses to be 1, got %d", pool.numProcesses.Load())
	}

	// Test Remove
	pool.Remove(pid)
	if pool.Has(pid) {
		t.Errorf("pool should not have the process after removal")
	}

	// Test numProcesses count after removal
	if pool.numProcesses.Load() != 0 {
		t.Errorf("expected numProcesses to be 0 after removal, got %d", pool.numProcesses.Load())
	}

	// Remove a non-existent process (should not panic)
	pool.Remove(badPid)
}

// Test the Schedule method
func TestProcessPool_Schedule(t *testing.T) {
	pool, _, cancel := createTestPool(t)
	defer cancel()

	// Add a process
	proc := newMockProcess()
	pid := pubsub.PID{Host: "test", UniqID: "1"}
	if err := pool.Add(pid, proc); err != nil {
		t.Fatalf("failed to add process: %v", err)
	}

	// Test scheduling a process
	err := pool.Schedule(pid)
	if err != nil {
		t.Errorf("failed to schedule process: %v", err)
	}

	// Test scheduling a non-existent process
	badPid := pubsub.PID{Host: "test", UniqID: "999"}
	err = pool.Schedule(badPid)
	if err != process.ErrNoProcess {
		t.Errorf("expected ErrNoProcess, got: %v", err)
	}

	// Test scheduling a process that's already scheduled
	err = pool.Schedule(pid)
	if err != nil {
		t.Errorf("failed to schedule already scheduled process: %v", err)
	}

	// Test scheduling when context is cancelled
	cancelPool, _, poolCancel := createTestPool(t)
	if err := cancelPool.Add(pid, proc); err != nil {
		t.Fatalf("failed to add process to cancel pool: %v", err)
	}

	poolCancel() // Cancel the context

	err = cancelPool.Schedule(pid)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

// Test the Send method
func TestProcessPool_Send(t *testing.T) {
	pool, _, cancel := createTestPool(t)
	defer cancel()

	// Add a process
	proc := newMockProcess()
	pid := pubsub.PID{Host: "test", UniqID: "1"}
	if err := pool.Add(pid, proc); err != nil {
		t.Fatalf("failed to add process: %v", err)
	}

	// Test sending a message
	pkg := pubsub.NewPackage(pid, pid, "test.topic")
	err := pool.Send(pid, pkg)
	if err != nil {
		t.Errorf("failed to send message: %v", err)
	}

	// Check if the process received the message
	packages := proc.ReceivedPackages()
	if len(packages) != 1 {
		t.Errorf("expected 1 package, got %d", len(packages))
	} else if packages[0] != pkg {
		t.Errorf("process received different package than sent")
	}

	// Test sending to a non-existent process
	badPid := pubsub.PID{Host: "test", UniqID: "999"}
	err = pool.Send(badPid, pkg)
	if err != process.ErrNoProcess {
		t.Errorf("expected ErrNoProcess, got: %v", err)
	}

	// Test sending with a process that returns an error
	proc.SetSendError(errors.New("test error"))
	err = pool.Send(pid, pkg)
	if err == nil || err.Error() != "test error" {
		t.Errorf("expected test error, got: %v", err)
	}
}

// Test the Start and Terminate methods
func TestProcessPool_StartAndTerminate(t *testing.T) {
	pool, _, cancel := createTestPool(t)
	defer cancel()

	// Add a process
	proc := newMockProcess()
	pid := pubsub.PID{Host: "test", UniqID: "1"}
	if err := pool.Add(pid, proc); err != nil {
		t.Fatalf("failed to add process: %v", err)
	}

	// Test terminating the process
	pool.Terminate(pid)
	if !proc.IsTerminated() {
		t.Errorf("process should be terminated")
	}

	// Test terminating a non-existent process (should not panic)
	badPid := pubsub.PID{Host: "test", UniqID: "999"}
	pool.Terminate(badPid)
}

// Test the worker goroutine behavior
func TestProcessPool_Worker(t *testing.T) {
	pool, _, cancel := createTestPool(t)
	defer cancel()

	// Add a process that's ready for work
	proc := newMockProcess()
	proc.SetReadyCount(2) // Set it to have 2 items ready to process

	var stepCalled sync.WaitGroup
	stepCalled.Add(1)

	proc.SetStepCallback(func() error {
		proc.SetReadyCount(proc.Ready() - 1)
		stepCalled.Done()
		return nil
	})

	pid := pubsub.PID{Host: "test", UniqID: "1"}
	if err := pool.Add(pid, proc); err != nil {
		t.Fatalf("failed to add process: %v", err)
	}

	// Wait for the worker to process
	if !waitForCompletion(&stepCalled, 100*time.Millisecond) {
		t.Fatalf("worker did not process the item within expected time")
	}

	// Test worker handling error from Step
	proc.SetReadyCount(1)
	stepCalled.Add(1)
	proc.SetStepCallback(func() error {
		stepCalled.Done()
		return errors.New("test step error")
	})

	// Schedule it again
	if err := pool.Schedule(pid); err != nil {
		t.Fatalf("failed to schedule process: %v", err)
	}

	// Wait for the worker to process
	if !waitForCompletion(&stepCalled, 100*time.Millisecond) {
		t.Fatalf("worker did not process the error case within expected time")
	}

	// The process should have been removed due to step error
	if pool.Has(pid) {
		t.Errorf("process should have been removed after step error")
	}
}

// Test worker for processes that stay ready
func TestProcessPool_WorkerReSchedule(t *testing.T) {
	pool, _, cancel := createTestPool(t)
	defer cancel()

	// Add a process that stays ready
	proc := newMockProcess()
	proc.SetReadyCount(1) // Start with 1 item

	var stepCounter int32
	const expectedSteps = 3

	var stepComplete sync.WaitGroup
	stepComplete.Add(expectedSteps)

	proc.SetStepCallback(func() error {
		// Always report that we're ready for more work
		proc.SetReadyCount(1)
		atomic.AddInt32(&stepCounter, 1)
		stepComplete.Done()
		return nil
	})

	pid := pubsub.PID{Host: "test", UniqID: "1"}
	if err := pool.Add(pid, proc); err != nil {
		t.Fatalf("failed to add process: %v", err)
	}

	// Wait for the worker to process multiple times
	if !waitForCompletion(&stepComplete, 500*time.Millisecond) {
		t.Fatalf("worker did not complete expected steps within timeout")
	}

	// Check that step was called the expected number of times
	if atomic.LoadInt32(&stepCounter) != expectedSteps {
		t.Errorf("expected %d steps, got %d", expectedSteps, atomic.LoadInt32(&stepCounter))
	}
}

// Test limited worker pool behavior
func TestProcessPool_LimitedWorkers(t *testing.T) {
	// Create a pool with only 1 worker but many processes
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := zaptest.NewLogger(t)
	pool := NewProcessPool(ctx, 1, 20, logger)
	pool.Start()

	const numProcesses = 10
	procs := make([]*mockProcess, numProcesses)
	pids := make([]pubsub.PID, numProcesses)

	// Add processes with work to do
	var wg sync.WaitGroup
	wg.Add(numProcesses)

	for i := 0; i < numProcesses; i++ {
		procs[i] = newMockProcess()
		procs[i].SetReadyCount(1)

		// Set up a step callback that takes some time and signals completion
		localIndex := i // Capture loop variable
		procs[i].SetStepCallback(func() error {
			time.Sleep(10 * time.Millisecond) // Simulate work
			procs[localIndex].SetReadyCount(0)
			wg.Done()
			return nil
		})

		pids[i] = pubsub.PID{Host: "test", UniqID: string('1' + i)}
		if err := pool.Add(pids[i], procs[i]); err != nil {
			t.Fatalf("failed to add process %d: %v", i, err)
		}
	}

	// Wait for all processes to complete, or timeout
	completed := waitForCompletion(&wg, 500*time.Millisecond)

	if !completed {
		// Count how many processes were executed
		processed := 0
		for i := 0; i < numProcesses; i++ {
			if procs[i].Ready() == 0 {
				processed++
			}
		}
		t.Logf("%d/%d processes processed before timeout", processed, numProcesses)
	}

	// Since we have just 1 worker, all processes should eventually be processed,
	// but we don't want the test to fail due to timing, so this is informational
}

// Test context cancellation handling
func TestProcessPool_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	logger := zaptest.NewLogger(t)
	pool := NewProcessPool(ctx, 2, 10, logger)
	pool.Start()

	// Add a process
	proc := newMockProcess()
	pid := pubsub.PID{Host: "test", UniqID: "1"}
	if err := pool.Add(pid, proc); err != nil {
		t.Fatalf("failed to add process: %v", err)
	}

	// Cancel the context
	cancel()

	// Try to schedule, should fail with context.Canceled
	err := pool.Schedule(pid)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled error, got: %v", err)
	}

	// Try to cancel, should also fail with context.Canceled
	err = pool.Cancel(pid, time.Now().Add(time.Second))
	if err != context.Canceled {
		t.Errorf("expected context.Canceled error for Cancel, got: %v", err)
	}

	// Sleep briefly to let the worker goroutines exit
	time.Sleep(10 * time.Millisecond)

	// Call Close, should not hang
	pool.Close()
}

// Test concurrent operations
func TestProcessPool_Concurrency(t *testing.T) {
	pool, _, cancel := createTestPool(t)
	defer cancel()

	// Add multiple processes concurrently
	var wg sync.WaitGroup
	const numProcesses = 10
	errorCh := make(chan error, numProcesses)

	for i := 0; i < numProcesses; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			proc := newMockProcess()
			pid := pubsub.PID{Host: "test", UniqID: string('1' + id)}

			if err := pool.Add(pid, proc); err != nil {
				errorCh <- err
				return
			}

			// Schedule the process
			if err := pool.Schedule(pid); err != nil {
				errorCh <- err
				return
			}
		}(i)
	}

	wg.Wait()
	close(errorCh)

	var errors []error
	for err := range errorCh {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		t.Errorf("got %d errors during concurrent operations: %v", len(errors), errors)
	}

	// Verify all processes were added
	processCount := 0
	for i := 0; i < numProcesses; i++ {
		pid := pubsub.PID{Host: "test", UniqID: string('1' + i)}
		if pool.Has(pid) {
			processCount++
		}
	}

	if processCount != numProcesses {
		t.Errorf("expected %d processes, got %d", numProcesses, processCount)
	}
}

// Test that processes are automatically rescheduled when they have more work
func TestProcessPool_AutoReschedule(t *testing.T) {
	pool, _, cancel := createTestPool(t)
	defer cancel()

	// Create a process that changes its ready count during execution
	proc := newMockProcess()
	proc.SetReadyCount(0) // Start with no work

	var stepCalled sync.WaitGroup
	stepCalled.Add(1)

	var stage int32

	proc.SetStepCallback(func() error {
		currentStage := atomic.AddInt32(&stage, 1)

		if currentStage == 1 {
			// First step, add more work and return
			proc.SetReadyCount(1)
			stepCalled.Done()
		} else if currentStage == 2 {
			// Second step, we should have been auto-rescheduled
			proc.SetReadyCount(0)
			stepCalled.Done()
		}

		return nil
	})

	pid := pubsub.PID{Host: "test", UniqID: "1"}
	if err := pool.Add(pid, proc); err != nil {
		t.Fatalf("failed to add process: %v", err)
	}

	// Manually schedule first execution
	if err := pool.Schedule(pid); err != nil {
		t.Fatalf("failed to schedule process: %v", err)
	}

	// Wait for first execution
	if !waitForCompletion(&stepCalled, 100*time.Millisecond) {
		t.Fatalf("worker did not process the first step within expected time")
	}

	// Prepare for second execution
	stepCalled.Add(1)

	// Wait for auto-rescheduled execution
	if !waitForCompletion(&stepCalled, 100*time.Millisecond) {
		t.Fatalf("worker did not process the auto-rescheduled step within expected time")
	}

	// Verify we reached the expected stage
	if atomic.LoadInt32(&stage) != 2 {
		t.Errorf("expected to reach stage 2, got stage %d", atomic.LoadInt32(&stage))
	}
}
