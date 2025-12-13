// Package event provides an event bus implementation.
package event

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
)

func TestEvent_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		event   Event
		wantErr bool
	}{
		{
			name: "complete event",
			event: Event{
				System: "registry",
				Kind:   "register",
				Path:   "services.api",
				Data:   map[string]any{"id": "123", "name": "api-service"},
			},
			wantErr: false,
		},
		{
			name: "minimal event",
			event: Event{
				System: "test",
				Kind:   "test.event",
			},
			wantErr: false,
		},
		{
			name: "with nil data",
			event: Event{
				System: "system",
				Kind:   "event",
				Path:   "path",
				Data:   nil,
			},
			wantErr: false,
		},
		{
			name: "with string data",
			event: Event{
				System: "log",
				Kind:   "info",
				Path:   "app.service",
				Data:   "log message",
			},
			wantErr: false,
		},
		{
			name: "with struct data",
			event: Event{
				System: "user",
				Kind:   "created",
				Path:   "users",
				Data:   struct{ ID string }{"user-123"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.event)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded = Event{}
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.event.System, decoded.System)
			assert.Equal(t, tt.event.Kind, decoded.Kind)
			assert.Equal(t, tt.event.Path, decoded.Path)
		})
	}
}

func TestTypeAliases(t *testing.T) {
	t.Run("SubscriberID", func(t *testing.T) {
		var id = SubscriberID("sub-123")
		assert.Equal(t, "sub-123", id)
		assert.IsType(t, "", id)
	})

	t.Run("System", func(t *testing.T) {
		var sys = System("test-system")
		assert.Equal(t, "test-system", sys)
		assert.IsType(t, "", sys)
	})

	t.Run("Kind", func(t *testing.T) {
		var kind = Kind("test.kind")
		assert.Equal(t, "test.kind", kind)
		assert.IsType(t, "", kind)
	})

	t.Run("Path", func(t *testing.T) {
		var path = Path("test.path")
		assert.Equal(t, "test.path", path)
		assert.IsType(t, "", path)
	})
}

func TestContext_Bus(t *testing.T) {
	t.Run("with app context", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()

		bus := GetBus(ctx)
		assert.Nil(t, bus)

		type mockBus struct{ Bus }
		mockB := &mockBus{}

		ctx = WithBus(ctx, mockB)

		retrieved := GetBus(ctx)
		assert.Equal(t, mockB, retrieved)
	})

	t.Run("without app context", func(t *testing.T) {
		ctx := context.Background()

		bus := GetBus(ctx)
		assert.Nil(t, bus)

		type mockBus struct{ Bus }
		mockB := &mockBus{}

		ctx = WithBus(ctx, mockB)
		assert.Equal(t, context.Background(), ctx)

		bus = GetBus(ctx)
		assert.Nil(t, bus)
	})
}

func TestErrorInterface(t *testing.T) {
	t.Run("NewRouterCanceledError", func(t *testing.T) {
		cause := context.Canceled
		err := NewRouterCanceledError(cause)
		assert.Contains(t, err.Error(), "router context canceled")
		assert.Equal(t, "Canceled", err.Kind().String())
		assert.False(t, err.Retryable().Bool())
		assert.Equal(t, cause, err.Unwrap())
		details := err.Details()
		require.NotNil(t, details)
	})

	t.Run("NewSubscriberError", func(t *testing.T) {
		cause := context.DeadlineExceeded
		err := NewSubscriberError(cause)
		assert.Contains(t, err.Error(), "failed to create subscriber")
		assert.Equal(t, "Internal", err.Kind().String())
		assert.True(t, err.Retryable().Bool())
		assert.Equal(t, cause, err.Unwrap())
	})

	t.Run("NewAwaitTimeoutError", func(t *testing.T) {
		err := NewAwaitTimeoutError("/test/path")
		assert.Contains(t, err.Error(), "await timeout")
		assert.Contains(t, err.Error(), "/test/path")
		assert.Equal(t, "Timeout", err.Kind().String())
		assert.True(t, err.Retryable().Bool())
		assert.Nil(t, err.Unwrap())
		details := err.Details()
		require.NotNil(t, details)
	})
}

func TestCommands(t *testing.T) {
	t.Run("EventsSubscribeCmd", func(t *testing.T) {
		cmd := EventsSubscribeCmd{
			System: "test-system",
			Kind:   "test-kind",
			Topic:  "test-topic",
			PID:    pid.PID{Node: "node1", Host: "host1", UniqID: "123"},
		}
		assert.Equal(t, CmdEventsSubscribe, cmd.CmdID())
		assert.Equal(t, "test-system", cmd.System)
		assert.Equal(t, "test-kind", cmd.Kind)
		assert.Equal(t, "test-topic", cmd.Topic)
	})

	t.Run("EventsSendCmd", func(t *testing.T) {
		cmd := EventsSendCmd{
			System: "test-system",
			Kind:   "test-kind",
			Path:   "test-path",
			Data:   map[string]any{"key": "value"},
		}
		assert.Equal(t, CmdEventsSend, cmd.CmdID())
		assert.Equal(t, "test-system", cmd.System)
		assert.Equal(t, "test-path", cmd.Path)
	})

	t.Run("command ID constants", func(t *testing.T) {
		assert.Equal(t, 90, int(CmdEventsSubscribe))
		assert.Equal(t, 91, int(CmdEventsSend))
	})
}

func TestSubscription(t *testing.T) {
	called := false
	sub := Subscription{
		System:      "test-system",
		Kind:        "test-kind",
		Topic:       "test-topic",
		Unsubscribe: func() { called = true },
	}
	assert.Equal(t, "test-system", sub.System)
	assert.Equal(t, "test-kind", sub.Kind)
	assert.Equal(t, "test-topic", sub.Topic)

	sub.Unsubscribe()
	assert.True(t, called)
}
