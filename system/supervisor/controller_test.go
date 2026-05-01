// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/supervisor"
)

// mockService implements supervisor.Topology for testing
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
	if testing.Short() {
		t.Skip("skipping lifecycle test in short mode")
	}
	detailsCh := make(chan any, 1)
	var receivedStates []struct {
		details any
		status  supervisor.Status
	}
	var statesMutex sync.Mutex

	mock := &mockService{
		startFunc: func(context.Context) (<-chan any, error) {
			time.Sleep(100 * time.Millisecond)
			detailsCh <- "service running"
			time.Sleep(100 * time.Millisecond)
			return detailsCh, nil
		},
		stopFunc: func(context.Context) error {
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
				details any
				status  supervisor.Status
			}{details, status})
			statesMutex.Unlock()
		},
	)

	// Test initial state
	if state := ctr.state.getSnapshot(); state.status != supervisor.StatusUnknown {
		t.Errorf("Expected initial Status Unknown, got %v", state.status)
	}

	err := ctr.Start()
	if err != nil {
		t.Fatalf("Failed to start supervisor: %v", err)
	}

	if state := ctr.state.getSnapshot(); state.status != supervisor.StatusRunning {
		t.Errorf("Expected Status Running, got %v", state.status)
	}

	time.Sleep(100 * time.Millisecond) // wait for service Details to propagate

	// Test transition to Stopped
	if err := ctr.Stop(); err != nil {
		t.Fatalf("Failed to transition to Stopped: %v", err)
	}

	if state := ctr.state.getSnapshot(); state.status != supervisor.StatusStopped {
		t.Errorf("Expected Status Stopped, got %v", state.status)
	}

	// Spawn final states safely
	statesMutex.Lock()
	finalStates := make([]struct {
		details any
		status  supervisor.Status
	}, len(receivedStates))
	copy(finalStates, receivedStates)
	statesMutex.Unlock()

	expectedStates := []supervisor.Status{
		supervisor.StatusStarting,
		supervisor.StatusRunning,
		supervisor.StatusRunning, // updated by service details
		supervisor.StatusStopping,
		supervisor.StatusStopped,
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
	if testing.Short() {
		t.Skip("skipping failure test in short mode")
	}
	detailsCh := make(chan any)
	attempts := 0
	stateReached := make(chan struct{}, 1)

	mock := &mockService{
		startFunc: func(_ context.Context) (<-chan any, error) {
			attempts++
			if attempts == 1 {
				return nil, errors.New("initial failure")
			}
			return detailsCh, nil
		},
		stopFunc: func(_ context.Context) error {
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
		func(status supervisor.Status, _ any) {
			if status == supervisor.StatusRunning {
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

	if state := ctr.state.getSnapshot(); state.status != supervisor.StatusRunning {
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
	if testing.Short() {
		t.Skip("skipping startup error test in short mode")
	}
	stateReached := make(chan struct{}, 1)
	expectedErr := errors.New("startup failed")

	mock := &mockService{
		startFunc: func(_ context.Context) (<-chan any, error) {
			return nil, expectedErr
		},
		stopFunc: func(context.Context) error {
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
		func(status supervisor.Status, _ any) {
			if status == supervisor.StatusFailed {
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
	if state.status != supervisor.StatusFailed {
		t.Errorf("Expected Failed Status, got %v", state.status)
	}
}

func TestController_StartMayCompleteInBackgroundWhileRetrying(t *testing.T) {
	ctrl := &Controller{
		config: supervisor.LifecycleConfig{
			RetryPolicy: supervisor.RetryPolicy{MaxAttempts: 3},
		},
		state: newInternalState(),
	}
	ctrl.state.setDesiredStatus(supervisor.StatusRunning)
	ctrl.state.updateState(supervisor.StatusFailed, errors.New("temporary failure"))
	ctrl.state.incRetryCount()

	if !ctrl.startMayCompleteInBackground() {
		t.Fatal("finite retrying service should be eligible for optional background completion")
	}

	ctrl.state.incRetryCount()
	ctrl.state.incRetryCount()
	if ctrl.startMayCompleteInBackground() {
		t.Fatal("exhausted finite retry policy should not be treated as retrying in background")
	}

	ctrl.config.RetryPolicy.MaxAttempts = 0
	if !ctrl.startMayCompleteInBackground() {
		t.Fatal("infinite retry policy should be eligible for optional background completion")
	}
}

func TestController_ServiceRecoveryAfterFailure(t *testing.T) {
	var currentChan chan any
	var chanMutex sync.Mutex
	stateTransitions := make([]supervisor.Status, 0)
	var statesMutex sync.Mutex

	mock := &mockService{
		startFunc: func(context.Context) (<-chan any, error) {
			chanMutex.Lock()
			// Spawn a new channel each time the service starts
			currentChan = make(chan any, 1)
			ch := currentChan // local copy to return
			chanMutex.Unlock()

			// Simulate service startup message
			ch <- "service started"

			return ch, nil
		},
		stopFunc: func(context.Context) error {
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
		func(status supervisor.Status, _ any) {
			statesMutex.Lock()
			stateTransitions = append(stateTransitions, status)
			statesMutex.Unlock()
		},
	)

	// Launch the service
	if err := ctr.Start(); err != nil {
		t.Fatalf("Failed to start service: %v", err)
	}

	// wait for initial startup and first Status update
	time.Sleep(200 * time.Millisecond)

	// Verify service is running
	state := ctr.state.getSnapshot()
	if state.status != supervisor.StatusRunning {
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
	if state.status != supervisor.StatusRunning {
		t.Fatalf("Expected service to be Running after recovery, got %v", state.status)
	}

	// stop the supervisor
	if err := ctr.Stop(); err != nil {
		t.Fatalf("Failed to stop supervisor: %v", err)
	}

	time.Sleep(100 * time.Millisecond) // wait for final state update

	// Spawn final state transitions
	statesMutex.Lock()
	transitions := make([]supervisor.Status, len(stateTransitions))
	copy(transitions, stateTransitions)
	statesMutex.Unlock()

	// Verify the state transition sequence
	expectedTransitions := []supervisor.Status{
		supervisor.StatusStarting, // Initial start
		supervisor.StatusRunning,  // First successful start
		supervisor.StatusRunning,  // Topology Details received
		supervisor.StatusFailed,   // Topology death
		supervisor.StatusStarting, // Recovery attempt
		supervisor.StatusRunning,  // Recovery successful
		supervisor.StatusRunning,  // Topology Details received after recovery
		supervisor.StatusStopping, // Clean shutdown
		supervisor.StatusStopped,  // Final state
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
		startFunc: func(context.Context) (<-chan any, error) {
			attempts++
			chanMutex.Lock()
			defer chanMutex.Unlock()

			if attempts == 1 {
				// The first attempt succeeds
				currentChan = make(chan any, 1)
				currentChan <- "service started"
				return currentChan, nil
			}

			// All subsequent attempts fail immediately
			return nil, fmt.Errorf("start attempt %d failed", attempts)
		},
		stopFunc: func(context.Context) error {
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
		func(status supervisor.Status, _ any) {
			statesMutex.Lock()
			defer statesMutex.Unlock()

			select {
			case <-ctx.Done():
				return
			default:
				stateTransitions = append(stateTransitions, status)
				if status == supervisor.StatusFailed {
					if attempts > maxRetries {
						once.Do(func() {
							close(stateChan)
						})
					}
				}
			}
		},
	)

	// Launch the service
	if err := ctr.Start(); err != nil {
		cancel()
		t.Fatalf("Failed to start service: %v", err)
	}

	// wait for initial startup and Details
	time.Sleep(100 * time.Millisecond)

	// Verify service is initially running
	state := ctr.state.getSnapshot()
	if state.status != supervisor.StatusRunning {
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
		// Topology reached final failed state
	case <-time.After(time.Second):
		cancel()
		t.Fatal("timeout waiting for service to reach final failed state")
	}

	// Spawn state transitions before cleanup
	statesMutex.Lock()
	transitions := make([]supervisor.Status, len(stateTransitions))
	copy(transitions, stateTransitions)
	statesMutex.Unlock()

	if err := ctr.Stop(); err != nil {
		t.Logf("Error during supervisor shutdown: %v", err)
	}

	// Verify the complete state transition sequence
	expectedTransitions := []supervisor.Status{
		supervisor.StatusStarting, // Initial start
		supervisor.StatusRunning,  // First successful start
		supervisor.StatusRunning,  // Topology Details received
		supervisor.StatusFailed,   // Topology death
		supervisor.StatusStarting, // First recovery attempt
		supervisor.StatusFailed,   // First recovery failure
		supervisor.StatusStarting, // Second recovery attempt
		supervisor.StatusFailed,   // Recovery failure
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
	state := newInternalState()
	state.status = supervisor.StatusRunning
	state.desired = supervisor.StatusRunning
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
	state := newInternalState()
	initialStatus := supervisor.StatusRunning
	state.status = initialStatus

	testCases := []struct {
		details     any
		wantDetails any
		name        string
		wantStatus  supervisor.Status
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
		stopFunc: func(context.Context) error {
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

	// Launch first transition that will block in handleTransition
	go func() {
		_ = ctr.Start()
		close(transitionStarted)
	}()

	// Give time for first transition to be processed
	time.Sleep(100 * time.Millisecond)

	// Launch second transition that will block on transitions channel
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

	// Spawn result from second transition
	select {
	case err := <-errChan:
		if err == nil {
			t.Fatal("Expected error from canceled transition, got nil")
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
		startFunc: func(context.Context) (<-chan any, error) {
			startAttempts++

			chanMutex.Lock()
			currentChan = make(chan any, 1)
			ch := currentChan // local copy to return
			chanMutex.Unlock()

			ch <- fmt.Sprintf("service started (attempt %d)", startAttempts)

			return ch, nil
		},
		stopFunc: func(context.Context) error {
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
		func(status supervisor.Status, _ any) {
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
	if err := waitForState(supervisor.StatusRunning, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := waitForState(supervisor.StatusRunning, time.Second); err != nil {
		t.Fatal(err)
	}

	if state := ctr.state.getSnapshot(); state.status != supervisor.StatusRunning {
		t.Fatalf("Expected service to be Running after start, got %v", state.status)
	}

	// stop the service
	if err := ctr.Stop(); err != nil {
		t.Fatalf("Failed to stop service: %v", err)
	}

	// wait for service to stop
	if err := waitForState(supervisor.StatusStopping, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := waitForState(supervisor.StatusStopped, time.Second); err != nil {
		t.Fatal(err)
	}

	if state := ctr.state.getSnapshot(); state.status != supervisor.StatusStopped {
		t.Fatalf("Expected service to be Stopped after stop, got %v", state.status)
	}

	// Restart the service
	if err := ctr.Start(); err != nil {
		t.Fatalf("Failed to restart service: %v", err)
	}

	// wait for second running state
	if err := waitForState(supervisor.StatusStarting, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := waitForState(supervisor.StatusRunning, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := waitForState(supervisor.StatusRunning, time.Second); err != nil {
		t.Fatal(err)
	}

	if state := ctr.state.getSnapshot(); state.status != supervisor.StatusRunning {
		t.Fatalf("Expected service to be Running after restart, got %v", state.status)
	}

	// Final stop
	if err := ctr.Stop(); err != nil {
		t.Fatalf("Failed final stop: %v", err)
	}

	// wait for final stopped state
	if err := waitForState(supervisor.StatusStopping, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := waitForState(supervisor.StatusStopped, time.Second); err != nil {
		t.Fatal(err)
	}

	// Verify start attempts
	if startAttempts != 2 {
		t.Errorf("Expected 2 start attempts, got %d", startAttempts)
	}

	// Spawn final state transitions
	statesMutex.Lock()
	transitions := make([]supervisor.Status, len(stateTransitions))
	copy(transitions, stateTransitions)
	statesMutex.Unlock()

	// Expected state transition sequence
	expectedTransitions := []supervisor.Status{
		supervisor.StatusStarting, // Initial start
		supervisor.StatusRunning,  // First running state
		supervisor.StatusRunning,  // Topology details received
		supervisor.StatusStopping, // First stop
		supervisor.StatusStopped,  // Stopped state
		supervisor.StatusStarting, // Restart
		supervisor.StatusRunning,  // Second running state
		supervisor.StatusRunning,  // Topology details received
		supervisor.StatusStopping, // Final stop
		supervisor.StatusStopped,  // Final state
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
		details any
		status  supervisor.Status
	}, 0)
	var statesMutex sync.Mutex

	mock := &mockService{
		startFunc: func(context.Context) (<-chan any, error) {
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
				details any
				status  supervisor.Status
			}{details, status})
			statesMutex.Unlock()
		},
	)

	// Launch the service
	if err := ctr.Start(); err != nil {
		t.Fatalf("Failed to start service: %v", err)
	}

	// wait for service to be running
	time.Sleep(100 * time.Millisecond)
	if state := ctr.State(); state.Status != supervisor.StatusRunning {
		t.Fatalf("Expected service to be Running, got %v", state.Status)
	}

	// Launch shutdown in a goroutine
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
		details any
		status  supervisor.Status
	}, len(stateTransitions))
	copy(transitions, stateTransitions)
	statesMutex.Unlock()

	expectedTransitions := []struct {
		details any
		status  supervisor.Status
	}{
		{"attempt 1", supervisor.StatusStarting},
		{nil, supervisor.StatusRunning},
		{"service running", supervisor.StatusRunning},
		{nil, supervisor.StatusStopping},
		{"cleaning up resources", supervisor.StatusStopping},
		{"closing connections", supervisor.StatusStopping},
		{nil, supervisor.StatusStopped},
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
		startFunc: func(context.Context) (<-chan any, error) {
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

	// Launch the service
	if err := ctr.Start(); err != nil {
		t.Fatalf("Failed to start service: %v", err)
	}

	// wait for service to be running
	time.Sleep(100 * time.Millisecond)

	// Launch shutdown in a goroutine
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

	// Verify service ended up in a stopped state
	finalState := ctr.State()
	if finalState.Status != supervisor.StatusFailed {
		t.Errorf("Expected final state to be Failed (since context was canceled), got %v", finalState.Status)
	}
}

func TestController_StopDuringFailedStart(t *testing.T) {
	var startCalled int32                    // atomic counter
	startAttempted := make(chan struct{}, 1) // buffered channel for signaling
	startFinished := make(chan struct{})

	mock := &mockService{
		startFunc: func(ctx context.Context) (<-chan any, error) {
			// Signal that we're in start (a buffered channel won't block)
			select {
			case startAttempted <- struct{}{}:
			default:
			}

			// Block until context is canceled or long timeout
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(10 * time.Second):
				return nil, errors.New("fake timeout")
			}
		},
		stopFunc: func(context.Context) error {
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
		func(_ supervisor.Status, _ any) {
		},
	)

	// Launch in a goroutine since it will block
	startErr := make(chan error, 1)
	go func() {
		err := ctr.Start()
		startErr <- err
	}()

	// Wait for service to enter start
	select {
	case <-startAttempted:
	case <-time.After(time.Second):
		t.Fatal("Controller did not enter start within timeout")
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
	if state.Status != supervisor.StatusFailed {
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
		stopFunc: func(context.Context) error {
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
		func(status supervisor.Status, _ any) {
			statesMutex.Lock()
			defer statesMutex.Unlock()
			stateTransitions = append(stateTransitions, status)
		},
	)

	// Launch the service
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
		t.Fatal("Controller never attempted to start")
	}

	// Verify state transitions
	statesMutex.Lock()
	transitions := make([]supervisor.Status, len(stateTransitions))
	copy(transitions, stateTransitions)
	statesMutex.Unlock()

	expectedTransitions := []supervisor.Status{
		supervisor.StatusStarting,
		supervisor.StatusFailed,
	}

	if !reflect.DeepEqual(transitions, expectedTransitions) {
		t.Errorf("Expected state transitions %v, got %v", expectedTransitions, transitions)
	}

	// Verify final state
	state := ctr.State()
	if state.Status != supervisor.StatusFailed {
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
		startFunc: func(context.Context) (<-chan any, error) {
			// Return ErrExit directly from the start
			return nil, supervisor.ErrExit
		},
		stopFunc: func(context.Context) error {
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
		func(status supervisor.Status, _ any) {
			statesMutex.Lock()
			defer statesMutex.Unlock()
			stateTransitions = append(stateTransitions, status)
		},
	)

	// Launch the service
	err := ctr.Start()

	// Should get ErrExit
	if !errors.Is(err, supervisor.ErrExit) {
		t.Fatalf("Expected supervisor.ErrExit, got: %v", err)
	}

	// Spawn final state transitions
	statesMutex.Lock()
	transitions := make([]supervisor.Status, len(stateTransitions))
	copy(transitions, stateTransitions)
	statesMutex.Unlock()

	// Expected state transition sequence
	expectedTransitions := []supervisor.Status{
		supervisor.StatusStarting,
		supervisor.StatusExited, // Should go directly to Exited on ErrExit
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
	if state.Status != supervisor.StatusExited {
		t.Errorf("Expected final status Exited, got: %v", state.Status)
	}
}

func TestController_ServiceExitDuringOperation(t *testing.T) {
	detailsCh := make(chan any, 1)
	stateTransitions := make([]supervisor.Status, 0)
	var statesMutex sync.Mutex

	mock := &mockService{
		startFunc: func(context.Context) (<-chan any, error) {
			return detailsCh, nil
		},
		stopFunc: func(context.Context) error {
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
		func(status supervisor.Status, _ any) {
			statesMutex.Lock()
			defer statesMutex.Unlock()
			stateTransitions = append(stateTransitions, status)
		},
	)

	// Launch the service
	err := ctr.Start()
	if err != nil {
		t.Fatalf("Failed to start service: %v", err)
	}

	// send ErrExit through details channel
	detailsCh <- supervisor.ErrExit

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Spawn final state transitions
	statesMutex.Lock()
	transitions := make([]supervisor.Status, len(stateTransitions))
	copy(transitions, stateTransitions)
	statesMutex.Unlock()

	// Expected state transition sequence
	expectedTransitions := []supervisor.Status{
		supervisor.StatusStarting,
		supervisor.StatusRunning,
		supervisor.StatusExited, // Should transition to Exited on ErrExit
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
	if state.Status != supervisor.StatusExited {
		t.Errorf("Expected final status Exited, got: %v", state.Status)
	}
}

func TestController_StressTestStartLast(t *testing.T) {
	var currentChan chan any
	var chanMutex sync.Mutex
	startAttempts := atomic.Int32{}
	stopAttempts := atomic.Int32{}

	mock := &mockService{
		startFunc: func(context.Context) (<-chan any, error) {
			startAttempts.Add(1)
			chanMutex.Lock()
			currentChan = make(chan any, 1)
			ch := currentChan
			ch <- "service started"
			chanMutex.Unlock()

			return ch, nil
		},
		stopFunc: func(context.Context) error {
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
			// use crypto package instead of weak math or math/v2
			b, err := rand.Int(rand.Reader, big.NewInt(10))
			if err != nil {
				return
			}
			if b.Int64() < 1 {
				_ = ctr.Start()
			} else {
				_ = ctr.Stop()
			}
		}()
	}

	// wait for the batch to complete
	wg.Wait()

	// The last operation is always Launch
	_ = ctr.Start()

	// Verify the final state is Running
	state := ctr.State()
	if state.Status != supervisor.StatusRunning {
		t.Errorf("Expected final status Running, got: %v", state.Status)
	}

	// Cleanup
	if err := ctr.Stop(); err != nil {
		t.Errorf("Failed to stop controller: %v", err)
	}
}

func TestController_RetryDelay(t *testing.T) {
	var mu sync.Mutex
	var startTimes []time.Time
	mock := &mockService{
		startFunc: func(_ context.Context) (<-chan any, error) {
			mu.Lock()
			startTimes = append(startTimes, time.Now())
			mu.Unlock()
			// Always fail immediately to trigger retries.
			return nil, errors.New("startup error")
		},
		stopFunc: func(_ context.Context) error {
			return nil
		},
	}
	config := supervisor.LifecycleConfig{
		StartTimeout:    500 * time.Millisecond,
		StopTimeout:     500 * time.Millisecond,
		StableThreshold: 50 * time.Millisecond, // ensure service is considered unstable so retry count is preserved
		RetryPolicy: supervisor.RetryPolicy{
			MaxAttempts:  3,
			InitialDelay: 200 * time.Millisecond,
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ctr := NewController(ctx, mock, config, func(_ supervisor.Status, _ any) {})
	err := ctr.Start()
	if err == nil {
		t.Fatal("Expected error from Serve() due to immediate failure, got nil")
	}
	// Wait for retries to occur with context timeout to prevent test hanging
	retryCtx, retryCancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer retryCancel()

	select {
	case <-retryCtx.Done():
		t.Log("Timeout waiting for retries")
	case <-time.After(500 * time.Millisecond):
		// Wait for retries to occur
	}
	mu.Lock()
	times := append([]time.Time(nil), startTimes...)
	mu.Unlock()
	if len(times) < 2 {
		t.Fatal("Expected at least two start attempts")
	}
	delay := times[1].Sub(times[0])
	if delay < 200*time.Millisecond {
		t.Errorf("Expected delay of at least 200ms between start attempts, got %v", delay)
	}
}

func TestController_StopCancelsPendingStartRetry(t *testing.T) {
	var attempts atomic.Int32
	attemptCh := make(chan int32, 10)

	mock := &mockService{
		startFunc: func(_ context.Context) (<-chan any, error) {
			attempt := attempts.Add(1)
			attemptCh <- attempt
			return nil, errors.New("startup error")
		},
		stopFunc: func(_ context.Context) error {
			return nil
		},
	}

	ctr := NewController(
		context.Background(),
		mock,
		supervisor.LifecycleConfig{
			StartTimeout:    200 * time.Millisecond,
			StopTimeout:     200 * time.Millisecond,
			StableThreshold: time.Hour,
			RetryPolicy: supervisor.RetryPolicy{
				MaxAttempts:  0,
				InitialDelay: 75 * time.Millisecond,
				MaxDelay:     75 * time.Millisecond,
			},
		},
		func(_ supervisor.Status, _ any) {},
	)

	startErr := make(chan error, 1)
	go func() {
		startErr <- ctr.Start()
	}()

	select {
	case <-attemptCh:
	case <-time.After(time.Second):
		t.Fatal("controller never attempted to start")
	}

	if err := ctr.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	select {
	case err := <-startErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected Start to unblock with context.Canceled, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Start did not unblock after Stop")
	}

	time.Sleep(200 * time.Millisecond)

	if got := attempts.Load(); got != 1 {
		t.Fatalf("expected no retry after Stop, got %d start attempts", got)
	}
}

func TestController_CancelStartUnblocksInProgressStart(t *testing.T) {
	startEntered := make(chan struct{})
	startCanceled := make(chan struct{})
	var startOnce sync.Once
	var cancelOnce sync.Once
	var stopCalled atomic.Bool

	mock := &mockService{
		startFunc: func(ctx context.Context) (<-chan any, error) {
			startOnce.Do(func() { close(startEntered) })
			select {
			case <-ctx.Done():
				cancelOnce.Do(func() { close(startCanceled) })
				return nil, ctx.Err()
			case <-time.After(10 * time.Second):
				return nil, errors.New("start was not canceled")
			}
		},
		stopFunc: func(_ context.Context) error {
			stopCalled.Store(true)
			return nil
		},
	}

	ctr := NewController(
		context.Background(),
		mock,
		supervisor.LifecycleConfig{
			StartTimeout: 10 * time.Second,
			StopTimeout:  200 * time.Millisecond,
			RetryPolicy: supervisor.RetryPolicy{
				MaxAttempts: 1,
			},
		},
		func(_ supervisor.Status, _ any) {},
	)
	defer ctr.cancel()

	startErr := make(chan error, 1)
	go func() {
		startErr <- ctr.Start()
	}()

	select {
	case <-startEntered:
	case <-time.After(time.Second):
		t.Fatal("controller never entered Start")
	}

	deadline := time.Now().Add(time.Second)
	for ctr.State().Status != supervisor.StatusStarting {
		if time.Now().After(deadline) {
			t.Fatalf("expected controller status Starting, got %s", ctr.State().Status)
		}
		time.Sleep(10 * time.Millisecond)
	}

	ctr.cancelStart()

	select {
	case <-startCanceled:
	case <-time.After(time.Second):
		t.Fatal("in-progress Start context was not canceled")
	}

	select {
	case err := <-startErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected Start to return context.Canceled, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Start did not unblock after cancelStart")
	}

	if stopCalled.Load() {
		t.Fatal("cancelStart should not call service Stop directly")
	}
}
