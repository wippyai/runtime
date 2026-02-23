// SPDX-License-Identifier: MPL-2.0

// Package resource provides a system for managing and accessing shared resources.
package resource

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
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
				assert.Equal(t, tt.expected, tt.system)
			}
			if tt.kind != "" {
				assert.Equal(t, tt.expected, tt.kind)
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
		{"resource not found", ErrNotFound, "resource not found"},
		{"resource released", ErrReleased, "resource has been released"},
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
		entry   Entry
		name    string
		wantErr bool
	}{
		{
			name: "complete entry",
			entry: Entry{
				ID:       registry.NewID("resources", "db"),
				Meta:     attrs.Bag{"type": "database"},
				Provider: nil,
			},
			wantErr: false,
		},
		{
			name: "minimal entry",
			entry: Entry{
				ID: registry.NewID("r", "test"),
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

	t.Run("idempotent", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()

		type mockRegistry struct{ Registry }
		mockReg := &mockRegistry{}

		ctx = WithRegistry(ctx, mockReg)

		type mockRegistry2 struct{ Registry }
		mockReg2 := &mockRegistry2{}

		WithRegistry(ctx, mockReg2)

		retrieved := GetRegistry(ctx)
		assert.Equal(t, mockReg, retrieved)
	})
}

func TestErrorInterface(t *testing.T) {
	t.Run("ErrNotFound", func(t *testing.T) {
		err := ErrNotFound
		assert.Equal(t, "resource not found", err.Error())
		assert.Equal(t, apierror.NotFound, err.Kind())
		assert.Equal(t, apierror.False, err.Retryable())
		assert.Nil(t, err.Details())
	})

	t.Run("ErrReleased", func(t *testing.T) {
		err := ErrReleased
		assert.Equal(t, "resource has been released", err.Error())
		assert.Equal(t, apierror.Invalid, err.Kind())
		assert.Equal(t, apierror.False, err.Retryable())
	})
}

func TestErrorIs(t *testing.T) {
	t.Run("same error type", func(t *testing.T) {
		assert.True(t, errors.Is(ErrNotFound, ErrNotFound))
		assert.True(t, errors.Is(ErrReleased, ErrReleased))
		assert.False(t, errors.Is(ErrNotFound, ErrReleased))
	})

	t.Run("different error type", func(t *testing.T) {
		assert.False(t, errors.Is(ErrNotFound, errors.New("other error")))
	})
}

type mockResource struct {
	value    any
	released bool
}

func (m *mockResource) Get() (any, error) {
	if m.released {
		return nil, ErrReleased
	}
	return m.value, nil
}

func (m *mockResource) Release() {
	m.released = true
}

func TestTrackedResource(t *testing.T) {
	t.Run("Get_Success", func(t *testing.T) {
		inner := &mockResource{value: "test-value"}
		releaseCalled := false
		tr := NewTrackedResource(inner, func() { releaseCalled = true })

		val, err := tr.Get()
		assert.NoError(t, err)
		assert.Equal(t, "test-value", val)
		assert.False(t, releaseCalled)
	})

	t.Run("Get_AfterRelease", func(t *testing.T) {
		inner := &mockResource{value: "test-value"}
		tr := NewTrackedResource(inner, nil)

		tr.Release()

		val, err := tr.Get()
		assert.Nil(t, val)
		assert.ErrorIs(t, err, ErrReleased)
	})

	t.Run("Release_CallsOnRelease", func(t *testing.T) {
		inner := &mockResource{value: "test"}
		releaseCalled := false
		tr := NewTrackedResource(inner, func() { releaseCalled = true })

		tr.Release()

		assert.True(t, releaseCalled)
		assert.True(t, inner.released)
	})

	t.Run("Release_IdempotentRelease", func(t *testing.T) {
		inner := &mockResource{value: "test"}
		releaseCount := 0
		tr := NewTrackedResource(inner, func() { releaseCount++ })

		tr.Release()
		tr.Release()
		tr.Release()

		assert.Equal(t, 1, releaseCount)
	})

	t.Run("Release_NilOnRelease", func(t *testing.T) {
		inner := &mockResource{value: "test"}
		tr := NewTrackedResource(inner, nil)

		tr.Release()

		assert.True(t, inner.released)
	})

	t.Run("PoolReuse", func(t *testing.T) {
		for i := 0; i < 10; i++ {
			inner := &mockResource{value: i}
			tr := NewTrackedResource(inner, nil)
			val, _ := tr.Get()
			assert.Equal(t, i, val)
			tr.Release()
		}
	})
}
