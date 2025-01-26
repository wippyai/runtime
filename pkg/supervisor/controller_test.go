package supervisor

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/supervisor"
)

// mockService implements supervisor.Lifecycle for testing
type mockService struct {
	startFunc func(context.Context) (<-chan any, error)
	stopFunc  func(context.Context) error
}

func (m *mockService) Start(ctx context.Context) (<-chan any, error) {
	return m.startFunc(ctx)
}

func (m *mockService) Stop(ctx context.Context) error {
	return m.stopFunc(ctx)
}

func TestController_BasicLifecycle(t *testing.T) {
	detailsCh := make(chan any, 1)
	var receivedStates []struct {
		status  supervisor.Status
		details any
	}
	var statesMutex sync.Mutex

	mock := &mockService{
		startFunc: func(ctx context.Context) (<-chan any, error) {
			time.Sleep(100 * time.Millisecond)
			detailsCh <- "service running"
			time.Sleep(100 * time.Millisecond)
			return detailsCh, nil
		},
		stopFunc: func(ctx context.Context) error {
			time.Sleep(100 * time.Millisecond)
			close(detailsCh)
			time.Sleep(100 * time.Millisecond)
			return nil
		},
	}

	ctr := NewController(
		context.Background(),
		mock,
		supervisor.LifecycleConfig{
			StartTimeout: 5 * time.Second,
			StopTimeout:  5 * time.Second,
			RetryPolicy: supervisor.RetryPolicy{
				MaxAttempts: 3,
			},
		},
		func(status supervisor.Status, details any) {
			statesMutex.Lock()
			receivedStates = append(receivedStates, struct {
				status  supervisor.Status
				details any
			}{status, details})
			statesMutex.Unlock()
		},
	)

	// Test initial state
	if state := ctr.state.getSnapshot(); state.status != supervisor.Unknown {
		t.Errorf("Expected initial Status Unknown, got %v", state.status)
	}

	err := ctr.Start()
	if err != nil {
		t.Fatalf("Failed to start supervisor: %v", err)
	}

	if state := ctr.state.getSnapshot(); state.status != supervisor.Running {
		t.Errorf("Expected Status Running, got %v", state.status)
	}

	time.Sleep(100 * time.Millisecond) // wait for service Details to propagate

	// Test transition to Stopped
	if err := ctr.Stop(); err != nil {
		t.Fatalf("Failed to transition to Stopped: %v", err)
	}

	if state := ctr.state.getSnapshot(); state.status != supervisor.Stopped {
		t.Errorf("Expected Status Stopped, got %v", state.status)
	}

	// Get final states safely
	statesMutex.Lock()
	finalStates := make([]struct {
		status  supervisor.Status
		details any
	}, len(receivedStates))
	copy(finalStates, receivedStates)
	statesMutex.Unlock()

	expectedStates := []supervisor.Status{
		supervisor.Starting,
		supervisor.Running,
		supervisor.Running, // updated by service details
		supervisor.Stopping,
		supervisor.Stopped,
	}

	if len(finalStates) != len(expectedStates) {
		t.Errorf("Expected %d state transitions, got %d", len(expectedStates), len(finalStates))
		t.Logf("Received states: %v", finalStates)
		return
	}

	for i, expected := range expectedStates {
		if finalStates[i].status != expected {
			t.Errorf("State transition %d: expected %v, got %v", i, expected, finalStates[i].status)
		}
	}
}

func TestController_ServiceFailure(t *testing.T) {
	detailsCh := make(chan any)
	attempts := 0
	stateReached := make(chan struct{}, 1)

	mock := &mockService{
		startFunc: func(ctx context.Context) (<-chan any, error) {
			attempts++
			if attempts == 1 {
				return nil, errors.New("initial failure")
			}
			return detailsCh, nil
		},
		stopFunc: func(ctx context.Context) error {
			close(detailsCh)
			return nil
		},
	}

	ctr := NewController(
		context.Background(),
		mock,
		supervisor.LifecycleConfig{
			StartTimeout: 5 * time.Second,
			StopTimeout:  5 * time.Second,
			RetryPolicy: supervisor.RetryPolicy{
				MaxAttempts:  3,
				InitialDelay: 100 * time.Millisecond,
			},
		},
		func(status supervisor.Status, details any) {
			if status == supervisor.Running {
				select {
				case stateReached <- struct{}{}:
				default:
				}
			}
		},
	)

	err := ctr.Start()
	if err != nil {
		t.Fatalf("Failed to start supervisor: %v", err)
	}

	// wait for service to reach Running state after recovery
	<-stateReached

	if state := ctr.state.getSnapshot(); state.status != supervisor.Running {
		t.Errorf("Expected Status Running after recovery, got %v", state.status)
	}

	if attempts != 2 {
		t.Errorf("Expected 2 start attempts, got %d", attempts)
	}

	if err := ctr.Stop(); err != nil {
		t.Fatalf("Failed to stop supervisor: %v", err)
	}
}

