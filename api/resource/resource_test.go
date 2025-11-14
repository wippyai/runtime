// Package resource provides a system for managing and accessing shared resources.
package resource

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
)

func TestEventConstants(t *testing.T) {
	tests := []struct {
		name     string
		system   event.System
		kind     event.Kind
		expected string
	}{
		{"system", System, "", "resource"},
		{"register", "", Register, "resource.register"},
		{"update", "", Update, "resource.update"},
		{"delete", "", Delete, "resource.delete"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.system != "" {
				assert.Equal(t, tt.expected, string(tt.system))
			}
			if tt.kind != "" {
				assert.Equal(t, tt.expected, string(tt.kind))
			}
		})
	}
}

func TestErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{"resource not found", ErrResourceNotFound, "resource not found"},
		{"resource locked", ErrResourceLocked, "resource is locked"},
		{"resource released", ErrResourceReleased, "resource has been released"},
		{"resource closed", ErrResourceClosed, "resource provider is closed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.err.Error())
			assert.True(t, errors.Is(tt.err, tt.err))
		})
	}
}

func TestAccessMode(t *testing.T) {
	tests := []struct {
		name string
		mode AccessMode
		val  AccessMode
	}{
		{"normal mode", ModeNormal, 1},
		{"exclusive mode", ModeExclusive, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.val, tt.mode)
		})
	}
}

func TestEntry_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		entry   Entry
		wantErr bool
	}{
		{
			name: "complete entry",
			entry: Entry{
				ID:       registry.ID{NS: "resources", Name: "db"},
				Meta:     registry.Metadata{"type": "database"},
				Provider: nil,
			},
			wantErr: false,
		},
		{
			name: "minimal entry",
			entry: Entry{
				ID: registry.ID{NS: "r", Name: "test"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.entry)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded Entry
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.entry.ID, decoded.ID)
			assert.Equal(t, tt.entry.Meta, decoded.Meta)
		})
	}
}

func TestContext_Registry(t *testing.T) {
	t.Run("with app context", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()

		reg := GetRegistry(ctx)
		assert.Nil(t, reg)

		type mockRegistry struct{ Registry }
		mockReg := &mockRegistry{}

		ctx = WithRegistry(ctx, mockReg)

		retrieved := GetRegistry(ctx)
		assert.Equal(t, mockReg, retrieved)
	})

	t.Run("without app context", func(t *testing.T) {
		ctx := context.Background()

		reg := GetRegistry(ctx)
		assert.Nil(t, reg)

		type mockRegistry struct{ Registry }
		mockReg := &mockRegistry{}

		ctx = WithRegistry(ctx, mockReg)
		assert.Equal(t, context.Background(), ctx)

		reg = GetRegistry(ctx)
		assert.Nil(t, reg)
	})
}
