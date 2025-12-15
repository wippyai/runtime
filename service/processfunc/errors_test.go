package processfunc

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	apierror "github.com/wippyai/runtime/api/error"
)

func TestError_Implements(_ *testing.T) {
	var _ error = ErrMonitorChannelClosed
	var _ apierror.Error = ErrMonitorChannelClosed
}

func TestErrMonitorChannelClosed(t *testing.T) {
	assert.Equal(t, apierror.Internal, ErrMonitorChannelClosed.Kind())
	assert.Equal(t, "monitor channel closed unexpectedly", ErrMonitorChannelClosed.Error())
}

func TestErrorConstructors(t *testing.T) {
	cause := errors.New("cause")

	t.Run("newRegisterPIDError", func(t *testing.T) {
		err := newRegisterPIDError(cause)
		assert.Equal(t, apierror.Internal, err.Kind())
		assert.Contains(t, err.Error(), "register caller pid")
		assert.True(t, errors.Is(err, cause))
	})

	t.Run("newAttachRelayError", func(t *testing.T) {
		err := newAttachRelayError(cause)
		assert.Equal(t, apierror.Internal, err.Kind())
		assert.Contains(t, err.Error(), "attach to relay")
		assert.True(t, errors.Is(err, cause))
	})

	t.Run("newStartProcessError", func(t *testing.T) {
		err := newStartProcessError(cause)
		assert.Equal(t, apierror.Internal, err.Kind())
		assert.Contains(t, err.Error(), "start process")
		assert.True(t, errors.Is(err, cause))
	})
}
