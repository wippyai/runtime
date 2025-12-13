package template

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
)

func TestSentinelErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		msg  string
	}{
		{"ErrTemplateNotFound", ErrTemplateNotFound, "template not found"},
		{"ErrSetNotFound", ErrSetNotFound, "template set not found"},
		{"ErrRenderFailed", ErrRenderFailed, "template rendering failed"},
		{"ErrSetNotEmpty", ErrSetNotEmpty, "template set is not empty"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.msg, tc.err.Error())
		})
	}
}

func TestValidationErrors(t *testing.T) {
	tests := []struct {
		name      string
		err       *Error
		msg       string
		kind      apierror.Kind
		retryable apierror.Ternary
	}{
		{"ErrEmptySource", ErrEmptySource, "template source cannot be empty", apierror.KindInvalid, apierror.False},
		{"ErrEmptySetName", ErrEmptySetName, "template set name cannot be empty", apierror.KindInvalid, apierror.False},
		{"ErrEmptyDelimiters", ErrEmptyDelimiters, "template delimiters cannot be empty", apierror.KindInvalid, apierror.False},
		{"ErrEmptyCommentDelimiters", ErrEmptyCommentDelimiters, "comment delimiters cannot be empty", apierror.KindInvalid, apierror.False},
		{"ErrConflictingDelimiters", ErrConflictingDelimiters, "template and comment delimiters must be different", apierror.KindInvalid, apierror.False},
		{"ErrEmptyExtensions", ErrEmptyExtensions, "template extensions cannot be empty", apierror.KindInvalid, apierror.False},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.msg, tc.err.Error())
			assert.Equal(t, tc.kind, tc.err.Kind())
			assert.Equal(t, tc.retryable, tc.err.Retryable())
		})
	}
}

func TestNewUnsupportedKindError(t *testing.T) {
	err := NewUnsupportedKindError("unknown.kind")
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "unknown.kind")
	assert.Equal(t, apierror.KindInvalid, err.Kind())
	assert.Equal(t, apierror.False, err.Retryable())
	assert.Equal(t, "unknown.kind", err.Details().GetString("kind", ""))
}

func TestNewDecodeConfigError(t *testing.T) {
	cause := errors.New("decode failed")
	err := NewDecodeConfigError(cause)
	require.NotNil(t, err)
	assert.Equal(t, "failed to decode template config", err.Error())
	assert.Equal(t, apierror.KindInvalid, err.Kind())
	assert.ErrorIs(t, err, cause)
}

func TestNewSetConfigDecodeError(t *testing.T) {
	cause := errors.New("set decode failed")
	err := NewSetConfigDecodeError(cause)
	require.NotNil(t, err)
	assert.Equal(t, "failed to decode set config", err.Error())
	assert.Equal(t, apierror.KindInvalid, err.Kind())
	assert.ErrorIs(t, err, cause)
}

func TestNewTemplateExistsError(t *testing.T) {
	err := NewTemplateExistsError("my-template", "my-set")
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "my-template")
	assert.Contains(t, err.Error(), "my-set")
	assert.Equal(t, apierror.KindAlreadyExists, err.Kind())
	assert.Equal(t, "my-template", err.Details().GetString("template", ""))
	assert.Equal(t, "my-set", err.Details().GetString("set", ""))
}

func TestNewCreateTemplateError(t *testing.T) {
	cause := errors.New("create failed")
	err := NewCreateTemplateError(cause)
	require.NotNil(t, err)
	assert.Equal(t, "failed to create template", err.Error())
	assert.Equal(t, apierror.KindInternal, err.Kind())
	assert.Equal(t, apierror.Unknown, err.Retryable())
	assert.ErrorIs(t, err, cause)
}

func TestNewSetAlreadyExistsError(t *testing.T) {
	err := NewSetAlreadyExistsError("set-123")
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "set-123")
	assert.Equal(t, apierror.KindAlreadyExists, err.Kind())
	assert.Equal(t, "set-123", err.Details().GetString("id", ""))
}

func TestNewSetNotFoundError(t *testing.T) {
	err := NewSetNotFoundError("missing-set")
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "missing-set")
	assert.Equal(t, apierror.KindNotFound, err.Kind())
	assert.ErrorIs(t, err, ErrSetNotFound)
}

func TestNewTemplateNotFoundError(t *testing.T) {
	err := NewTemplateNotFoundError("missing-template")
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "missing-template")
	assert.Equal(t, apierror.KindNotFound, err.Kind())
	assert.ErrorIs(t, err, ErrTemplateNotFound)
}

func TestNewSetNotEmptyError(t *testing.T) {
	err := NewSetNotEmptyError("my-set", 5)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "my-set")
	assert.Equal(t, apierror.KindInvalid, err.Kind())
	assert.Equal(t, 5, err.Details().GetInt("template_count", 0))
	assert.ErrorIs(t, err, ErrSetNotEmpty)
}

func TestNewRenderFailedError(t *testing.T) {
	cause := errors.New("render error")
	err := NewRenderFailedError(cause)
	require.NotNil(t, err)
	assert.Equal(t, "template render failed", err.Error())
	assert.Equal(t, apierror.KindInternal, err.Kind())
	assert.ErrorIs(t, err, ErrRenderFailed)
}

func TestNewUnsupportedAccessModeError(t *testing.T) {
	err := NewUnsupportedAccessModeError("exclusive")
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "exclusive")
	assert.Equal(t, apierror.KindInvalid, err.Kind())
	assert.Equal(t, "exclusive", err.Details().GetString("mode", ""))
}

func TestNewTemplateExistsInSetError(t *testing.T) {
	err := NewTemplateExistsInSetError("duplicate")
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "duplicate")
	assert.Equal(t, apierror.KindAlreadyExists, err.Kind())
}

func TestNewMigrateTemplateError(t *testing.T) {
	cause := errors.New("migration failed")
	err := NewMigrateTemplateError("my-template", cause)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "my-template")
	assert.Equal(t, apierror.KindInternal, err.Kind())
	assert.ErrorIs(t, err, cause)
}

func TestErrorInterface(t *testing.T) {
	err := NewTemplateNotFoundError("test")

	var apiErr apierror.Error
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, apierror.KindNotFound, apiErr.Kind())
}
