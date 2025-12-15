package supervisor

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

func TestError_Implements(_ *testing.T) {
	var _ error = ErrNoRelayNode
	var _ apierror.Error = ErrNoRelayNode
}

func TestSentinelErrors(t *testing.T) {
	tests := []struct {
		err     apierror.Error
		kind    apierror.Kind
		message string
	}{
		{ErrNoRelayNode, apierror.KindInternal, "no relay node in context"},
		{ErrNoTopology, apierror.KindInternal, "no topology in context"},
		{ErrNoProcessManager, apierror.KindInternal, "no process manager in context"},
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			assert.Equal(t, tt.kind, tt.err.Kind())
			assert.Equal(t, tt.message, tt.err.Error())
		})
	}
}

func TestErrorConstructors(t *testing.T) {
	cause := errors.New("cause")

	t.Run("newRegisterPIDError", func(t *testing.T) {
		err := newRegisterPIDError(cause)
		assert.Equal(t, apierror.KindInternal, err.Kind())
		assert.Contains(t, err.Error(), "register supervisor pid")
		assert.True(t, errors.Is(err, cause))
	})

	t.Run("newAttachRelayError", func(t *testing.T) {
		err := newAttachRelayError(cause)
		assert.Equal(t, apierror.KindInternal, err.Kind())
		assert.Contains(t, err.Error(), "attach to relay")
		assert.True(t, errors.Is(err, cause))
	})

	t.Run("newStartProcessError", func(t *testing.T) {
		err := newStartProcessError(cause)
		assert.Equal(t, apierror.KindInternal, err.Kind())
		assert.Contains(t, err.Error(), "start process")
		assert.True(t, errors.Is(err, cause))
	})

	t.Run("newDecodeConfigError", func(t *testing.T) {
		err := newDecodeConfigError(cause)
		assert.Equal(t, apierror.KindInvalid, err.Kind())
		assert.Contains(t, err.Error(), "decode config")
		assert.Equal(t, apierror.False, err.Retryable())
		assert.True(t, errors.Is(err, cause))
	})

	t.Run("newInvalidEntryKindError", func(t *testing.T) {
		err := newInvalidEntryKindError("got", "expected")
		assert.Equal(t, apierror.KindInvalid, err.Kind())
		assert.Equal(t, "invalid entry kind", err.Error())
		assert.Equal(t, apierror.False, err.Retryable())
		details := err.Details().(attrs.Bag)
		assert.Equal(t, "got", details["got"])
		assert.Equal(t, "expected", details["expected"])
	})

	t.Run("newServiceNotFoundError", func(t *testing.T) {
		err := newServiceNotFoundError("svc-id")
		assert.Equal(t, apierror.KindNotFound, err.Kind())
		assert.Equal(t, "service not found", err.Error())
		assert.Equal(t, apierror.False, err.Retryable())
		details := err.Details().(attrs.Bag)
		assert.Equal(t, "svc-id", details["id"])
	})

	t.Run("newSendCancelError", func(t *testing.T) {
		err := newSendCancelError(cause)
		assert.Equal(t, apierror.KindInternal, err.Kind())
		assert.Contains(t, err.Error(), "send cancel")
		assert.True(t, errors.Is(err, cause))
	})
}
