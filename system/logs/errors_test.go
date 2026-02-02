package logs

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
)

func TestSentinelErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      apierror.Error
		expected string
		kind     string
	}{
		{"ErrGetLoggingConfigTimeout", ErrGetLoggingConfigTimeout, "failed to get logging config", "Timeout"},
		{"ErrSetTempConfigTimeout", ErrSetTempConfigTimeout, "failed to set temporary config", "Timeout"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.err.Error())
			assert.Equal(t, tt.kind, tt.err.Kind().String())
			assert.True(t, tt.err.Retryable().Bool())
			assert.Nil(t, errors.Unwrap(tt.err))
			assert.Nil(t, tt.err.Details())
		})
	}
}

func TestErrorConstructors(t *testing.T) {
	cause := errors.New("test cause")

	t.Run("NewSubscriberError", func(t *testing.T) {
		err := NewSubscriberError(cause)
		assert.Contains(t, err.Error(), "failed to create subscriber")
		assert.Equal(t, "Internal", err.Kind().String())
		assert.True(t, err.Retryable().Bool())
		assert.Equal(t, cause, errors.Unwrap(err))
		require.NotNil(t, err.Details())
	})

	t.Run("NewConfigMismatchError", func(t *testing.T) {
		err := NewConfigMismatchError("requested", "got")
		assert.Contains(t, err.Error(), "config mismatch")
		assert.Equal(t, "Internal", err.Kind().String())
		details := err.Details()
		require.NotNil(t, details)
		req, _ := details.Get("requested")
		assert.Equal(t, "requested", req)
		g, _ := details.Get("got")
		assert.Equal(t, "got", g)
	})

	t.Run("NewGetLoggingConfigError", func(t *testing.T) {
		err := NewGetLoggingConfigError(cause)
		assert.Contains(t, err.Error(), "failed to get logging config")
		assert.Equal(t, cause, errors.Unwrap(err))
	})

	t.Run("NewSetTempConfigError", func(t *testing.T) {
		err := NewSetTempConfigError(cause)
		assert.Contains(t, err.Error(), "failed to set temporary config")
		assert.Equal(t, cause, errors.Unwrap(err))
	})
}
