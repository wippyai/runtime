package supervisor

import (
	"context"
	"errors"
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/supervisor"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockService implements supervisor.Service for testing
type mockService struct {
	startFunc func(context.Context) (<-chan payload.Payload, error)
	stopFunc  func(context.Context) error
}

func (m *mockService) Start(ctx context.Context) (<-chan payload.Payload, error) {
	return m.startFunc(ctx)
}

func (m *mockService) Stop(ctx context.Context) error {
	return m.stopFunc(ctx)
}

func TestSupervisor_BasicLifecycle(t *testing.T) {
	detailsCh := make(chan payload.Payload, 1)
	var receivedStates []struct {
		status  supervisor.Status
		details payload.Payload
	}
	var statesMutex sync.Mutex

	mock := &mockService{
		startFunc: func(ctx context.Context) (<-chan payload.Payload, error) {
			time.Sleep(100 * time.Millisecond)
			detailsCh <- payload.NewString("service running")
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

	config := supervisor.ServiceConfig{
		StartTimeout: 5 * time.Second,
		StopTimeout:  5 * time.Second,
		RetryPolicy: supervisor.RetryPolicy{
			MaxAttempts: 3,
		},
		ForceShutdown: true,
	}

	sup := NewSupervisor(
		context.Background(),
		mock,
		config,
		func(status supervisor.Status, details payload.Payload) {
			statesMutex.Lock()
			receivedStates = append(receivedStates, struct {
				status  supervisor.Status
				details payload.Payload
			}{status, details})
			statesMutex.Unlock()
		},
	)

	// Test initial state
	if state := sup.state.getSnapshot(); state.status != supervisor.Unknown {
		t.Errorf("Expected initial status Unknown, got %v", state.status)
	}

	// Test transition to Running
	if err := sup.TransitionTo(supervisor.Running); err != nil {
		t.Fatalf("Failed to transition to Running: %v", err)
	}

	if state := sup.state.getSnapshot(); state.status != supervisor.Running {
		t.Errorf("Expected status Running, got %v", state.status)
	}

	time.Sleep(100 * time.Millisecond) // wait for service details to propagate

	// Test transition to Stopped
	if err := sup.TransitionTo(supervisor.Stopped); err != nil {
		t.Fatalf("Failed to transition to Stopped: %v", err)
	}

	if state := sup.state.getSnapshot(); state.status != supervisor.Stopped {
		t.Errorf("Expected status Stopped, got %v", state.status)
	}

	// Stop supervisor
	if err := sup.Stop(); err != nil {
		t.Fatalf("Failed to stop supervisor: %v", err)
	}

	// Get final states safely
	statesMutex.Lock()
	finalStates := make([]struct {
		status  supervisor.Status
		details payload.Payload
	}, len(receivedStates))
	copy(finalStates, receivedStates)
	statesMutex.Unlock()

	expectedStates := []supervisor.Status{
		supervisor.Starting,
		supervisor.Running,
		supervisor.Running, // updated by service
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

func TestSupervisor_ServiceFailure(t *testing.T) {
	detailsCh := make(chan payload.Payload)
	attempts := 0
	stateReached := make(chan struct{}, 1)

	mock := &mockService{
		startFunc: func(ctx context.Context) (<-chan payload.Payload, error) {
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

	config := supervisor.ServiceConfig{
		StartTimeout: 5 * time.Second,
		StopTimeout:  5 * time.Second,
		RetryPolicy: supervisor.RetryPolicy{
			MaxAttempts:  3,
			InitialDelay: 100 * time.Millisecond,
		},
		ForceShutdown: true,
	}

	sup := NewSupervisor(
		context.Background(),
		mock,
		config,
		func(status supervisor.Status, details payload.Payload) {
			if status == supervisor.Running {
				select {
				case stateReached <- struct{}{}:
				default:
				}
			}
		},
	)

	// Start the service
	if err := sup.TransitionTo(supervisor.Running); err != nil {
		t.Fatalf("Failed to transition to Running: %v", err)
	}

	// Wait for service to reach Running state after recovery
	<-stateReached

	if state := sup.state.getSnapshot(); state.status != supervisor.Running {
		t.Errorf("Expected status Running after recovery, got %v", state.status)
	}

	if attempts != 2 {
		t.Errorf("Expected 2 start attempts, got %d", attempts)
	}

	if err := sup.Stop(); err != nil {
		t.Fatalf("Failed to stop supervisor: %v", err)
	}
}

func TestSupervisor_StartupError(t *testing.T) {
	stateReached := make(chan struct{}, 1)
	expectedErr := errors.New("startup failed")

	mock := &mockService{
		startFunc: func(ctx context.Context) (<-chan payload.Payload, error) {
			return nil, expectedErr
		},
		stopFunc: func(ctx context.Context) error {
			return nil
		},
	}

	sup := NewSupervisor(
		context.Background(),
		mock,
		supervisor.ServiceConfig{
			StartTimeout: time.Second,
			RetryPolicy:  supervisor.RetryPolicy{MaxAttempts: 1},
		},
		func(status supervisor.Status, details payload.Payload) {
			if status == supervisor.Failed {
				select {
				case stateReached <- struct{}{}:
				default:
				}
			}
		},
	)

	err := sup.TransitionTo(supervisor.Running)
	if err == nil {
		t.Fatal("Expected error on startup, got nil")
	}

	<-stateReached

	state := sup.state.getSnapshot()
	if state.status != supervisor.Failed {
		t.Errorf("Expected Failed status, got %v", state.status)
	}
}

func TestSupervisor_ForceShutdown(t *testing.T) {
	stateReached := make(chan struct{}, 1)
	stopErr := errors.New("failed to stop gracefully")

	mock := &mockService{
		startFunc: func(ctx context.Context) (<-chan payload.Payload, error) {
			ch := make(chan payload.Payload, 1)
			ch <- payload.NewString("running")
			return ch, nil
		},
		stopFunc: func(ctx context.Context) error {
			return stopErr
		},
	}

	sup := NewSupervisor(
		context.Background(),
		mock,
		supervisor.ServiceConfig{
			StartTimeout:  time.Second,
			StopTimeout:   time.Second,
			ForceShutdown: true,
			RetryPolicy:   supervisor.RetryPolicy{MaxAttempts: 1},
		},
		func(status supervisor.Status, details payload.Payload) {
			if status == supervisor.Stopped {
				select {
				case stateReached <- struct{}{}:
				default:
				}
			}
		},
	)

	if err := sup.TransitionTo(supervisor.Running); err != nil {
		t.Fatalf("Failed to start service: %v", err)
	}

	if err := sup.TransitionTo(supervisor.Stopped); err != nil {
		t.Fatalf("Failed to stop service: %v", err)
	}

	<-stateReached

	state := sup.state.getSnapshot()
	if state.status != supervisor.Stopped {
		t.Errorf("Expected Stopped status after force shutdown, got %v", state.status)
	}
}
func TestSupervisor_ContextCancellation(t *testing.T) {
	detailsCh := make(chan payload.Payload)
	serviceStarted := make(chan struct{})
	serviceStopped := make(chan struct{})

	mock := &mockService{
		startFunc: func(ctx context.Context) (<-chan payload.Payload, error) {
			defer close(serviceStarted)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
				return detailsCh, nil
			}
		},
		stopFunc: func(ctx context.Context) error {
			defer close(serviceStopped)
			close(detailsCh)
			return nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	sup := NewSupervisor(
		ctx,
		mock,
		supervisor.ServiceConfig{
			StartTimeout: time.Second,
			StopTimeout:  time.Second,
			RetryPolicy:  supervisor.RetryPolicy{MaxAttempts: 3},
		},
		nil,
	)

	// Start the service
	if err := sup.Start(); err != nil {
		t.Fatalf("Failed to start service: %v", err)
	}

	<-serviceStarted // Wait for service to start

	// Cancel context
	cancel()

	// Wait for service to stop
	select {
	case <-serviceStopped:
		// Expected behavior
	case <-time.After(2 * time.Second):
		t.Fatal("Service did not stop after context cancellation")
	}

	state := sup.state.getSnapshot()
	if state.status != supervisor.Stopped {
		t.Errorf("Expected status Stopped after context cancellation, got %v", state.status)
	}
}

func TestSupervisor_StartTimeout(t *testing.T) {
	mock := &mockService{
		startFunc: func(ctx context.Context) (<-chan payload.Payload, error) {
			// Simulate a slow start that should timeout
			time.Sleep(2 * time.Second)
			return make(chan payload.Payload), nil
		},
		stopFunc: func(ctx context.Context) error {
			return nil
		},
	}

	sup := NewSupervisor(
		context.Background(),
		mock,
		supervisor.ServiceConfig{
			StartTimeout: 100 * time.Millisecond, // Short timeout
			StopTimeout:  time.Second,
			RetryPolicy:  supervisor.RetryPolicy{MaxAttempts: 1},
		},
		nil,
	)

	err := sup.Start()
	if err == nil {
		t.Fatal("Expected timeout error, got nil")
	}

	state := sup.state.getSnapshot()
	if state.status != supervisor.Failed {
		t.Errorf("Expected Failed status after timeout, got %v", state.status)
	}
}

func TestSupervisor_ServiceRecoveryAfterFailure(t *testing.T) {
	var currentChan chan payload.Payload
	var chanMutex sync.Mutex
	stateTransitions := make([]supervisor.Status, 0)
	var statesMutex sync.Mutex

	mock := &mockService{
		startFunc: func(ctx context.Context) (<-chan payload.Payload, error) {
			chanMutex.Lock()
			// Create a new channel each time the service starts
			currentChan = make(chan payload.Payload, 1)
			ch := currentChan // local copy to return
			chanMutex.Unlock()

			// Simulate service startup message
			ch <- payload.NewString("service started")

			return ch, nil
		},
		stopFunc: func(ctx context.Context) error {
			return nil
		},
	}

	config := supervisor.ServiceConfig{
		StartTimeout: 5 * time.Second,
		StopTimeout:  5 * time.Second,
		RetryPolicy: supervisor.RetryPolicy{
			MaxAttempts:  3,
			InitialDelay: 100 * time.Millisecond,
		},
		ForceShutdown: true,
	}

	sup := NewSupervisor(
		context.Background(),
		mock,
		config,
		func(status supervisor.Status, details payload.Payload) {
			statesMutex.Lock()
			stateTransitions = append(stateTransitions, status)
			statesMutex.Unlock()
		},
	)

	// Start the service
	if err := sup.Start(); err != nil {
		t.Fatalf("Failed to start service: %v", err)
	}

	// Wait for initial startup and first status update
	time.Sleep(200 * time.Millisecond)

	// Verify service is running
	state := sup.state.getSnapshot()
	if state.status != supervisor.Running {
		t.Fatalf("Expected service to be Running, got %v", state.status)
	}

	// Simulate service death by closing the current channel
	chanMutex.Lock()
	if currentChan != nil {
		close(currentChan)
	}
	chanMutex.Unlock()

	// Wait for recovery process
	time.Sleep(500 * time.Millisecond)

	// Verify service recovered
	state = sup.state.getSnapshot()
	if state.status != supervisor.Running {
		t.Fatalf("Expected service to be Running after recovery, got %v", state.status)
	}

	// Stop the supervisor
	if err := sup.Stop(); err != nil {
		t.Fatalf("Failed to stop supervisor: %v", err)
	}

	// Get final state transitions
	statesMutex.Lock()
	transitions := make([]supervisor.Status, len(stateTransitions))
	copy(transitions, stateTransitions)
	statesMutex.Unlock()

	// Verify the state transition sequence
	expectedTransitions := []supervisor.Status{
		supervisor.Starting, // Initial start
		supervisor.Running,  // First successful start
		supervisor.Running,  // Service details received
		supervisor.Failed,   // Service death
		supervisor.Starting, // Recovery attempt
		supervisor.Running,  // Recovery successful
		supervisor.Running,  // Service details received after recovery
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

func TestSupervisor_ServiceFailedRecovery(t *testing.T) {
	var currentChan chan payload.Payload
	var chanMutex sync.Mutex
	stateTransitions := make([]supervisor.Status, 0)
	var statesMutex sync.Mutex
	attempts := 0
	maxRetries := 2
	stateChan := make(chan struct{})
	var once sync.Once

	mock := &mockService{
		startFunc: func(ctx context.Context) (<-chan payload.Payload, error) {
			attempts++
			chanMutex.Lock()
			defer chanMutex.Unlock()

			if attempts == 1 {
				// First attempt succeeds
				currentChan = make(chan payload.Payload, 1)
				currentChan <- payload.NewString("service started")
				return currentChan, nil
			}

			// All subsequent attempts fail immediately
			return nil, fmt.Errorf("start attempt %d failed", attempts)
		},
		stopFunc: func(ctx context.Context) error {
			return nil
		},
	}

	config := supervisor.ServiceConfig{
		StartTimeout: 1 * time.Second,
		StopTimeout:  1 * time.Second,
		RetryPolicy: supervisor.RetryPolicy{
			MaxAttempts:  maxRetries,
			InitialDelay: 100 * time.Millisecond,
		},
		ForceShutdown: true,
	}

	ctx, cancel := context.WithCancel(context.Background())

	sup := NewSupervisor(
		ctx,
		mock,
		config,
		func(status supervisor.Status, details payload.Payload) {
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
	if err := sup.Start(); err != nil {
		cancel()
		t.Fatalf("Failed to start service: %v", err)
	}

	// Wait for initial startup and details
	time.Sleep(100 * time.Millisecond)

	// Verify service is initially running
	state := sup.state.getSnapshot()
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

	// Wait for service to reach final failed state
	select {
	case <-stateChan:
		// Service reached final failed state
	case <-time.After(time.Second):
		cancel()
		t.Fatal("Timeout waiting for service to reach final failed state")
	}

	// Get state transitions before cleanup
	statesMutex.Lock()
	transitions := make([]supervisor.Status, len(stateTransitions))
	copy(transitions, stateTransitions)
	statesMutex.Unlock()

	if err := sup.Stop(); err != nil {
		t.Logf("Error during supervisor shutdown: %v", err)
	}

	// Verify the complete state transition sequence
	expectedTransitions := []supervisor.Status{
		supervisor.Starting, // Initial start
		supervisor.Running,  // First successful start
		supervisor.Running,  // Service details received
		supervisor.Failed,   // Service death
		supervisor.Starting, // First recovery attempt
		supervisor.Failed,   // First recovery failure
		supervisor.Starting, // Second recovery attempt
		supervisor.Failed,   // Final failure
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

	// Verify retry count
	expectedAttempts := maxRetries + 1 // Initial attempt + maxRetries
	if attempts != expectedAttempts {
		t.Errorf("Expected %d total attempts, got %d", expectedAttempts, attempts)
	}
}

func TestSupervisor_ContextHandling(t *testing.T) {
	state := newServiceState()

	// Test initial context state
	if state.getContext() != nil {
		t.Error("Expected nil initial context")
	}

	// Test setting context
	ctx, cancel := context.WithCancel(context.Background())
	state.setContext(ctx, cancel)

	if state.getContext() != ctx {
		t.Error("Context not set correctly")
	}

	// Test cancelling context
	state.cancelContext()
	select {
	case <-ctx.Done():
		// Expected behavior
	default:
		t.Error("Context not cancelled")
	}

	// Test setting new context after cancellation
	newCtx, newCancel := context.WithCancel(context.Background())
	state.setContext(newCtx, newCancel)

	if state.getContext() != newCtx {
		t.Error("New context not set correctly")
	}
}

func TestSupervisor_InvalidTransitions(t *testing.T) {
	mock := &mockService{
		startFunc: func(ctx context.Context) (<-chan payload.Payload, error) {
			return make(chan payload.Payload), nil
		},
		stopFunc: func(ctx context.Context) error {
			return nil
		},
	}

	sup := NewSupervisor(
		context.Background(),
		mock,
		supervisor.ServiceConfig{
			StartTimeout: time.Second,
			StopTimeout:  time.Second,
		},
		nil,
	)

	invalidStates := []supervisor.Status{
		supervisor.Failed,
		supervisor.Starting,
		supervisor.Stopping,
		supervisor.Status("InvalidStatus"),
	}

	for _, state := range invalidStates {
		t.Run(string(state), func(t *testing.T) {
			err := sup.TransitionTo(state)
			if err == nil {
				t.Errorf("Expected error when transitioning to %v", state)
			}
		})
	}
}

func TestSupervisor_ServiceStateSnapshot(t *testing.T) {
	state := newServiceState()
	state.status = supervisor.Running
	state.desired = supervisor.Running
	state.retryCount = 5
	state.lastUpdate = time.Now()
	state.details = payload.NewString("test details")

	snapshot := state.getSnapshot()

	if snapshot.status != state.status {
		t.Errorf("Status mismatch: expected %v, got %v", state.status, snapshot.status)
	}
	if snapshot.desired != state.desired {
		t.Errorf("Desired status mismatch: expected %v, got %v", state.desired, snapshot.desired)
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

func TestSupervisor_ServiceDetailsUpdate(t *testing.T) {
	state := newServiceState()
	initialStatus := supervisor.Running
	state.status = initialStatus

	testCases := []struct {
		name        string
		details     payload.Payload
		wantStatus  supervisor.Status
		wantDetails payload.Payload
	}{
		{
			name:        "update with string payload",
			details:     payload.NewString("test details"),
			wantStatus:  initialStatus,
			wantDetails: payload.NewString("test details"),
		},
		{
			name:        "update with error payload",
			details:     payload.NewError(errors.New("test error")),
			wantStatus:  initialStatus,
			wantDetails: payload.NewError(errors.New("test error")),
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
				t.Errorf("State details = %v, want %v", state.details, tc.wantDetails)
			}

			if time.Since(state.lastUpdate) > time.Second {
				t.Error("Last update timestamp not updated")
			}
		})
	}
}

func TestSupervisor_CancelDuringTransition(t *testing.T) {
	// Channel to block the first transition
	blockChan := make(chan struct{})
	transitionStarted := make(chan struct{})

	mock := &mockService{
		startFunc: func(ctx context.Context) (<-chan payload.Payload, error) {
			// First call blocks until we're ready to proceed
			select {
			case <-blockChan:
				return make(chan payload.Payload), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
		stopFunc: func(ctx context.Context) error {
			return nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	sup := NewSupervisor(
		ctx,
		mock,
		supervisor.ServiceConfig{
			StartTimeout: 5 * time.Second,
			StopTimeout:  1 * time.Second,
		},
		nil,
	)

	// Start first transition that will block in handleTransition
	go func() {
		sup.TransitionTo(supervisor.Running)
		close(transitionStarted)
	}()

	// Give time for first transition to be processed
	time.Sleep(100 * time.Millisecond)

	// Start second transition that will block on transitions channel
	errChan := make(chan error, 1)
	go func() {
		errChan <- sup.TransitionTo(supervisor.Running)
	}()

	// Give time for second transition to block on channel
	time.Sleep(100 * time.Millisecond)

	// Cancel context
	cancel()

	// Wait a bit to ensure cancellation is processed
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
		t.Fatal("Timeout waiting for transition error")
	}
}
