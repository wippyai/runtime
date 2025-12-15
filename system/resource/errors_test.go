package resource

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	apierror "github.com/wippyai/runtime/api/error"
)

func TestNewSubscriberError(t *testing.T) {
	cause := errors.New("connection failed")
	err := NewSubscriberError(cause)

	assert.Contains(t, err.Error(), "failed to create subscriber")
	assert.Contains(t, err.Error(), "connection failed")
	assert.Equal(t, apierror.Internal, err.Kind())
	assert.Equal(t, apierror.True, err.Retryable())
	assert.True(t, errors.Is(err, cause))
	assert.NotNil(t, err.Details())

	causeVal, ok := err.Details().Get("cause")
	assert.True(t, ok)
	assert.Equal(t, "connection failed", causeVal)
}
