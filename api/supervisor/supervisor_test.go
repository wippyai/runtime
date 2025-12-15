// Package supervisor provides service lifecycle management and supervision.
package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/event"
)

func TestEventConstants(t *testing.T) {
	tests := []struct {
		name     string
		system   event.System
		kind     event.Kind
		expected string
	}{
		{"system", System, "", "supervisor"},
		{"register", "", ServiceRegister, "service.register"},
		{"remove", "", ServiceRemove, "service.remove"},
		{"update", "", ServiceUpdate, "service.update"},
		{"start", "", ServiceStart, "service.start"},
		{"stop", "", ServiceStop, "service.stop"},
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

func TestStatusConstants(t *testing.T) {
	tests := []struct {
		name     string
		status   Status
		expected string
	}{
		{"unknown", StatusUnknown, "unknown"},
		{"starting", StatusStarting, "starting"},
		{"running", StatusRunning, "running"},
		{"stopping", StatusStopping, "stopping"},
		{"stopped", StatusStopped, "stopped"},
		{"exited", StatusExited, "exited"},
		{"failed", StatusFailed, "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.status)
		})
	}
}

func TestErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{"terminated", ErrTerminated, "service terminated"},
		{"exit", ErrExit, "service exited"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.err.Error())
			assert.True(t, errors.Is(tt.err, tt.err))
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
				Service: nil,
				Config:  LifecycleConfig{},
			},
			wantErr: false,
		},
		{
			name: "minimal entry",
			entry: Entry{
				Service: nil,
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
		})
	}
}

func TestErrorInterface(t *testing.T) {
	t.Run("ErrTerminated", func(t *testing.T) {
		err := ErrTerminated
		assert.Equal(t, "service terminated", err.Error())
		assert.Equal(t, KindTerminated, err.Kind())
		assert.Equal(t, apierror.False, err.Retryable())
		assert.Nil(t, err.Details())
	})

	t.Run("ErrExit", func(t *testing.T) {
		err := ErrExit
		assert.Equal(t, "service exited", err.Error())
		assert.Equal(t, KindExited, err.Kind())
		assert.Equal(t, apierror.False, err.Retryable())
	})

	t.Run("ErrStartTimeout", func(t *testing.T) {
		err := ErrStartTimeout
		assert.Equal(t, "service start timed out", err.Error())
		assert.Equal(t, apierror.KindTimeout, err.Kind())
		assert.Equal(t, apierror.True, err.Retryable())
	})

	t.Run("ErrOutsideTransaction", func(t *testing.T) {
		err := ErrOutsideTransaction
		assert.Equal(t, "action received outside of transaction", err.Error())
		assert.Equal(t, apierror.KindInvalid, err.Kind())
		assert.Equal(t, apierror.False, err.Retryable())
	})
}

func TestErrorMethods(t *testing.T) {
	t.Run("SetCause", func(t *testing.T) {
		cause := errors.New("underlying cause")
		err := apierror.SetCause(ErrTerminated, cause)
		assert.Equal(t, "service terminated", err.Error())
		assert.True(t, errors.Is(err, cause))
	})

	t.Run("SetMessage", func(t *testing.T) {
		err := apierror.SetMessage(ErrTerminated, "custom message")
		assert.Equal(t, "custom message", err.Error())
		assert.Equal(t, KindTerminated, err.Kind())
	})
}

func TestErrorConstructors(t *testing.T) {
	cause := errors.New("test cause")

	t.Run("NewInvalidDurationError", func(t *testing.T) {
		err := NewInvalidDurationError("timeout", cause)
		assert.Contains(t, err.Error(), "invalid")
		assert.Contains(t, err.Error(), "timeout")
		assert.Equal(t, apierror.KindInvalid, err.Kind())
		assert.True(t, errors.Is(err, cause))
		val, ok := err.Details().Get("field")
		assert.True(t, ok)
		assert.Equal(t, "timeout", val)
	})

	t.Run("NewServiceNotFoundError", func(t *testing.T) {
		err := NewServiceNotFoundError("my-service")
		assert.Contains(t, err.Error(), "my-service")
		assert.Equal(t, apierror.KindNotFound, err.Kind())
		val, ok := err.Details().Get("service_id")
		assert.True(t, ok)
		assert.Equal(t, "my-service", val)
	})
}

