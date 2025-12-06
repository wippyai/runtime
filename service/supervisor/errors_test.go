package supervisor

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

func TestError_Implements(_ *testing.T) {
	var _ error = (*Error)(nil)
	var _ apierror.Error = (*Error)(nil)
}

func TestError_Message(t *testing.T) {
	err := &Error{message: "test message"}
	assert.Equal(t, "test message", err.Error())
}

func TestError_Kind(t *testing.T) {
	err := &Error{kind: apierror.KindInternal}
	assert.Equal(t, apierror.KindInternal, err.Kind())
}

func TestError_Retryable(t *testing.T) {
	err := &Error{retryable: apierror.False}
	assert.Equal(t, apierror.False, err.Retryable())
}

func TestError_Details(t *testing.T) {
	details := attrs.Bag{"key": "value"}
	err := &Error{details: details}
	assert.Equal(t, details, err.Details())
}

func TestError_Unwrap(t *testing.T) {
	cause := errors.New("root cause")
	err := &Error{cause: cause}
	assert.Equal(t, cause, err.Unwrap())
	assert.True(t, errors.Is(err, cause))
}

func TestSentinelErrors(t *testing.T) {
	tests := []struct {
		err     *Error
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
		assert.Equal(t, "register supervisor pid", err.Error())
		assert.Equal(t, cause, err.Unwrap())
	})

	t.Run("newAttachRelayError", func(t *testing.T) {
		err := newAttachRelayError(cause)
		assert.Equal(t, apierror.KindInternal, err.Kind())
		assert.Equal(t, "attach to relay", err.Error())
		assert.Equal(t, cause, err.Unwrap())
	})

	t.Run("newStartProcessError", func(t *testing.T) {
		err := newStartProcessError(cause)
		assert.Equal(t, apierror.KindInternal, err.Kind())
		assert.Equal(t, "start process", err.Error())
		assert.Equal(t, cause, err.Unwrap())
	})

	t.Run("newDecodeConfigError", func(t *testing.T) {
		err := newDecodeConfigError(cause)
		assert.Equal(t, apierror.KindInvalid, err.Kind())
		assert.Equal(t, "decode config", err.Error())
		assert.Equal(t, apierror.False, err.Retryable())
		assert.Equal(t, cause, err.Unwrap())
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
}
