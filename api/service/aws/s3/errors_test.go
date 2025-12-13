package s3

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

func TestNewStorageAlreadyExistsError(t *testing.T) {
	err := NewStorageAlreadyExistsError("my-storage")
	e := err.(*Error)

	assert.Equal(t, apierror.KindAlreadyExists, e.Kind())
	assert.Contains(t, e.Error(), "my-storage")
	assert.Contains(t, e.Error(), "already exists")
}

func TestNewAddEntryError(t *testing.T) {
	cause := errors.New("underlying error")
	err := NewAddEntryError(cause)
	e := err.(*Error)

	assert.Equal(t, apierror.KindInternal, e.Kind())
	assert.Equal(t, apierror.Unknown, e.Retryable())
	assert.Equal(t, cause, e.Unwrap())
}

func TestNewStorageNotFoundError(t *testing.T) {
	err := NewStorageNotFoundError("missing-storage")
	e := err.(*Error)

	assert.Equal(t, apierror.KindNotFound, e.Kind())
	assert.Contains(t, e.Error(), "missing-storage")
	assert.Contains(t, e.Error(), "not found")
}

func TestNewUpdateEntryError(t *testing.T) {
	cause := errors.New("update failed")
	err := NewUpdateEntryError(cause)
	e := err.(*Error)

	assert.Equal(t, apierror.KindInternal, e.Kind())
	assert.Equal(t, cause, e.Unwrap())
}

func TestNewDecodeConfigError(t *testing.T) {
	cause := errors.New("invalid json")
	err := NewDecodeConfigError(cause)
	e := err.(*Error)

	assert.Equal(t, apierror.KindInvalid, e.Kind())
	assert.Equal(t, apierror.False, e.Retryable())
	assert.Equal(t, cause, e.Unwrap())
}

func TestNewAcquireResourceError(t *testing.T) {
	cause := errors.New("resource locked")
	err := NewAcquireResourceError(cause)
	e := err.(*Error)

	assert.Equal(t, apierror.KindInternal, e.Kind())
	assert.Equal(t, cause, e.Unwrap())
}

func TestNewGetConfigError(t *testing.T) {
	cause := errors.New("config missing")
	err := NewGetConfigError(cause)
	e := err.(*Error)

	assert.Equal(t, apierror.KindInternal, e.Kind())
	assert.Equal(t, cause, e.Unwrap())
}

func TestNewAWSConfigInvalidError(t *testing.T) {
	err := NewAWSConfigInvalidError()
	e := err.(*Error)

	assert.Equal(t, apierror.KindInternal, e.Kind())
	assert.Equal(t, apierror.False, e.Retryable())
	assert.Nil(t, e.Unwrap())
	assert.Contains(t, e.Error(), "aws config")
}