func TestSupervisorContext(t *testing.T) {
	t.Run("GetSupervisor_NoAppContext", func(t *testing.T) {
		ctx := context.Background()
		sup := GetSupervisor(ctx)
		assert.Nil(t, sup)
	})

	t.Run("WithSupervisor_NoAppContext", func(t *testing.T) {
		ctx := context.Background()
		result := WithSupervisor(ctx, "mock-supervisor")
		assert.Equal(t, ctx, result)
	})

	t.Run("WithSupervisor_WithAppContext", func(t *testing.T) {
		appCtx := ctxapi.NewAppContext()
		ctx := ctxapi.WithAppContext(context.Background(), appCtx)

		result := WithSupervisor(ctx, "mock-supervisor")
		assert.Equal(t, ctx, result)

		sup := GetSupervisor(ctx)
		assert.Equal(t, "mock-supervisor", sup)
	})

	t.Run("WithSupervisor_Idempotent", func(t *testing.T) {
		appCtx := ctxapi.NewAppContext()
		ctx := ctxapi.WithAppContext(context.Background(), appCtx)

		WithSupervisor(ctx, "first-supervisor")
		WithSupervisor(ctx, "second-supervisor")

		sup := GetSupervisor(ctx)
		assert.Equal(t, "first-supervisor", sup)
	})
}

func TestShutdownContext(t *testing.T) {
	t.Run("GetExitCode_NoAppContext", func(t *testing.T) {
		ctx := context.Background()
		code := GetExitCode(ctx)
		assert.Equal(t, 0, code)
	})

	t.Run("GetExitCode_Default", func(t *testing.T) {
		appCtx := ctxapi.NewAppContext()
		ctx := ctxapi.WithAppContext(context.Background(), appCtx)

		code := GetExitCode(ctx)
		assert.Equal(t, 0, code)
	})

	t.Run("SetAndGetExitCode", func(t *testing.T) {
		appCtx := ctxapi.NewAppContext()
		ctx := ctxapi.WithAppContext(context.Background(), appCtx)

		setExitCode(ctx, 42)
		code := GetExitCode(ctx)
		assert.Equal(t, 42, code)
	})

	t.Run("SetSignalChannel_NoAppContext", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan os.Signal, 1)
		SetSignalChannel(ctx, ch)
	})

	t.Run("SetAndGetSignalChannel", func(t *testing.T) {
		appCtx := ctxapi.NewAppContext()
		ctx := ctxapi.WithAppContext(context.Background(), appCtx)

		ch := make(chan os.Signal, 1)
		SetSignalChannel(ctx, ch)

		retrieved := getSignalChannel(ctx)
		assert.NotNil(t, retrieved)
	})

	t.Run("getSignalChannel_NoAppContext", func(t *testing.T) {
		ctx := context.Background()
		ch := getSignalChannel(ctx)
		assert.Nil(t, ch)
	})

	t.Run("getSignalChannel_WrongType", func(t *testing.T) {
		appCtx := ctxapi.NewAppContext()
		ctx := ctxapi.WithAppContext(context.Background(), appCtx)

		appCtx.Update(&ctxapi.Key{Name: "supervisor.signalChannelCtxKey"}, "not a channel")

		ch := getSignalChannel(ctx)
		assert.Nil(t, ch)
	})

	t.Run("GetExitCode_WrongType", func(t *testing.T) {
		appCtx := ctxapi.NewAppContext()
		ctx := ctxapi.WithAppContext(context.Background(), appCtx)

		appCtx.Update(&ctxapi.Key{Name: "supervisor.exitCodeCtxKey"}, "not an int")

		code := GetExitCode(ctx)
		assert.Equal(t, 0, code)
	})

	t.Run("TriggerShutdown_WithChannel", func(t *testing.T) {
		appCtx := ctxapi.NewAppContext()
		ctx := ctxapi.WithAppContext(context.Background(), appCtx)

		ch := make(chan os.Signal, 1)
		SetSignalChannel(ctx, ch)

		TriggerShutdown(ctx, 1)

		assert.Equal(t, 1, GetExitCode(ctx))
		select {
		case sig := <-ch:
			assert.NotNil(t, sig)
		default:
			t.Fatal("expected signal to be sent")
		}
	})

	t.Run("TriggerShutdown_NoChannel", func(t *testing.T) {
		appCtx := ctxapi.NewAppContext()
		ctx := ctxapi.WithAppContext(context.Background(), appCtx)

		TriggerShutdown(ctx, 2)

		assert.Equal(t, 2, GetExitCode(ctx))
	})
}
