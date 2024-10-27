package component

import (
	"context"
	"github.com/ponyruntime/pony/api"
	"github.com/ponyruntime/pony/api/payload"
	ebs "github.com/ponyruntime/pony/component/eventbus"
	"github.com/ponyruntime/pony/exec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
	"testing"
)

// MockSubsystemServer is a mock implementation of component.Component
type MockSubsystemServer struct {
	mock.Mock
}

func (m *MockSubsystemServer) Register(ctx context.Context, event api.Event, state any) (any, error) {
	args := m.Called(ctx, event, state)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(any), args.Error(1)
}

func (m *MockSubsystemServer) Commit(ctx context.Context, state any) {
	m.Called(ctx, state)
}

func (m *MockSubsystemServer) Start(ctx context.Context, q *exec.Queue) {
}

func (m *MockSubsystemServer) Stop(ctx context.Context) {
}

// TestHubListenEvents tests the event listening functionality
func TestUpdateState(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	queue := exec.NewQueue()
	mockServer := &MockSubsystemServer{}

	subsystems := []Declaration{
		{
			ID:        "test",
			Component: mockServer,
		},
	}

	hub := NewHub(logger, queue, subsystems...)
	defer hub.Close()

	// Setup mock expectations
	newState := struct {
		State string
	}{
		State: "new-state",
	}

	mockServer.On("Register",
		mock.MatchedBy(func(ctx context.Context) bool {
			return true
		}),
		mock.MatchedBy(func(event api.Event) bool {
			return event.Kind() == "test"
		}),
		nil,
	).Return(newState, nil)

	// Start listening
	hub.ListenEvents()

	sub := ebs.NewSubscriber()
	defer sub.Close()

	// begin operation
	hub.eb.Send(context.Background(), ebs.NewEvent(api.Transaction, api.EventBegin, nil))

	// Send test event
	hub.eb.Send(
		context.Background(),
		ebs.NewEvent(
			"test",
			"test",
			payload.NewString("configure-server"),
		),
	)

	sub.Wait(api.Transaction, api.EventRegisterChange)

	mockServer.AssertExpectations(t)
	assert.Equal(t, newState, hub.sm.states["test"].State)
}

// TestHubListenEvents tests the event listening functionality
func TestStateRollback(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	queue := exec.NewQueue()
	mockServer := &MockSubsystemServer{}

	subsystems := []Declaration{
		{
			ID:        "test",
			Component: mockServer,
		},
	}

	hub := NewHub(logger, queue, subsystems...)
	defer hub.Close()

	// Setup mock expectations
	newState := struct {
		State string
	}{
		State: "new-state",
	}

	mockServer.On("Register",
		mock.MatchedBy(func(ctx context.Context) bool {
			return true
		}),
		mock.MatchedBy(func(event api.Event) bool {
			return event.Kind() == "test"
		}),
		nil,
	).Return(newState, nil)
	mockServer.AssertNotCalled(t, "Commit")

	// Start listening
	hub.ListenEvents()

	sub := ebs.NewSubscriber()
	defer sub.Close()

	// begin operation
	hub.eb.Send(context.Background(), ebs.NewEvent(api.Transaction, api.EventBegin, nil))

	// Send test event
	hub.eb.Send(
		context.Background(),
		ebs.NewEvent(
			"test",
			"test",
			payload.NewString("configure-server"),
		),
	)

	sub.Wait(api.Transaction, api.EventRegisterChange)

	hub.eb.Send(context.Background(), ebs.NewEvent(api.Transaction, api.EventRollback, nil))

	mockServer.AssertExpectations(t)
}

// TestHubListenEvents tests the event listening functionality
func TestStatePropagateState(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	queue := exec.NewQueue()
	mockServer := &MockSubsystemServer{}

	subsystems := []Declaration{
		{
			ID:        "test",
			Component: mockServer,
		},
	}

	hub := NewHub(logger, queue, subsystems...)
	defer hub.Close()

	// Setup mock expectations
	newState := struct {
		State string
	}{
		State: "new-state",
	}

	mockServer.On("Register",
		mock.MatchedBy(func(ctx context.Context) bool {
			return true
		}),
		mock.MatchedBy(func(event api.Event) bool {
			return event.Kind() == "test"
		}),
		nil,
	).Return(newState, nil)

	finalState := struct {
		State string
	}{
		State: "final-state",
	}
	mockServer.On("Register",
		mock.MatchedBy(func(ctx context.Context) bool {
			return true
		}),
		mock.MatchedBy(func(event api.Event) bool {
			return event.Kind() == "test"
		}),
		newState,
	).Return(finalState, nil)
	mockServer.On("Commit",
		mock.MatchedBy(func(ctx context.Context) bool {
			return true
		}),
		finalState,
	)

	// Start listening
	hub.ListenEvents()

	sub := ebs.NewSubscriber()
	defer sub.Close()

	// begin operation
	hub.eb.Send(context.Background(), ebs.NewEvent(api.Transaction, api.EventBegin, nil))

	// Send test event
	hub.eb.Send(
		context.Background(),
		ebs.NewEvent(
			"test",
			"test",
			payload.NewString("configure-server"),
		),
	)

	sub.Wait(api.Transaction, api.EventRegisterChange)

	hub.eb.Send(
		context.Background(),
		ebs.NewEvent(
			"test",
			"test",
			payload.NewString("configure-server-2"),
		),
	)

	sub.Wait(api.Transaction, api.EventRegisterChange)

	hub.eb.Send(context.Background(), ebs.NewEvent(api.Transaction, api.EventCommit, nil))

	e := sub.Wait(api.Transaction, api.EventRegisterCommit)
	assert.Equal(t, finalState, e.Payload().Data().(State).State)

	mockServer.AssertExpectations(t)
}
