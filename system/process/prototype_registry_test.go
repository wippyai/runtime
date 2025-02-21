package process

import (
	"context"
	"github.com/ponyruntime/pony/api/process"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/system/eventbus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// mockProcess implements process.Process interface
type mockProcess struct {
	sendErr error
	stepErr error
}

func (m *mockProcess) Send(batch *pubsub.Batch) error {
	return m.sendErr
}

func (m *mockProcess) Start(_ context.Context, _ pubsub.PID, _ payload.Payloads) error {
	return nil
}

func (m *mockProcess) Step() (bool, error) {
	return true, m.stepErr
}

func newTestPrototypeRegistry(t *testing.T) (*PrototypeRegistry, events.Bus) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	reg := NewPrototypeFactory(bus, logger)
	return reg, bus
}

func TestPrototypeRegistry_StartStop(t *testing.T) {
	ctx := context.Background()
	reg, _ := newTestPrototypeRegistry(t)

	err := reg.Start(ctx)
	require.NoError(t, err)
	assert.NotNil(t, reg.subscriber)

	err = reg.Stop()
	require.NoError(t, err)
}

func TestPrototypeRegistry_RegisterPrototype(t *testing.T) {
	ctx := context.Background()
	protoRegistry, bus := newTestPrototypeRegistry(t)
	require.NoError(t, protoRegistry.Start(ctx))
	defer func() { assert.NoError(t, protoRegistry.Stop()) }()

	responses := make(chan events.Event, 1)
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		process.PrototypeSystem,
		"prototype.*",
		func(evt events.Event) {
			if evt.Kind == process.AcceptPrototype || evt.Kind == process.RejectPrototype {
				responses <- evt
			}
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	t.Run("successful registration", func(t *testing.T) {
		mockProcess := func() (process.Process, error) {
			return &mockProcess{}, nil
		}

		bus.Send(ctx, events.Event{
			System: process.PrototypeSystem,
			Kind:   process.RegisterPrototype,
			Path:   "test:mock-process",
			Data:   process.Prototype(mockProcess),
		})

		select {
		case resp := <-responses:
			assert.Equal(t, process.AcceptPrototype, resp.Kind)
			assert.Equal(t, "test:mock-process", resp.Path)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for response")
		}

		createdProcess, err := protoRegistry.Create(registry.ParseID("test:mock-process"))
		require.NoError(t, err)
		assert.NotNil(t, createdProcess)
	})

	t.Run("invalid registration payload", func(t *testing.T) {
		bus.Send(ctx, events.Event{
			System: process.PrototypeSystem,
			Kind:   process.RegisterPrototype,
			Path:   "test:invalid-process",
			Data:   "invalid data",
		})

		select {
		case resp := <-responses:
			assert.Equal(t, process.RejectPrototype, resp.Kind)
			assert.Equal(t, "test:invalid-process", resp.Path)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for response")
		}
	})

	t.Run("registration with error-producing prototype", func(t *testing.T) {
		errorPrototype := func() (process.Process, error) {
			return &mockProcess{
				sendErr: assert.AnError,
				stepErr: assert.AnError,
			}, nil
		}

		bus.Send(ctx, events.Event{
			System: process.PrototypeSystem,
			Kind:   process.RegisterPrototype,
			Path:   "test:error-process",
			Data:   process.Prototype(errorPrototype),
		})

		select {
		case resp := <-responses:
			assert.Equal(t, process.AcceptPrototype, resp.Kind)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for response")
		}

		proc, err := protoRegistry.Create(registry.ParseID("test:error-process"))
		require.NoError(t, err)
		assert.Error(t, proc.Send(nil))

		_, err = proc.Step()
		assert.Error(t, err)
	})
}

func TestPrototypeRegistry_DeletePrototype(t *testing.T) {
	ctx := context.Background()
	protoRegistry, bus := newTestPrototypeRegistry(t)
	require.NoError(t, protoRegistry.Start(ctx))
	defer func() { assert.NoError(t, protoRegistry.Stop()) }()

	responses := make(chan events.Event, 1)
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		process.PrototypeSystem,
		"prototype.*",
		func(evt events.Event) {
			if evt.Kind == process.AcceptPrototype || evt.Kind == process.RejectPrototype {
				responses <- evt
			}
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	// Register a test prototype first
	processID := "test:mock-process"
	mockProcess := func() (process.Process, error) {
		return &mockProcess{}, nil
	}

	bus.Send(ctx, events.Event{
		System: process.PrototypeSystem,
		Kind:   process.RegisterPrototype,
		Path:   processID,
		Data:   process.Prototype(mockProcess),
	})

	select {
	case resp := <-responses:
		assert.Equal(t, process.AcceptPrototype, resp.Kind)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for registration response")
	}

	t.Run("successful deletion", func(t *testing.T) {
		bus.Send(ctx, events.Event{
			System: process.PrototypeSystem,
			Kind:   process.DeletePrototype,
			Path:   processID,
		})

		select {
		case resp := <-responses:
			assert.Equal(t, process.AcceptPrototype, resp.Kind)
			assert.Equal(t, processID, resp.Path)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for response")
		}

		// Verify prototype was deleted
		_, err := protoRegistry.Create(registry.ParseID(processID))
		assert.Error(t, err)
	})

	t.Run("delete non-existent prototype", func(t *testing.T) {
		bus.Send(ctx, events.Event{
			System: process.PrototypeSystem,
			Kind:   process.DeletePrototype,
			Path:   "test:nonexistent",
		})

		select {
		case resp := <-responses:
			assert.Equal(t, process.RejectPrototype, resp.Kind)
			assert.Equal(t, "test:nonexistent", resp.Path)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for response")
		}
	})
}

func TestPrototypeRegistry_Create(t *testing.T) {
	ctx := context.Background()
	protoRegistry, bus := newTestPrototypeRegistry(t)
	require.NoError(t, protoRegistry.Start(ctx))
	defer func() { assert.NoError(t, protoRegistry.Stop()) }()

	// Register test prototypes
	successProcess := func() (process.Process, error) {
		return &mockProcess{}, nil
	}
	errorProcess := func() (process.Process, error) {
		return nil, assert.AnError
	}

	bus.Send(ctx, events.Event{
		System: process.PrototypeSystem,
		Kind:   process.RegisterPrototype,
		Path:   "test:success-process",
		Data:   process.Prototype(successProcess),
	})

	bus.Send(ctx, events.Event{
		System: process.PrototypeSystem,
		Kind:   process.RegisterPrototype,
		Path:   "test:error-process",
		Data:   process.Prototype(errorProcess),
	})

	// Wait for registrations to complete
	time.Sleep(100 * time.Millisecond)

	tests := []struct {
		name        string
		processID   string
		expectError bool
	}{
		{
			name:        "successful creation",
			processID:   "test:success-process",
			expectError: false,
		},
		{
			name:        "non-existent prototype",
			processID:   "test:nonexistent",
			expectError: true,
		},
		{
			name:        "prototype returns error",
			processID:   "test:error-process",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proc, err := protoRegistry.Create(registry.ParseID(tt.processID))
			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, proc)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, proc)
			}
		})
	}
}
