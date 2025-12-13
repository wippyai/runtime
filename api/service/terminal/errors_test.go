package terminal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	apierror "github.com/wippyai/runtime/api/error"
)

func TestSentinelErrors(t *testing.T) {
	assert.EqualError(t, ErrHostNotRunning, "host is not running")
	assert.EqualError(t, ErrHostShuttingDown, "host is shutting down")
	assert.EqualError(t, ErrHostAlreadyRunning, "host already running")
}

func TestNewDecodeConfigError(t *testing.T) {
	cause := assert.AnError
	err := NewDecodeConfigError(cause)

	assert.Equal(t, "failed to decode terminal config", err.Error())
	assert.Equal(t, apierror.KindInvalid, err.Kind())
	assert.Equal(t, apierror.False, err.Retryable())
	assert.Nil(t, err.Details())
	assert.Equal(t, cause, err.Unwrap())
}

func TestError_Implements(t *testing.T) {
	var _ error = (*Error)(nil)
	var _ apierror.Error = (*Error)(nil)
}
