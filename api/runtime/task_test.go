// Package runtime provides runtime execution and command management.
package runtime

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
)

func TestTask_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		task    Task
		wantErr bool
	}{
		{
			name: "complete task",
			task: Task{
				ID:       registry.NewID("functions", "process"),
				Payloads: payload.Payloads{payload.New("test data")},
				Options: func() attrs.Attributes {
					bag := attrs.NewBag()
					bag.Set("timeout", "30s")
					return bag
				}(),
				Context: []ctxapi.Pair{
					{Key: &ctxapi.Key{Name: "user"}, Value: "john"},
					{Key: &ctxapi.Key{Name: "scope"}, Value: "admin"},
				},
			},
			wantErr: false,
		},
		{
			name: "minimal task",
			task: Task{
				ID: registry.NewID("f", "test"),
			},
			wantErr: false,
		},
		{
			name: "with payloads only",
			task: Task{
				ID:       registry.NewID("funcs", "handler"),
				Payloads: payload.Payloads{payload.New(map[string]any{"key": "value"})},
			},
			wantErr: false,
		},
		{
			name: "with context only",
			task: Task{
				ID: registry.NewID("funcs", "handler"),
				Context: []ctxapi.Pair{
					{Key: &ctxapi.Key{Name: "env"}, Value: "production"},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.task)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.NotEmpty(t, data)
		})
	}
}

func TestResult_Marshal(t *testing.T) {
	testErr := errors.New("test error")

	tests := []struct {
		name    string
		result  Result
		wantErr bool
	}{
		{
			name: "successful result",
			result: Result{
				Value: payload.New("success"),
				Error: nil,
			},
			wantErr: false,
		},
		{
			name: "error result",
			result: Result{
				Value: payload.New(nil),
				Error: testErr,
			},
			wantErr: false,
		},
		{
			name: "result with complex value",
			result: Result{
				Value: payload.New(map[string]any{
					"status": "completed",
					"data":   []int{1, 2, 3},
				}),
				Error: nil,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.result)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.NotEmpty(t, data)
		})
	}
}

func TestContext_FrameID(t *testing.T) {
	t.Run("with frame context", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)

		id, ok := GetFrameID(ctx)
		assert.False(t, ok)
		assert.Equal(t, registry.NewID("", ""), id)

		testID := registry.NewID("test", "function")
		err := SetFrameID(ctx, testID)
		require.NoError(t, err)

		retrieved, ok := GetFrameID(ctx)
		assert.True(t, ok)
		assert.Equal(t, testID, retrieved)
	})

	t.Run("without frame context", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()

		id, ok := GetFrameID(ctx)
		assert.False(t, ok)
		assert.Equal(t, registry.NewID("", ""), id)

		testID := registry.NewID("test", "function")
		err := SetFrameID(ctx, testID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no frame context available")
	})
}

func TestContext_FramePID(t *testing.T) {
	t.Run("with frame context", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)

		p, ok := GetFramePID(ctx)
		assert.False(t, ok)
		assert.Equal(t, pid.PID{}, p)

		testPID := pid.PID{UniqID: "test-pid-123"}
		err := SetFramePID(ctx, testPID)
		require.NoError(t, err)

		retrieved, ok := GetFramePID(ctx)
		assert.True(t, ok)
		assert.Equal(t, testPID, retrieved)
	})

	t.Run("without frame context", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()

		p, ok := GetFramePID(ctx)
		assert.False(t, ok)
		assert.Equal(t, pid.PID{}, p)

		testPID := pid.PID{UniqID: "test-pid-123"}
		err := SetFramePID(ctx, testPID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no frame context available")
	})
}

func TestContext_FrameLifecycleOptions(t *testing.T) {
	t.Run("with frame context and lifecycle options", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()
		ctx, fc := ctxapi.OpenFrameContext(ctx)

		opts := map[string]any{"timeout": 30}
		_ = fc.Set(FrameLifecycleOptionsKey, opts)

		retrieved := GetFrameLifecycleOptions(ctx)
		assert.NotNil(t, retrieved)
		assert.Equal(t, opts, retrieved)
	})

	t.Run("with frame context but no lifecycle options", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)

		retrieved := GetFrameLifecycleOptions(ctx)
		assert.Nil(t, retrieved)
	})

	t.Run("without frame context", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()

		retrieved := GetFrameLifecycleOptions(ctx)
		assert.Nil(t, retrieved)
	})
}

func TestErrorInterface(t *testing.T) {
	t.Run("ErrNoFrameContext", func(t *testing.T) {
		err := ctxapi.ErrNoFrameContext
		assert.Equal(t, "no frame context available", err.Error())
		assert.Equal(t, "Invalid", err.Kind().String())
		assert.False(t, err.Retryable().Bool())
		assert.Nil(t, err.Details())
	})
}
