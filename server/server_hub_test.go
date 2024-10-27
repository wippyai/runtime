package server

import (
	"context"
	"github.com/ponyruntime/pony/api"
	"github.com/ponyruntime/pony/component"
	eventsbus "github.com/ponyruntime/pony/eventbus"
	"github.com/ponyruntime/pony/exec"
	"github.com/ponyruntime/pony/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
	"testing"
)

// MockSubsystemServer is a mock implementation of component.Component
type MockSubsystemServer struct {
	mock.Mock
}

func (m *MockSubsystemServer) Handle(ctx context.Context, event api.Event, state any) (any, error) {
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
func TestStateChange(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	queue := exec.NewQueue()
	mockServer := &MockSubsystemServer{}

	subsystems := []component.Declaration{
		{
			ID:        "test",
			Component: mockServer,
		},
	}

	hub := NewHub(logger, queue, subsystems...)

	// Setup mock expectations
	newState := struct {
		State string
	}{
		State: "new-state",
	}

	mockServer.On("Handle",
		mock.MatchedBy(func(ctx context.Context) bool {
			return true
		}),
		mock.MatchedBy(func(event api.Event) bool {
			return event.Type() == "test"
		}),
		nil,
	).Return(newState, nil)

	// Start listening
	hub.ListenEvents()

	sub := eventsbus.NewSubscriber()
	defer sub.Close()

	// Send test event
	hub.eb.Send(
		context.Background(),
		eventsbus.NewEvent(
			"test",
			"test",
			payload.NewString("configure-server"),
		),
	)

	ev := sub.Wait(api.Transaction, api.EventStateChange)

	assert.Equal(t, api.EventStateChange, ev.Type())

	// Verify
	mockServer.AssertExpectations(t)
	assert.Equal(t, newState, hub.states["test"])
}