func TestController_StartupError(t *testing.T) {
	stateReached := make(chan struct{}, 1)
	expectedErr := errors.New("startup failed")

	mock := &mockService{
		startFunc: func(ctx context.Context) (<-chan any, error) {
			return nil, expectedErr
		},
		stopFunc: func(ctx context.Context) error {
			return nil
		},
	}

	ctr := NewController(
		context.Background(),
		mock,
		supervisor.LifecycleConfig{
			StartTimeout: time.Second,
			RetryPolicy:  supervisor.RetryPolicy{MaxAttempts: 1},
		},
		func(status supervisor.Status, details any) {
			if status == supervisor.Failed {
				select {
				case stateReached <- struct{}{}:
				default:
				}
			}
		},
	)

	err := ctr.Start()
	if err == nil {
		t.Fatal("Expected startup error, got nil")
	}

	<-stateReached

	state := ctr.state.getSnapshot()
	if state.status != supervisor.Failed {
		t.Errorf("Expected Failed Status, got %v", state.status)
	}
}

func TestController_ServiceRecoveryAfterFailure(t *testing.T) {
	var currentChan chan any
	var chanMutex sync.Mutex
	stateTransitions := make([]supervisor.Status, 0)
	var statesMutex sync.Mutex

	mock := &mockService{
		startFunc: func(ctx context.Context) (<-chan any, error) {
			chanMutex.Lock()
			// Create a new channel each time the service starts
			currentChan = make(chan any, 1)
			ch := currentChan // local copy to return
			chanMutex.Unlock()

			// Simulate service startup message
			ch <- "service started"

			return ch, nil
		},
		stopFunc: func(ctx context.Context) error {
			return nil
		},
	}

	ctr := NewController(
		context.Background(),
		mock,
		supervisor.LifecycleConfig{
			StartTimeout: 5 * time.Second,
			StopTimeout:  5 * time.Second,
			RetryPolicy: supervisor.RetryPolicy{
				MaxAttempts:  3,
				InitialDelay: 100 * time.Millisecond,
			},
		},
		func(status supervisor.Status, details any) {
			statesMutex.Lock()
			stateTransitions = append(stateTransitions, status)
			statesMutex.Unlock()
		},
	)

	// Start the service
	if err := ctr.Start(); err != nil {
		t.Fatalf("Failed to start service: %v", err)
	}

	// wait for initial startup and first Status update
	time.Sleep(200 * time.Millisecond)

	// Verify service is running
	state := ctr.state.getSnapshot()
	if state.status != supervisor.Running {
		t.Fatalf("Expected service to be Running, got %v", state.status)
	}

	// Simulate service death by closing the current channel
	chanMutex.Lock()
	if currentChan != nil {
		close(currentChan)
	}
	chanMutex.Unlock()

	// wait for recovery process
	time.Sleep(500 * time.Millisecond)

	// Verify service recovered
	state = ctr.state.getSnapshot()
	if state.status != supervisor.Running {
		t.Fatalf("Expected service to be Running after recovery, got %v", state.status)
	}

	// stop the supervisor
	if err := ctr.Stop(); err != nil {
		t.Fatalf("Failed to stop supervisor: %v", err)
	}

	time.Sleep(100 * time.Millisecond) // wait for final state update

	// Get final state transitions
	statesMutex.Lock()
	transitions := make([]supervisor.Status, len(stateTransitions))
	copy(transitions, stateTransitions)
	statesMutex.Unlock()

	// Verify the state transition sequence
	expectedTransitions := []supervisor.Status{
		supervisor.Starting, // Initial start
		supervisor.Running,  // First successful start
		supervisor.Running,  // Lifecycle Details received
		supervisor.Failed,   // Lifecycle death
		supervisor.Starting, // Recovery attempt
		supervisor.Running,  // Recovery successful
		supervisor.Running,  // Lifecycle Details received after recovery
		supervisor.Stopping, // Clean shutdown
		supervisor.Stopped,  // Final state
	}

	if len(transitions) != len(expectedTransitions) {
		t.Errorf("Expected %d transitions, got %d", len(expectedTransitions), len(transitions))
		t.Logf("Actual transitions: %v", transitions)
		return
	}

	for i, expected := range expectedTransitions {
		if transitions[i] != expected {
			t.Errorf("Transition %d: expected %v, got %v", i, expected, transitions[i])
		}
	}
}

