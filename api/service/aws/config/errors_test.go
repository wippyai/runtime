package config

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	apierror "github.com/wippyai/runtime/api/error"
)

func TestError_Interface(t *testing.T) {
	err := NewUnsupportedKindError("test-kind")

	// Check it implements error interface
	var _ error = err

	e := err.(*Error)
	assert.Equal(t, "unsupported entry kind: test-kind", e.Error())
	assert.Equal(t, apierror.KindInvalid, e.Kind())
	assert.Equal(t, apierror.False, e.Retryable())
	assert.NotNil(t, e.Details())
	assert.Equal(t, "test-kind", e.Details().GetString("kind", ""))
	assert.Nil(t, e.Unwrap())
}

func TestNewUnsupportedKindError(t *testing.T) {
	err := NewUnsupportedKindError("invalid.kind")
	e := err.(*Error)

	assert.Equal(t, apierror.KindInvalid, e.Kind())
	assert.Contains(t, e.Error(), "invalid.kind")
}

func TestNewConfigAlreadyExistsError(t *testing.T) {
	err := NewConfigAlreadyExistsError("my-config")
	e := err.(*Error)

	assert.Equal(t, apierror.KindAlreadyExists, e.Kind())
	assert.Contains(t, e.Error(), "my-config")
	assert.Contains(t, e.Error(), "already exists")
}

func TestNewDecodeConfigError(t *testing.T) {
	cause := errors.New("invalid json")
	err := NewDecodeConfigError(cause)
	e := err.(*Error)

	assert.Equal(t, apierror.KindInvalid, e.Kind())
	assert.Equal(t, apierror.False, e.Retryable())
	assert.Equal(t, cause, e.Unwrap())
}

func TestNewCreateAWSConfigError(t *testing.T) {
	cause := errors.New("aws error")
	err := NewCreateAWSConfigError(cause)
	e := err.(*Error)

	assert.Equal(t, apierror.KindInternal, e.Kind())
	assert.Equal(t, apierror.Unknown, e.Retryable())
	assert.Equal(t, cause, e.Unwrap())
}

func TestNewConfigNotFoundError(t *testing.T) {
	err := NewConfigNotFoundError("missing-config")
	e := err.(*Error)

	assert.Equal(t, apierror.KindNotFound, e.Kind())
	assert.Contains(t, e.Error(), "missing-config")
	assert.Contains(t, e.Error(), "not found")
}
