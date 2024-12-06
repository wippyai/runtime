package component

import (
	"context"
	"fmt"
	"testing"

	"github.com/ponyruntime/pony/api"
	"github.com/ponyruntime/pony/api/payload"
	ebs "github.com/ponyruntime/pony/component/eventbus"
	"github.com/ponyruntime/pony/config"
	"github.com/ponyruntime/pony/exec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

// mockComponent is a mock implementation of component.Component
type mockComponent struct {
	mock.Mock
}

func (m *mockComponent) Register(ctx context.Context, event api.Event, chs State) (State, error) {
	args := m.Called(ctx, event, chs)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(State), args.Error(1)
}

func (m *mockComponent) Apply(ctx context.Context, state State) error {
	m.Called(ctx, state)
	return nil
}

func (m *mockComponent) Start(ctx context.Context, q *exec.Queue) {
}

func (m *mockComponent) Stop(ctx context.Context) {
}

type mockState struct {
	mock.Mock
	Data string
}

func (m *mockState) Discard(ctx context.Context) {
	m.Called(ctx)
}

// TestHubListenEvents tests the event listening functionality
func TestUpdateState(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	queue := exec.NewQueue()
	cmp := &mockComponent{}

	subsystems := []Declaration{
		{
			ID:        "test",
			Component: cmp,
		},
	}

	hub := NewHub(logger, queue, subsystems...)
	defer hub.Close(context.Background())

	// Setup mock expectations
	newState := new(mockState)

	cmp.On("Register",
		mock.MatchedBy(func(ctx context.Context) bool {
			return true
		}),
		mock.MatchedBy(func(event api.Event) bool {
			return event.Kind() == "test"
		}),
		mock.Anything,
	).Return(newState, nil)

	// Start listening
	hub.Boot(context.Background())

	sub := ebs.NewSubscriber()
	defer sub.Close()

	// begin operation
	hub.eb.Send(context.Background(), ebs.NewEvent(config.ConfigGroup, config.Begin, nil))

	// Send test event
	hub.eb.Send(
		context.Background(),
		ebs.NewEvent(
			"test",
			"test",
			payload.NewString("configure-server"),
		),
	)

	sub.Wait(config.ConfigGroup, config.AckState)

	cmp.AssertExpectations(t)
	assert.Equal(t, newState, hub.changes.get("test").changes)
}

// TestHubListenEvents tests the event listening functionality
func TestStateRollback(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	queue := exec.NewQueue()
	cmp := &mockComponent{}

	subsystems := []Declaration{
		{
			ID:        "test",
			Component: cmp,
		},
	}

	hub := NewHub(logger, queue, subsystems...)
	defer hub.Close(context.Background())

	// Setup mock expectations
	newState := new(mockState)
	newState.On("Discard", mock.Anything).Return()

	cmp.On("Register",
		mock.MatchedBy(func(ctx context.Context) bool {
			return true
		}),
		mock.MatchedBy(func(event api.Event) bool {
			return event.Kind() == "test"
		}),
		nil,
	).Return(newState, nil)

	// Start listening
	hub.Boot(context.Background())

	sub := ebs.NewSubscriber()
	defer sub.Close()

	// begin operation
	hub.eb.Send(context.Background(), ebs.NewEvent(config.ConfigGroup, config.Begin, nil))

	// Send test event
	hub.eb.Send(
		context.Background(),
		ebs.NewEvent(
			"test",
			"test",
			payload.NewString("configure-server"),
		),
	)

	sub.Wait(config.ConfigGroup, config.AckState)

	hub.eb.Send(context.Background(), ebs.NewEvent(config.ConfigGroup, config.Discard, nil))
	sub.Wait(config.ConfigGroup, config.Done)

	cmp.AssertExpectations(t)
}

// TestHubListenEvents tests the event listening functionality
func TestStatePropagateState(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	queue := exec.NewQueue()
	cmp := &mockComponent{}

	subsystems := []Declaration{
		{
			ID:        "test",
			Component: cmp,
		},
	}

	hub := NewHub(logger, queue, subsystems...)
	defer hub.Close(context.Background())

	// Setup mock expectations
	newState := new(mockState)

	cmp.On("Register",
		mock.MatchedBy(func(ctx context.Context) bool {
			return true
		}),
		mock.MatchedBy(func(event api.Event) bool {
			return event.Kind() == "test"
		}),
		nil,
	).Return(newState, nil)

	finalState := new(mockState)
	finalState.Data = "final"

	cmp.On("Register",
		mock.MatchedBy(func(ctx context.Context) bool {
			return true
		}),
		mock.MatchedBy(func(event api.Event) bool {
			return event.Kind() == "test"
		}),
		newState,
	).Return(finalState, nil)

	cmp.On("Apply",
		//mock.MatchedBy(func(ctx context.Context) bool {
		//	return true
		//}),
		//finalState,
		mock.Anything,
		mock.Anything,
	).Return(fmt.Errorf("error"))

	// Start listening
	hub.Boot(context.Background())

	sub := ebs.NewSubscriber()
	defer sub.Close()

	// begin operation
	hub.eb.Send(context.Background(), ebs.NewEvent(config.ConfigGroup, config.Begin, nil))

	// Send test event
	hub.eb.Send(
		context.Background(),
		ebs.NewEvent(
			"test",
			"test",
			payload.NewString("configure-server"),
		),
	)

	sub.Wait(config.ConfigGroup, config.AckState)

	hub.eb.Send(
		context.Background(),
		ebs.NewEvent(
			"test",
			"test",
			payload.NewString("configure-server-2"),
		),
	)

	sub.Wait(config.ConfigGroup, config.AckState)

	hub.eb.Send(context.Background(), ebs.NewEvent(config.ConfigGroup, config.Apply, nil))

	e := sub.Wait(config.ConfigGroup, config.Done)

	assert.Equal(t, finalState, e.Payload().Data().(state).changes)
	cmp.AssertExpectations(t)
}
