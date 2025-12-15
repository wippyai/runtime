// Package function provides abstractions for managing and executing asynchronous functions.
package function

import (
	"context"
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

func TestConstants(t *testing.T) {
	t.Run("System", func(t *testing.T) {
		assert.Equal(t, "function", System)
	})
}

func TestEventConstants(t *testing.T) {
	tests := []struct {
		name     string
		kind     event.Kind
		expected string
	}{
		{"register", Register, "function.register"},
		{"delete", Delete, "function.delete"},
		{"accept", Accept, "function.accept"},
		{"reject", Reject, "function.reject"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.kind)
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

func TestContext_InterceptorRegistry(t *testing.T) {
	t.Run("with app context", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()

		reg := GetInterceptorRegistry(ctx)
		assert.Nil(t, reg)

		type mockInterceptorRegistry struct{ InterceptorRegistry }
		mockReg := &mockInterceptorRegistry{}

		ctx = WithInterceptorRegistry(ctx, mockReg)

		retrieved := GetInterceptorRegistry(ctx)
		assert.Equal(t, mockReg, retrieved)
	})

	t.Run("without app context", func(t *testing.T) {
		ctx := context.Background()

		reg := GetInterceptorRegistry(ctx)
		assert.Nil(t, reg)

		type mockInterceptorRegistry struct{ InterceptorRegistry }
		mockReg := &mockInterceptorRegistry{}

		ctx = WithInterceptorRegistry(ctx, mockReg)
		assert.Equal(t, context.Background(), ctx)

		reg = GetInterceptorRegistry(ctx)
		assert.Nil(t, reg)
	})
}

func TestSentinelErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      apierror.Error
		expected string
		kind     string
	}{
		{"ErrRegistryNotFound", ErrRegistryNotFound, "function registry not found in context", "NotFound"},
		{"ErrProcessContextNotFound", ErrProcessContextNotFound, "process context not found", "NotFound"},
		{"ErrCallNotFound", ErrCallNotFound, "async call not found", "NotFound"},
		{"ErrNilContext", ErrNilContext, "nil context", "Invalid"},
		{"ErrNilCallback", ErrNilCallback, "nil callback", "Invalid"},
		{"ErrNodeNotFound", ErrNodeNotFound, "relay node not configured", "NotFound"},
		{"ErrPIDNotFound", ErrPIDNotFound, "frame PID not found in context", "NotFound"},
		{"ErrPIDGeneratorNotFound", ErrPIDGeneratorNotFound, "PID generator not found in context", "NotFound"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.err.Error())
			assert.Equal(t, tt.kind, tt.err.Kind().String())
			assert.False(t, tt.err.Retryable().Bool())
			assert.Nil(t, errors.Unwrap(tt.err))
		})
	}
}

func TestErrorMethods(t *testing.T) {
	t.Run("WithCause", func(t *testing.T) {
		cause := errors.New("underlying cause")
		newErr := ErrCallNotFound.WithCause(cause)
		assert.Equal(t, cause, errors.Unwrap(newErr))
		assert.Equal(t, ErrCallNotFound.Error(), newErr.Error())
	})

	t.Run("WithDetails", func(t *testing.T) {
		details := attrs.NewBagFrom(map[string]any{"key": "value"})
		newErr := ErrCallNotFound.WithDetails(details)
		assert.NotNil(t, newErr.Details())
		val, _ := newErr.Details().Get("key")
		assert.Equal(t, "value", val)
	})

	t.Run("WithMessage", func(t *testing.T) {
		newErr := ErrCallNotFound.WithMessage("custom message")
		assert.Equal(t, "custom message", newErr.Error())
		assert.Equal(t, ErrCallNotFound.Kind(), newErr.Kind())
	})
}

func TestErrorConstructors(t *testing.T) {
	t.Run("NewHandlerNotFoundError", func(t *testing.T) {
		id := registry.NewID("ns", "name")
		err := NewHandlerNotFoundError(id)
		assert.Contains(t, err.Error(), "no handler registered for target")
		assert.Equal(t, "NotFound", err.Kind().String())
		details := err.Details()
		require.NotNil(t, details)
		target, _ := details.Get("target")
		assert.Equal(t, id.String(), target)
	})
}

func TestCommandPools(t *testing.T) {
	t.Run("CallCmd", func(t *testing.T) {
		cmd := AcquireCallCmd()
		assert.NotNil(t, cmd)
		assert.Equal(t, Call, cmd.CmdID())
		cmd.Release()
	})

	t.Run("AsyncStartCmd", func(t *testing.T) {
		cmd := AcquireAsyncStartCmd()
		assert.NotNil(t, cmd)
		cmd.Topic = "test-topic"
		assert.Equal(t, AsyncStart, cmd.CmdID())
		cmd.Release()
		assert.Empty(t, cmd.Topic)
	})

	t.Run("AsyncCancelCmd", func(t *testing.T) {
		cmd := AcquireAsyncCancelCmd()
		assert.NotNil(t, cmd)
		cmd.Topic = "test-topic"
		assert.Equal(t, AsyncCancel, cmd.CmdID())
		cmd.Release()
		assert.Empty(t, cmd.Topic)
	})
}

func TestCommandIDs(t *testing.T) {
	assert.Equal(t, 200, int(Call))
	assert.Equal(t, 201, int(AsyncStart))
	assert.Equal(t, 203, int(AsyncCancel))
}

func TestCallResult(t *testing.T) {
	t.Run("with value", func(t *testing.T) {
		result := CallResult{Value: "test", Error: nil}
		assert.Equal(t, "test", result.Value)
		assert.Nil(t, result.Error)
	})

	t.Run("with error", func(t *testing.T) {
		err := errors.New("test error")
		result := CallResult{Value: nil, Error: err}
		assert.Nil(t, result.Value)
		assert.Equal(t, err, result.Error)
	})
}

func TestAsyncStartResult(t *testing.T) {
	t.Run("no error", func(t *testing.T) {
		result := AsyncStartResult{Error: nil}
		assert.Nil(t, result.Error)
	})

	t.Run("with error", func(t *testing.T) {
		err := errors.New("async start error")
		result := AsyncStartResult{Error: err}
		assert.Equal(t, err, result.Error)
	})
}
