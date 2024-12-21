package supervisor

import (
	"context"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/supervisor"
	"sync"
	"testing"
	"time"
)

// mockService implements supervisor.Service for testing
type mockService struct {
	startFunc func(context.Context) (<-chan supervisor.ServiceState, error)
	stopFunc  func(context.Context) error
}

func (m *mockService) Start(ctx context.Context) (<-chan supervisor.ServiceState, error) {
	return m.startFunc(ctx)
}

func (m *mockService) Stop(ctx context.Context) error {
	return m.stopFunc(ctx)
}

func TestSupervisor_BasicLifecycle(t *testing.T) {
	statusCh := make(chan supervisor.ServiceState, 1)
	var receivedStates []supervisor.ServiceState
	var statesMutex sync.Mutex
	stateReached := make(chan struct{}, 2) // Buffered channel to avoid missing signals

	// Create mock service
	mock := &mockService{
		startFunc: func(ctx context.Context) (<-chan supervisor.ServiceState, error) {
			statusCh <- supervisor.ServiceState{Status: supervisor.Running}
			return statusCh, nil
		},
		stopFunc: func(ctx context.Context) error {
			close(statusCh)
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

	// Create supervisor with status change callback
	sup := NewSupervisor(
		context.Background(),
		mock,
		config,
		func(state supervisor.ServiceState) {
			statesMutex.Lock()
			receivedStates = append(receivedStates, state)
			statesMutex.Unlock()

			if state.Status == supervisor.Running || state.Status == supervisor.Stopped {
				select {
				case stateReached <- struct{}{}:
				default:
					// Channel is full, which is fine
				}
			}
		},
	)

	// Test initial state
	if state := sup.GetState(); state.Status != supervisor.Unknown {
		t.Errorf("Expected initial status Unknown, got %v", state.Status)
	}

	// Test transition to Running
	if err := sup.TransitionTo(supervisor.Running); err != nil {
		t.Fatalf("Failed to transition to Running: %v", err)
	}

	// Wait for Running state
	<-stateReached

	if state := sup.GetState(); state.Status != supervisor.Running {
		t.Errorf("Expected status Running, got %v", state.Status)
	}

	// Test transition to Stopped
	if err := sup.TransitionTo(supervisor.Stopped); err != nil {
		t.Fatalf("Failed to transition to Stopped: %v", err)
	}

	// Wait for Stopped state
	<-stateReached

	if state := sup.GetState(); state.Status != supervisor.Stopped {
		t.Errorf("Expected status Stopped, got %v", state.Status)
	}

	// Stop supervisor
	if err := sup.Stop(); err != nil {
		t.Fatalf("Failed to stop supervisor: %v", err)
	}

	// Get final states safely
	statesMutex.Lock()
	finalStates := make([]supervisor.ServiceState, len(receivedStates))
	copy(finalStates, receivedStates)
	statesMutex.Unlock()

	// Verify state transitions
	expectedStates := []supervisor.Status{
		supervisor.Starting, // Initial transition to Running
		supervisor.Running,  // Service started
		supervisor.Stopping, // Transition to Stopped
		supervisor.Stopped,  // Service stopped
	}

	if len(finalStates) != len(expectedStates) {
		t.Errorf("Expected %d state transitions, got %d", len(expectedStates), len(finalStates))
		t.Logf("Received states: %v", finalStates)
		return
	}

	for i, expected := range expectedStates {
		if finalStates[i].Status != expected {
			t.Errorf("State transition %d: expected %v, got %v", i, expected, finalStates[i].Status)
		}
	}
}

func TestSupervisor_ServiceFailure(t *testing.T) {
	statusCh := make(chan supervisor.ServiceState)
	attempts := 0
	stateReached := make(chan struct{}, 1)

	// Create mock service that fails initially then succeeds
	mock := &mockService{
		startFunc: func(ctx context.Context) (<-chan supervisor.ServiceState, error) {
			attempts++
			if attempts == 1 {
				go func() {
					statusCh <- supervisor.ServiceState{
						Status:  supervisor.Failed,
						Details: payload.NewError(context.Canceled),
					}
				}()
			} else {
				go func() {
					statusCh <- supervisor.ServiceState{Status: supervisor.Running}
				}()
			}
			return statusCh, nil
		},
		stopFunc: func(ctx context.Context) error {
			close(statusCh)
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
		func(state supervisor.ServiceState) {
			if state.Status == supervisor.Running {
				select {
				case stateReached <- struct{}{}:
				default:
					// Channel is full, which is fine
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

	// Verify service recovered and is running
	if state := sup.GetState(); state.Status != supervisor.Running {
		t.Errorf("Expected status Running after recovery, got %v", state.Status)
	}

	// Verify retry attempts
	if attempts != 2 {
		t.Errorf("Expected 2 start attempts, got %d", attempts)
	}

	if err := sup.Stop(); err != nil {
		t.Fatalf("Failed to stop supervisor: %v", err)
	}
}
