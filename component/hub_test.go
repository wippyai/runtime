package component

import (
	"context"
	"github.com/ponyruntime/pony/api"
	eventsbus2 "github.com/ponyruntime/pony/component/eventbus"
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

	subsystems := []Declaration{
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
			return event.Kind() == "test"
		}),
		nil,
	).Return(newState, nil)

	// Start listening
	hub.ListenEvents()

	sub := eventsbus2.NewSubscriber()
	defer sub.Close()

	// Send test event
	hub.eb.Send(
		context.Background(),
		eventsbus2.NewEvent(
			"test",
			"test",
			payload.NewString("configure-server"),
		),
	)

	ev := sub.Wait(api.Transaction, api.EventAcceptChange)

	assert.Equal(t, api.EventAcceptChange, ev.Kind())

	// Verify
	mockServer.AssertExpectations(t)
	assert.Equal(t, newState, hub.states["test"])
}
