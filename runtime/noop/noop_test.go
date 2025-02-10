package noop

import (
	"context"
	"testing"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// mockEventBus is a mock implementation of the events.Bus interface for testing
type mockEventBus struct {
	sendCount int
	lastEvent events.Event
}

func (m *mockEventBus) Subscribe(context.Context, events.System, chan<- events.Event) (events.SubscriberID, error) {
	return "", nil
}

func (m *mockEventBus) SubscribeP(context.Context, events.System, events.Kind, chan<- events.Event) (events.SubscriberID, error) {
	return "", nil
}

func (m *mockEventBus) Unsubscribe(context.Context, events.SubscriberID) {
}

func (m *mockEventBus) Send(_ context.Context, event events.Event) {
	m.sendCount++
	m.lastEvent = event
}

func (m *mockEventBus) reset() {
	m.sendCount = 0
	m.lastEvent = events.Event{}
}

func TestNoopRuntime_Execute(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	bus := &mockEventBus{}
	n := NewNoopRuntime(bus, logger)

	tests := []struct {
		name    string
		task    runtime.Task
		wantErr bool
	}{
		{
			name: "basic execution",
			task: runtime.Task{
				Handler: "test-function",
			},
			wantErr: false,
		},
		{
			name: "empty target",
			task: runtime.Task{
				Handler: "",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resultCh, err := n.Execute(tt.task)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, resultCh)

			// Verify the result
			result := <-resultCh
			require.NotNil(t, result)
			require.NotNil(t, result.Payload)
			require.Contains(t, result.Payload.Data(), tt.task.Handler)
		})
	}
}

func TestNoopRuntime_Add(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	bus := &mockEventBus{}
	n := NewNoopRuntime(bus, logger)

	tests := []struct {
		name    string
		entry   registry.Entry
		wantErr bool
	}{
		{
			name: "add function entry",
			entry: registry.Entry{
				ID:   "test-function",
				Kind: "function",
			},
			wantErr: false,
		},
		{
			name: "add empty entry",
			entry: registry.Entry{
				ID:   "",
				Kind: "",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus.reset() // Reset mock state before each test case

			err := n.Add(context.Background(), tt.entry)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			// Verify event was sent
			require.Equal(t, 1, bus.sendCount)
			require.Equal(t, runtime.FunctionSystem, bus.lastEvent.System)
			require.Equal(t, runtime.RegisterFunctionCommand, bus.lastEvent.Kind)
			require.Equal(t, events.Path(tt.entry.ID), bus.lastEvent.Path)
		})
	}
}

func TestNoopRuntime_Update(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	bus := &mockEventBus{}
	n := NewNoopRuntime(bus, logger)

	tests := []struct {
		name    string
		entry   registry.Entry
		wantErr bool
	}{
		{
			name: "update function entry",
			entry: registry.Entry{
				ID:   "test-function",
				Kind: "function",
			},
			wantErr: false,
		},
		{
			name: "update empty entry",
			entry: registry.Entry{
				ID:   "",
				Kind: "",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus.reset() // Reset mock state before each test case

			err := n.Update(context.Background(), tt.entry)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestNoopRuntime_Delete(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	bus := &mockEventBus{}
	n := NewNoopRuntime(bus, logger)

	tests := []struct {
		name    string
		entry   registry.Entry
		wantErr bool
	}{
		{
			name: "delete function entry",
			entry: registry.Entry{
				ID:   "test-function",
				Kind: "function",
			},
			wantErr: false,
		},
		{
			name: "delete empty entry",
			entry: registry.Entry{
				ID:   "",
				Kind: "",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus.reset() // Reset mock state before each test case

			err := n.Delete(context.Background(), tt.entry)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			// Verify event was sent
			require.Equal(t, 1, bus.sendCount)
			require.Equal(t, runtime.FunctionSystem, bus.lastEvent.System)
			require.Equal(t, runtime.DeleteFunctionCommand, bus.lastEvent.Kind)
			require.Equal(t, events.Path(tt.entry.ID), bus.lastEvent.Path)
		})
	}
}