func TestController_ServiceFailedRecovery(t *testing.T) {
	var currentChan chan any
	var chanMutex sync.Mutex
	stateTransitions := make([]supervisor.Status, 0)
	var statesMutex sync.Mutex
	attempts := 0
	maxRetries := 2
	stateChan := make(chan struct{})
	var once sync.Once

	mock := &mockService{
		startFunc: func(ctx context.Context) (<-chan any, error) {
			attempts++
			chanMutex.Lock()
			defer chanMutex.Unlock()

			if attempts == 1 {
				// First attempt succeeds
				currentChan = make(chan any, 1)
				currentChan <- "service started"
				return currentChan, nil
			}

			// All subsequent attempts fail immediately
			return nil, fmt.Errorf("start attempt %d failed", attempts)
		},
		stopFunc: func(ctx context.Context) error {
			return nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ctr := NewController(
		ctx,
		mock,
		supervisor.LifecycleConfig{
			StartTimeout: 1 * time.Second,
			StopTimeout:  1 * time.Second,
			RetryPolicy: supervisor.RetryPolicy{
				MaxAttempts:  maxRetries,
				InitialDelay: 100 * time.Millisecond,
			},
		},
		func(status supervisor.Status, details any) {
			statesMutex.Lock()
			defer statesMutex.Unlock()

			select {
			case <-ctx.Done():
				return
			default:
				stateTransitions = append(stateTransitions, status)
				if status == supervisor.Failed {
					if attempts > maxRetries {
						once.Do(func() {
							close(stateChan)
						})
					}
				}
			}
		},
	)

	// Start the service
	if err := ctr.Start(); err != nil {
		cancel()
		t.Fatalf("Failed to start service: %v", err)
	}

	// wait for initial startup and Details
	time.Sleep(100 * time.Millisecond)

	// Verify service is initially running
	state := ctr.state.getSnapshot()
	if state.status != supervisor.Running {
		cancel()
		t.Fatalf("Expected service to be Running initially, got %v", state.status)
	}

	// Simulate service death
	chanMutex.Lock()
	if currentChan != nil {
		close(currentChan)
	}
	chanMutex.Unlock()

	// wait for service to reach final failed state
	select {
	case <-stateChan:
		// Lifecycle reached final failed state
	case <-time.After(time.Second):
		cancel()
		t.Fatal("timeout waiting for service to reach final failed state")
	}

	// Get state transitions before cleanup
	statesMutex.Lock()
	transitions := make([]supervisor.Status, len(stateTransitions))
	copy(transitions, stateTransitions)
	statesMutex.Unlock()

	if err := ctr.Stop(); err != nil {
		t.Logf("Error during supervisor shutdown: %v", err)
	}

	// Verify the complete state transition sequence
	expectedTransitions := []supervisor.Status{
		supervisor.Starting, // Initial start
		supervisor.Running,  // First successful start
		supervisor.Running,  // Lifecycle Details received
		supervisor.Failed,   // Lifecycle death
		supervisor.Starting, // First recovery attempt
		supervisor.Failed,   // First recovery failure
		supervisor.Starting, // Second recovery attempt
		supervisor.Failed,   // Recovery failure
	}

	if len(transitions) != len(expectedTransitions) {
		t.Errorf("Expected %d transitions, got %d", len(expectedTransitions), len(transitions))
		t.Logf("Actual transitions: %v", transitions)
		t.Logf("Attempts made: %d", attempts)
		return
	}

	for i, expected := range expectedTransitions {
		if transitions[i] != expected {
			t.Errorf("Transition %d: expected %v, got %v", i, expected, transitions[i])
		}
	}

	defer cancel()

	// Verify retry count
	expectedAttempts := maxRetries + 1 // Initial attempt + maxRetries
	if attempts != expectedAttempts {
		t.Errorf("Expected %d total attempts, got %d", expectedAttempts, attempts)
	}
}

func TestController_ServiceStateSnapshot(t *testing.T) {
	state := newServiceState()
	state.status = supervisor.Running
	state.desired = supervisor.Running
	state.retryCount = 5
	state.lastUpdate = time.Now()
	state.details = "test Details"

	snapshot := state.getSnapshot()

	if snapshot.status != state.status {
		t.Errorf("Status mismatch: expected %v, got %v", state.status, snapshot.status)
	}
	if snapshot.desired != state.desired {
		t.Errorf("Desired Status mismatch: expected %v, got %v", state.desired, snapshot.desired)
	}
	if snapshot.retryCount != state.retryCount {
		t.Errorf("Retry count mismatch: expected %v, got %v", state.retryCount, snapshot.retryCount)
	}
	if !snapshot.lastUpdate.Equal(state.lastUpdate) {
		t.Errorf("Last update mismatch: expected %v, got %v", state.lastUpdate, snapshot.lastUpdate)
	}
	if snapshot.details != state.details {
		t.Errorf("Details mismatch: expected %v, got %v", state.details, snapshot.details)
	}
}

func TestController_ServiceDetailsUpdate(t *testing.T) {
	state := newServiceState()
	initialStatus := supervisor.Running
	state.status = initialStatus

	testCases := []struct {
		name        string
		details     any
		wantStatus  supervisor.Status
		wantDetails any
	}{
		{
			name:        "update with string payload",
			details:     "test Details",
			wantStatus:  initialStatus,
			wantDetails: "test Details",
		},
		{
			name:        "update with error payload",
			details:     errors.New("test error"),
			wantStatus:  initialStatus,
			wantDetails: errors.New("test error"),
		},
		{
			name:        "update with nil payload",
			details:     nil,
			wantStatus:  initialStatus,
			wantDetails: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotStatus, gotDetails := state.updateDetails(tc.details)

			if gotStatus != tc.wantStatus {
				t.Errorf("Status = %v, want %v", gotStatus, tc.wantStatus)
			}

			if !reflect.DeepEqual(gotDetails, tc.wantDetails) {
				t.Errorf("Details = %v, want %v", gotDetails, tc.wantDetails)
			}

			if !reflect.DeepEqual(state.details, tc.wantDetails) {
				t.Errorf("State Details = %v, want %v", state.details, tc.wantDetails)
			}

			if time.Since(state.lastUpdate) > time.Second {
				t.Error("Last update timestamp not updated")
			}
		})
	}
}

func TestController_CancelDuringTransition(t *testing.T) {
	// opChan to block the first transition
	blockChan := make(chan struct{})
	transitionStarted := make(chan struct{})

	mock := &mockService{
		startFunc: func(ctx context.Context) (<-chan any, error) {
			// First call blocks until we're ready to proceed
			select {
			case <-blockChan:
				return make(chan any), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
		stopFunc: func(ctx context.Context) error {
			return nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	ctr := NewController(
		ctx,
		mock,
		supervisor.LifecycleConfig{
			StartTimeout: 5 * time.Second,
			StopTimeout:  1 * time.Second,
		},
		nil,
	)

	// Start first transition that will block in handleTransition
	go func() {
		_ = ctr.Start()
		close(transitionStarted)
	}()

	// Give time for first transition to be processed
	time.Sleep(100 * time.Millisecond)

	// Start second transition that will block on transitions channel
	errChan := make(chan error, 1)
	go func() {
		errChan <- ctr.Start()
	}()

	// Give time for second transition to block on channel
	time.Sleep(100 * time.Millisecond)

	// Cancel context
	cancel()

	// wait a bit to ensure cancellation is processed
	time.Sleep(100 * time.Millisecond)

	// Now unblock the first transition
	close(blockChan)

	// Get result from second transition
	select {
	case err := <-errChan:
		if err == nil {
			t.Fatal("Expected error from cancelled transition, got nil")
		}
		if !strings.Contains(err.Error(), "supervisor is stopped") {
			t.Errorf("Expected 'supervisor is stopped' error, got: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for transition error")
	}
}

func TestController_StopAndRestart(t *testing.T) {
	var currentChan chan any
	var chanMutex sync.Mutex
	stateTransitions := make([]supervisor.Status, 0)
	var statesMutex sync.Mutex
	startAttempts := 0

	// Channels for synchronizing state transitions
	stateSignals := make(chan supervisor.Status, 10)

	mock := &mockService{
		startFunc: func(ctx context.Context) (<-chan any, error) {
			startAttempts++

			chanMutex.Lock()
			currentChan = make(chan any, 1)
			ch := currentChan // local copy to return
			chanMutex.Unlock()

			ch <- fmt.Sprintf("service started (attempt %d)", startAttempts)

			return ch, nil
		},
		stopFunc: func(ctx context.Context) error {
			chanMutex.Lock()
			if currentChan != nil {
				close(currentChan)
				currentChan = nil
			}
			chanMutex.Unlock()
			return nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ctr := NewController(
		ctx,
		mock,
		supervisor.LifecycleConfig{
			StartTimeout: 1 * time.Second,
			StopTimeout:  1 * time.Second,
			RetryPolicy: supervisor.RetryPolicy{
				MaxAttempts:  3,
				InitialDelay: 100 * time.Millisecond,
			},
		},
		func(status supervisor.Status, details any) {
			statesMutex.Lock()
			stateTransitions = append(stateTransitions, status)
			statesMutex.Unlock()

			select {
			case stateSignals <- status:
			default:
			}
		},
	)

	// Helper function to wait for specific state
	waitForState := func(expectedState supervisor.Status, timeout time.Duration) error {
		timer := time.NewTimer(timeout)
		defer timer.Stop()

		for {
			select {
			case state := <-stateSignals:
				if state == expectedState {
					return nil
				}
			case <-timer.C:
				return fmt.Errorf("timeout waiting for state %v", expectedState)
			}
		}
	}

	// Initial start
	if err := ctr.Start(); err != nil {
		t.Fatalf("Failed initial start: %v", err)
	}

	// wait for first running state (need two Running signals due to details update)
	if err := waitForState(supervisor.Running, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := waitForState(supervisor.Running, time.Second); err != nil {
		t.Fatal(err)
	}

	if state := ctr.state.getSnapshot(); state.status != supervisor.Running {
		t.Fatalf("Expected service to be Running after start, got %v", state.status)
	}

	// stop the service
	if err := ctr.Stop(); err != nil {
		t.Fatalf("Failed to stop service: %v", err)
	}

	// wait for service to stop
	if err := waitForState(supervisor.Stopping, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := waitForState(supervisor.Stopped, time.Second); err != nil {
		t.Fatal(err)
	}

	if state := ctr.state.getSnapshot(); state.status != supervisor.Stopped {
		t.Fatalf("Expected service to be Stopped after stop, got %v", state.status)
	}

	// Restart the service
	if err := ctr.Start(); err != nil {
		t.Fatalf("Failed to restart service: %v", err)
	}

	// wait for second running state
	if err := waitForState(supervisor.Starting, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := waitForState(supervisor.Running, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := waitForState(supervisor.Running, time.Second); err != nil {
		t.Fatal(err)
	}

	if state := ctr.state.getSnapshot(); state.status != supervisor.Running {
		t.Fatalf("Expected service to be Running after restart, got %v", state.status)
	}

	// Final stop
	if err := ctr.Stop(); err != nil {
		t.Fatalf("Failed final stop: %v", err)
	}

	// wait for final stopped state
	if err := waitForState(supervisor.Stopping, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := waitForState(supervisor.Stopped, time.Second); err != nil {
		t.Fatal(err)
	}

	// Verify start attempts
	if startAttempts != 2 {
		t.Errorf("Expected 2 start attempts, got %d", startAttempts)
	}

	// Get final state transitions
	statesMutex.Lock()
	transitions := make([]supervisor.Status, len(stateTransitions))
	copy(transitions, stateTransitions)
	statesMutex.Unlock()

	// Expected state transition sequence
	expectedTransitions := []supervisor.Status{
		supervisor.Starting, // Initial start
		supervisor.Running,  // First running state
		supervisor.Running,  // Lifecycle details received
		supervisor.Stopping, // First stop
		supervisor.Stopped,  // Stopped state
		supervisor.Starting, // Restart
		supervisor.Running,  // Second running state
		supervisor.Running,  // Lifecycle details received
		supervisor.Stopping, // Final stop
		supervisor.Stopped,  // Final state
	}

	if len(transitions) != len(expectedTransitions) {
		t.Errorf("Expected %d transitions, got %d", len(expectedTransitions), len(transitions))
		t.Logf("Actual transitions: %v", transitions)
		return
	}

	for i, expected := range expectedTransitions {
		if transitions[i] != expected {
			t.Errorf("Transition %d: expected %v, got %v", i, expected, transitions[i])
		}
	}
}

func TestController_GracefulShutdown(t *testing.T) {
	var shutdownStarted, shutdownCompleted sync.WaitGroup
	shutdownStarted.Add(1)
	shutdownCompleted.Add(1)

	detailsCh := make(chan any, 1)
	stateTransitions := make([]struct {
		status  supervisor.Status
		details any
	}, 0)
	var statesMutex sync.Mutex

	mock := &mockService{
		startFunc: func(ctx context.Context) (<-chan any, error) {
			detailsCh <- "service running"
			return detailsCh, nil
		},
		stopFunc: func(ctx context.Context) error {
			// Signal that shutdown has started
			shutdownStarted.Done()

			select {
			case detailsCh <- "cleaning up resources":
			case <-ctx.Done():
				return ctx.Err()
			}

			select {
			case <-time.After(500 * time.Millisecond):
			case <-ctx.Done():
				return ctx.Err()
			}

			select {
			case detailsCh <- "closing connections":
			case <-ctx.Done():
				return ctx.Err()
			}

			select {
			case <-time.After(500 * time.Millisecond):
			case <-ctx.Done():
				return ctx.Err()
			}

			close(detailsCh)
			shutdownCompleted.Done()

			return nil
		},
	}

	ctr := NewController(
		context.Background(),
		mock,
		supervisor.LifecycleConfig{
			StartTimeout: 1 * time.Second,
			StopTimeout:  2 * time.Second,
			RetryPolicy: supervisor.RetryPolicy{
				MaxAttempts: 3,
			},
		},
		func(status supervisor.Status, details any) {
			statesMutex.Lock()
			stateTransitions = append(stateTransitions, struct {
				status  supervisor.Status
				details any
			}{status, details})
			statesMutex.Unlock()
		},
	)

	// Start the service
	if err := ctr.Start(); err != nil {
		t.Fatalf("Failed to start service: %v", err)
	}

	// wait for service to be running
	time.Sleep(100 * time.Millisecond)
	if state := ctr.State(); state.Status != supervisor.Running {
		t.Fatalf("Expected service to be Running, got %v", state.Status)
	}

	// Start shutdown in a goroutine
	shutdownErr := make(chan error, 1)
	go func() {
		shutdownErr <- ctr.Stop()
	}()

	// wait for shutdown to start
	shutdownStarted.Wait()

	// wait for shutdown to complete
	shutdownCompleted.Wait()

	// Check if shutdown completed successfully
	select {
	case err := <-shutdownErr:
		if err != nil {
			t.Errorf("Shutdown returned error: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Shutdown did not complete within expected timeframe")
	}

	// Verify state transition sequence with payloads
	statesMutex.Lock()
	transitions := make([]struct {
		status  supervisor.Status
		details any
	}, len(stateTransitions))
	copy(transitions, stateTransitions)
	statesMutex.Unlock()

	expectedTransitions := []struct {
		status  supervisor.Status
		details any
	}{
		{supervisor.Starting, "attempt 1"},
		{supervisor.Running, nil},
		{supervisor.Running, "service running"},
		{supervisor.Stopping, nil},
		{supervisor.Stopping, "cleaning up resources"},
		{supervisor.Stopping, "closing connections"},
		{supervisor.Stopped, nil},
	}

	if len(transitions) != len(expectedTransitions) {
		t.Errorf("Expected %d transitions, got %d", len(expectedTransitions), len(transitions))
		t.Logf("Actual transitions:")
		for i, tr := range transitions {
			t.Logf("%d: status=%v details=%v", i, tr.status, tr.details)
		}
		return
	}

	for i, expected := range expectedTransitions {
		if transitions[i].status != expected.status {
			t.Errorf("Transition %d: expected status %v, got %v", i, expected.status, transitions[i].status)
		}
		if !reflect.DeepEqual(transitions[i].details, expected.details) {
			t.Errorf("Transition %d: expected details %v, got %v", i, expected.details, transitions[i].details)
		}
	}
}

func TestController_ShutdownTimeout(t *testing.T) {
	shutdownStarted := make(chan struct{})
	detailsCh := make(chan any)

	mock := &mockService{
		startFunc: func(ctx context.Context) (<-chan any, error) {
			return detailsCh, nil
		},
		stopFunc: func(ctx context.Context) error {
			close(shutdownStarted)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
				close(detailsCh)
				return nil
			}
		},
	}

	ctx := context.Background()
	ctr := NewController(
		ctx,
		mock,
		supervisor.LifecycleConfig{
			StartTimeout: 1 * time.Second,
			StopTimeout:  500 * time.Millisecond, // Short timeout
			RetryPolicy: supervisor.RetryPolicy{
				MaxAttempts: 1,
			},
		},
		nil,
	)

	// Start the service
	if err := ctr.Start(); err != nil {
		t.Fatalf("Failed to start service: %v", err)
	}

	// wait for service to be running
	time.Sleep(100 * time.Millisecond)

	// Start shutdown in a goroutine
	var wg sync.WaitGroup
	wg.Add(1)
	var shutdownErr error
	go func() {
		defer wg.Done()
		shutdownErr = ctr.Stop()
	}()

	// wait for shutdown to start
	<-shutdownStarted

	// wait for shutdown to complete
	wg.Wait()

	// Verify we got a timeout error
	if shutdownErr == nil {
		t.Error("Expected shutdown to return timeout error, got nil")
	} else if !strings.Contains(shutdownErr.Error(), "failed to stop service") {
		t.Errorf("Expected 'failed to stop service' error, got: %v", shutdownErr)
	}

	// Verify service ended up in stopped state
	finalState := ctr.State()
	if finalState.Status != supervisor.Failed {
		t.Errorf("Expected final state to be Failed (since context was cancelled), got %v", finalState.Status)
	}
}

func TestController_StopDuringFailedStart(t *testing.T) {
	var startCalled int32                    // atomic counter
	startAttempted := make(chan struct{}, 1) // buffered channel for signaling
	startFinished := make(chan struct{})

	mock := &mockService{
		startFunc: func(ctx context.Context) (<-chan any, error) {
			// Signal that we're in start (buffered channel won't block)
			select {
			case startAttempted <- struct{}{}:
			default:
			}

			// Block until context is cancelled or long timeout
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(10 * time.Second):
				return nil, errors.New("fake timeout")
			}
		},
		stopFunc: func(ctx context.Context) error {
			defer func() {
				close(startFinished)
			}()
			return nil
		},
	}

	ctr := NewController(
		context.Background(),
		mock,
		supervisor.LifecycleConfig{
			StartTimeout: 1 * time.Second,
			StopTimeout:  1 * time.Second,
			RetryPolicy: supervisor.RetryPolicy{
				MaxAttempts:  3,
				InitialDelay: 100 * time.Millisecond,
			},
		},
		func(status supervisor.Status, details any) {
		},
	)

	// Start in a goroutine since it will block
	startErr := make(chan error, 1)
	go func() {
		err := ctr.Start()
		startErr <- err
	}()

	// Wait for service to enter start
	select {
	case <-startAttempted:
	case <-time.After(time.Second):
		t.Fatal("Service did not enter start within timeout")
	}

	// Now try to stop while service is starting
	err := ctr.Stop()
	if err == nil {
		t.Errorf("Expected stop timeout error, got nil")
	}

	// Verify the start error indicates cancellation
	select {
	case err := <-startErr:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Expected context.Canceled error, got: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Did not receive start error within timeout")
	}

	// Verify final state
	state := ctr.State()
	if state.Status != supervisor.Failed {
		t.Errorf("Expected final status Failed, got: %v", state.Status)
	}

	// Verify retry count matches expectations
	count := atomic.LoadInt32(&startCalled)
	if count > 2 {
		t.Errorf("Too many retry attempts: %d", count)
	}
}

func TestController_StartTimeout(t *testing.T) {
	startAttempted := make(chan struct{})
	detailsCh := make(chan any)

	mock := &mockService{
		startFunc: func(ctx context.Context) (<-chan any, error) {
			// Signal that we entered start
			close(startAttempted)

			// Block until context cancellation or timeout
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(2 * time.Second): // Simulate slow startup
				return detailsCh, nil
			}
		},
		stopFunc: func(ctx context.Context) error {
			close(detailsCh)
			return nil
		},
	}

	stateTransitions := make([]supervisor.Status, 0)
	var statesMutex sync.Mutex

	ctr := NewController(
		context.Background(),
		mock,
		supervisor.LifecycleConfig{
			StartTimeout: 100 * time.Millisecond, // Short timeout
			StopTimeout:  time.Second,
			RetryPolicy: supervisor.RetryPolicy{
				MaxAttempts: 1, // No retries for clearer testing
			},
		},
		func(status supervisor.Status, details any) {
			statesMutex.Lock()
			defer statesMutex.Unlock()
			stateTransitions = append(stateTransitions, status)
		},
	)

	// Start the service
	err := ctr.Start()

	// Verify we get timeout error
	if err == nil {
		t.Fatal("Expected timeout error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Expected context.DeadlineExceeded error, got: %v", err)
	}

	// Verify the service attempted to start
	select {
	case <-startAttempted:
		// Expected behavior
	case <-time.After(time.Second):
		t.Fatal("Service never attempted to start")
	}

	// Verify state transitions
	statesMutex.Lock()
	transitions := make([]supervisor.Status, len(stateTransitions))
	copy(transitions, stateTransitions)
	statesMutex.Unlock()

	expectedTransitions := []supervisor.Status{
		supervisor.Starting,
		supervisor.Failed,
	}

	if !reflect.DeepEqual(transitions, expectedTransitions) {
		t.Errorf("Expected state transitions %v, got %v", expectedTransitions, transitions)
	}

	// Verify final state
	state := ctr.State()
	if state.Status != supervisor.Failed {
		t.Errorf("Expected final status Failed, got: %v", state.Status)
	}

	// Cleanup
	if err := ctr.Stop(); err == nil {
		t.Fatal("Expected error from stop, got nil")
	}
}

func TestController_ServiceExitError(t *testing.T) {
	stateTransitions := make([]supervisor.Status, 0)
	var statesMutex sync.Mutex

	mock := &mockService{
		startFunc: func(ctx context.Context) (<-chan any, error) {
			// Return ErrExit directly from start
			return nil, supervisor.ErrExit
		},
		stopFunc: func(ctx context.Context) error {
			return nil
		},
	}

	ctr := NewController(
		context.Background(),
		mock,
		supervisor.LifecycleConfig{
			StartTimeout: 1 * time.Second,
			StopTimeout:  1 * time.Second,
			RetryPolicy: supervisor.RetryPolicy{
				MaxAttempts: 3, // Should not retry on ErrExit
			},
		},
		func(status supervisor.Status, details any) {
			statesMutex.Lock()
			defer statesMutex.Unlock()
			stateTransitions = append(stateTransitions, status)
		},
	)

	// Start the service
	err := ctr.Start()

	// Should get ErrExit
	if !errors.Is(err, supervisor.ErrExit) {
		t.Fatalf("Expected supervisor.ErrExit, got: %v", err)
	}

	// Get final state transitions
	statesMutex.Lock()
	transitions := make([]supervisor.Status, len(stateTransitions))
	copy(transitions, stateTransitions)
	statesMutex.Unlock()

	// Expected state transition sequence
	expectedTransitions := []supervisor.Status{
		supervisor.Starting,
		supervisor.Exited, // Should go directly to Exited on ErrExit
	}

	if len(transitions) != len(expectedTransitions) {
		t.Errorf("Expected %d transitions, got %d", len(expectedTransitions), len(transitions))
		t.Logf("Actual transitions: %v", transitions)
		return
	}

	for i, expected := range expectedTransitions {
		if transitions[i] != expected {
			t.Errorf("Transition %d: expected %v, got %v", i, expected, transitions[i])
		}
	}

	// Verify final state
	state := ctr.State()
	if state.Status != supervisor.Exited {
		t.Errorf("Expected final status Exited, got: %v", state.Status)
	}
}

func TestController_ServiceExitDuringOperation(t *testing.T) {
	detailsCh := make(chan any, 1)
	stateTransitions := make([]supervisor.Status, 0)
	var statesMutex sync.Mutex

	mock := &mockService{
		startFunc: func(ctx context.Context) (<-chan any, error) {
			return detailsCh, nil
		},
		stopFunc: func(ctx context.Context) error {
			return nil
		},
	}

	ctr := NewController(
		context.Background(),
		mock,
		supervisor.LifecycleConfig{
			StartTimeout: 1 * time.Second,
			StopTimeout:  1 * time.Second,
			RetryPolicy: supervisor.RetryPolicy{
				MaxAttempts: 3, // Should not retry on ErrExit
			},
		},
		func(status supervisor.Status, details any) {
			statesMutex.Lock()
			defer statesMutex.Unlock()
			stateTransitions = append(stateTransitions, status)
		},
	)

	// Start the service
	err := ctr.Start()
	if err != nil {
		t.Fatalf("Failed to start service: %v", err)
	}

	// Send ErrExit through details channel
	detailsCh <- supervisor.ErrExit

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Get final state transitions
	statesMutex.Lock()
	transitions := make([]supervisor.Status, len(stateTransitions))
	copy(transitions, stateTransitions)
	statesMutex.Unlock()

	// Expected state transition sequence
	expectedTransitions := []supervisor.Status{
		supervisor.Starting,
		supervisor.Running,
		supervisor.Exited, // Should transition to Exited on ErrExit
	}

	if len(transitions) != len(expectedTransitions) {
		t.Errorf("Expected %d transitions, got %d", len(expectedTransitions), len(transitions))
		t.Logf("Actual transitions: %v", transitions)
		return
	}

	for i, expected := range expectedTransitions {
		if transitions[i] != expected {
			t.Errorf("Transition %d: expected %v, got %v", i, expected, transitions[i])
		}
	}

	// Verify final state
	state := ctr.State()
	if state.Status != supervisor.Exited {
		t.Errorf("Expected final status Exited, got: %v", state.Status)
	}
}

func TestController_StressTestStartLast(t *testing.T) {
	var currentChan chan any
	var chanMutex sync.Mutex
	startAttempts := atomic.Int32{}
	stopAttempts := atomic.Int32{}

	mock := &mockService{
		startFunc: func(ctx context.Context) (<-chan any, error) {
			startAttempts.Add(1)
			chanMutex.Lock()
			currentChan = make(chan any, 1)
			ch := currentChan
			ch <- "service started"
			chanMutex.Unlock()

			return ch, nil
		},
		stopFunc: func(ctx context.Context) error {
			stopAttempts.Add(1)
			chanMutex.Lock()
			if currentChan != nil {
				close(currentChan)
				currentChan = nil
			}
			chanMutex.Unlock()
			return nil
		},
	}

	ctr := NewController(
		context.Background(),
		mock,
		supervisor.LifecycleConfig{
			StartTimeout: 1 * time.Second,
			StopTimeout:  1 * time.Second,
			RetryPolicy: supervisor.RetryPolicy{
				MaxAttempts:  3,
				InitialDelay: 50 * time.Millisecond,
			},
		},
		nil,
	)

	const numOperations = 100
	var wg sync.WaitGroup

	// Launch random operations
	for i := 0; i < numOperations-1; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()
			if rand.Float32() < 0.5 {
				_ = ctr.Start()
			} else {
				_ = ctr.Stop()
			}
		}()
	}

	// wait for batch to complete
	wg.Wait()

	// Last operation is always Start
	_ = ctr.Start()

	// Verify final state is Running
	state := ctr.State()
	if state.Status != supervisor.Running {
		t.Errorf("Expected final status Running, got: %v", state.Status)
	}

	// Cleanup
	if err := ctr.Stop(); err != nil {
		t.Errorf("Failed to stop controller: %v", err)
	}
}
