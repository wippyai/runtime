// Package event provides an event bus implementation.
package event

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
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
