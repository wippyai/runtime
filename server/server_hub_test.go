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

func (m *MockSubsystemServer) Handle(ctx context.Context, event api.Event, state *component.State) (*component.State, error) {
	args := m.Called(ctx, event, state)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*component.State), args.Error(1)
}

func (m *MockSubsystemServer) Commit(ctx context.Context, state *component.State) error {
	args := m.Called(ctx, state)
	return args.Error(0)
}

func (m *MockSubsystemServer) Start(ctx context.Context, q *exec.Queue) {
}

func (m *MockSubsystemServer) Stop(ctx context.Context) {
}

// TestHubListenEvents tests the event listening functionality
func TestHubHandleEvent(t *testing.T) {
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
	newState := &component.State{
		Component: "test",
	}

	mockServer.On("Handle",
		mock.MatchedBy(func(ctx context.Context) bool {
			return true
		}),
		mock.MatchedBy(func(event api.Event) bool {
			return event.Type() == "test"
		}),
		(*component.State)(nil),
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

	ev := sub.Wait(api.Transaction, "change")

	assert.Equal(t, api.EventType("state"), ev.Type())

	// Verify
	mockServer.AssertExpectations(t)
	assert.Equal(t, newState, hub.states["test"])
}
