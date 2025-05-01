package noop

import (
	"context"
	"testing"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/function"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// mockEventBus is a mock implementation of the event.Bus interface for testing
type mockEventBus struct {
	sendCount int
	lastEvent event.Event
}

func (m *mockEventBus) Subscribe(context.Context, event.System, chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}

func (m *mockEventBus) SubscribeP(context.Context, event.System, event.Kind, chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}

func (m *mockEventBus) Unsubscribe(context.Context, event.SubscriberID) {
}

func (m *mockEventBus) Send(_ context.Context, event event.Event) {
	m.sendCount++
	m.lastEvent = event
}

func (m *mockEventBus) reset() {
	m.sendCount = 0
	m.lastEvent = event.Event{}
}

func TestNoopRuntime_Execute(t *testing.T) {
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
				ID: registry.ParseID("test-function"),
			},
			wantErr: false,
		},
		{
			name: "empty target",
			task: runtime.Task{
				ID: registry.ParseID(""),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resultCh, err := n.Execute(context.Background(), tt.task)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, resultCh)

			result := <-resultCh
			require.NotNil(t, result)
			require.NotNil(t, result.Value)
			require.Contains(t, result.Value.Data(), tt.task.ID.String())
		})
	}
}

func TestNoopRuntime_Add(t *testing.T) {
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
				ID: registry.ID{
					NS:   "test-ns",
					Name: "test-function",
				},
				Kind: "function",
			},
			wantErr: false,
		},
		{
			name: "add empty entry",
			entry: registry.Entry{
				ID: registry.ID{
					NS:   "",
					Name: "",
				},
				Kind: "",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus.reset()

			err := n.Add(context.Background(), tt.entry)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			require.Equal(t, 1, bus.sendCount)
			require.Equal(t, function.System, bus.lastEvent.System)
			require.Equal(t, function.Register, bus.lastEvent.Kind)
			require.Equal(t, tt.entry.ID.String(), bus.lastEvent.Path)
		})
	}
}

func TestNoopRuntime_Update(t *testing.T) {
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
				ID: registry.ID{
					NS:   "test-ns",
					Name: "test-function",
				},
				Kind: "function",
			},
			wantErr: false,
		},
		{
			name: "update empty entry",
			entry: registry.Entry{
				ID: registry.ID{
					NS:   "",
					Name: "",
				},
				Kind: "",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus.reset()

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
				ID: registry.ID{
					NS:   "test-ns",
					Name: "test-function",
				},
				Kind: "function",
			},
			wantErr: false,
		},
		{
			name: "delete empty entry",
			entry: registry.Entry{
				ID: registry.ID{
					NS:   "",
					Name: "",
				},
				Kind: "",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus.reset()

			err := n.Delete(context.Background(), tt.entry)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			require.Equal(t, 1, bus.sendCount)
			require.Equal(t, function.System, bus.lastEvent.System)
			require.Equal(t, function.Delete, bus.lastEvent.Kind)
			require.Equal(t, tt.entry.ID.String(), bus.lastEvent.Path)
		})
	}
}
