package processfunc

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
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

func TestError_Unwrap(t *testing.T) {
	cause := errors.New("root cause")
	err := &Error{cause: cause}
	assert.Equal(t, cause, err.Unwrap())
	assert.True(t, errors.Is(err, cause))
}

func TestErrMonitorChannelClosed(t *testing.T) {
	assert.Equal(t, apierror.KindInternal, ErrMonitorChannelClosed.Kind())
	assert.Equal(t, "monitor channel closed unexpectedly", ErrMonitorChannelClosed.Error())
}

func TestErrorConstructors(t *testing.T) {
	cause := errors.New("cause")

	t.Run("newRegisterPIDError", func(t *testing.T) {
		err := newRegisterPIDError(cause)
		assert.Equal(t, apierror.KindInternal, err.Kind())
		assert.Equal(t, "register caller pid: cause", err.Error())
		assert.Equal(t, cause, err.Unwrap())
	})

	t.Run("newAttachRelayError", func(t *testing.T) {
		err := newAttachRelayError(cause)
		assert.Equal(t, apierror.KindInternal, err.Kind())
		assert.Equal(t, "attach to relay: cause", err.Error())
		assert.Equal(t, cause, err.Unwrap())
	})

	t.Run("newStartProcessError", func(t *testing.T) {
		err := newStartProcessError(cause)
		assert.Equal(t, apierror.KindInternal, err.Kind())
		assert.Equal(t, "start process: cause", err.Error())
		assert.Equal(t, cause, err.Unwrap())
	})
}
