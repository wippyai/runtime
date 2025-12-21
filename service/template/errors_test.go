package template

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
)

func TestNewUnsupportedKindError(t *testing.T) {
	err := NewUnsupportedKindError("unknown")
	require.NotNil(t, err)
	assert.Equal(t, apierror.Invalid, err.Kind())
	assert.Contains(t, err.Error(), "unsupported entry kind")
	assert.Contains(t, err.Error(), "unknown")

	details := err.Details()
	require.NotNil(t, details)
	assert.Equal(t, "unknown", details.GetString("kind", ""))
}

func TestNewDecodeConfigError(t *testing.T) {
	cause := errors.New("json error")
	err := NewDecodeConfigError(cause)
	require.NotNil(t, err)
	assert.Equal(t, apierror.Invalid, err.Kind())
	assert.Contains(t, err.Error(), "failed to decode template config")
	assert.True(t, errors.Is(err, cause))
}

func TestNewSetConfigDecodeError(t *testing.T) {
	cause := errors.New("yaml error")
	err := NewSetConfigDecodeError(cause)
	require.NotNil(t, err)
	assert.Equal(t, apierror.Invalid, err.Kind())
	assert.Contains(t, err.Error(), "failed to decode set config")
}

func TestNewTemplateExistsError(t *testing.T) {
	err := NewTemplateExistsError("header.html", "emails")
	require.NotNil(t, err)
	assert.Equal(t, apierror.AlreadyExists, err.Kind())
	assert.Contains(t, err.Error(), "header.html")
	assert.Contains(t, err.Error(), "emails")

	details := err.Details()
	require.NotNil(t, details)
	assert.Equal(t, "header.html", details.GetString("template", ""))
	assert.Equal(t, "emails", details.GetString("set", ""))
}

func TestNewCreateTemplateError(t *testing.T) {
	cause := errors.New("io error")
	err := NewCreateTemplateError(cause)
	require.NotNil(t, err)
	assert.Equal(t, apierror.Internal, err.Kind())
	assert.Contains(t, err.Error(), "failed to create template")
}

func TestNewSetAlreadyExistsError(t *testing.T) {
	err := NewSetAlreadyExistsError("main-templates")
	require.NotNil(t, err)
	assert.Equal(t, apierror.AlreadyExists, err.Kind())
	assert.Contains(t, err.Error(), "main-templates")

	details := err.Details()
	require.NotNil(t, details)
	assert.Equal(t, "main-templates", details.GetString("id", ""))
}

func TestNewCreateSetError(t *testing.T) {
	cause := errors.New("db error")
	err := NewCreateSetError(cause)
	require.NotNil(t, err)
	assert.Equal(t, apierror.Internal, err.Kind())
	assert.Contains(t, err.Error(), "failed to create template set")
}

func TestNewRemoveTemplateError(t *testing.T) {
	cause := errors.New("not found")
	err := NewRemoveTemplateError(cause)
	require.NotNil(t, err)
	assert.Equal(t, apierror.Internal, err.Kind())
	assert.Contains(t, err.Error(), "failed to remove template from source set")
}

func TestNewAddTemplateError(t *testing.T) {
	cause := errors.New("conflict")
	err := NewAddTemplateError(cause)
	require.NotNil(t, err)
	assert.Equal(t, apierror.Internal, err.Kind())
	assert.Contains(t, err.Error(), "failed to add template to target set")
}

func TestNewTemplateNameExistsError(t *testing.T) {
	err := NewTemplateNameExistsError("footer", "common")
	require.NotNil(t, err)
	assert.Equal(t, apierror.AlreadyExists, err.Kind())
	assert.Contains(t, err.Error(), "footer")
	assert.Contains(t, err.Error(), "common")

	details := err.Details()
	require.NotNil(t, details)
	assert.Equal(t, "footer", details.GetString("template", ""))
	assert.Equal(t, "common", details.GetString("set", ""))
}

func TestNewRemoveOldTemplateError(t *testing.T) {
	cause := errors.New("locked")
	err := NewRemoveOldTemplateError(cause)
	require.NotNil(t, err)
	assert.Equal(t, apierror.Internal, err.Kind())
	assert.Contains(t, err.Error(), "failed to remove old template")
}

func TestNewAddTemplateWithNewNameError(t *testing.T) {
	cause := errors.New("validation error")
	err := NewAddTemplateWithNewNameError(cause)
	require.NotNil(t, err)
	assert.Equal(t, apierror.Internal, err.Kind())
	assert.Contains(t, err.Error(), "failed to add template with new name")
}

func TestNewUpdateTemplateError(t *testing.T) {
	cause := errors.New("update failed")
	err := NewUpdateTemplateError(cause)
	require.NotNil(t, err)
	assert.Equal(t, apierror.Internal, err.Kind())
	assert.Contains(t, err.Error(), "failed to update template")
}

func TestNewDeleteTemplateError(t *testing.T) {
	cause := errors.New("delete failed")
	err := NewDeleteTemplateError(cause)
	require.NotNil(t, err)
	assert.Equal(t, apierror.Internal, err.Kind())
	assert.Contains(t, err.Error(), "failed to remove template")
}

func TestNewUpdateSetError(t *testing.T) {
	cause := errors.New("set update failed")
	err := NewUpdateSetError(cause)
	require.NotNil(t, err)
	assert.Equal(t, apierror.Internal, err.Kind())
	assert.Contains(t, err.Error(), "failed to update template set")
}

func TestNewMigrateTemplateError(t *testing.T) {
	cause := errors.New("migration error")
	err := NewMigrateTemplateError("old-template", cause)
	require.NotNil(t, err)
	assert.Equal(t, apierror.Internal, err.Kind())
	assert.Contains(t, err.Error(), "old-template")
	assert.Contains(t, err.Error(), "failed to migrate template")

	details := err.Details()
	require.NotNil(t, details)
	assert.Equal(t, "old-template", details.GetString("template", ""))
}

func TestNewTemplateExistsInSetError(t *testing.T) {
	err := NewTemplateExistsInSetError("duplicate")
	require.NotNil(t, err)
	assert.Equal(t, apierror.AlreadyExists, err.Kind())
	assert.Contains(t, err.Error(), "duplicate")
	assert.Contains(t, err.Error(), "already exists in set")

	details := err.Details()
	require.NotNil(t, details)
	assert.Equal(t, "duplicate", details.GetString("template", ""))
}

func TestNewGetCompiledTemplateError(t *testing.T) {
	cause := errors.New("compile error")
	err := NewGetCompiledTemplateError(cause)
	require.NotNil(t, err)
	assert.Equal(t, apierror.Internal, err.Kind())
	assert.Contains(t, err.Error(), "failed to get compiled template")
}

func TestNewUnmarshalPayloadError(t *testing.T) {
	cause := errors.New("json unmarshal error")
	err := NewUnmarshalPayloadError(cause)
	require.NotNil(t, err)
	assert.Equal(t, apierror.Invalid, err.Kind())
	assert.Contains(t, err.Error(), "failed to unmarshal payload")
}

func TestNewUnsupportedAccessModeError(t *testing.T) {
	err := NewUnsupportedAccessModeError("write-only")
	require.NotNil(t, err)
	assert.Equal(t, apierror.Invalid, err.Kind())
	assert.Contains(t, err.Error(), "unsupported access mode")
	assert.Contains(t, err.Error(), "write-only")

	details := err.Details()
	require.NotNil(t, details)
	assert.Equal(t, "write-only", details.GetString("mode", ""))
}

func TestNewSetNotFoundError(t *testing.T) {
	err := NewSetNotFoundError("missing-set")
	require.NotNil(t, err)
	assert.Equal(t, apierror.NotFound, err.Kind())
	assert.Contains(t, err.Error(), "missing-set")
	assert.True(t, errors.Is(err, ErrSetNotFound))

	details := err.Details()
	require.NotNil(t, details)
	assert.Equal(t, "missing-set", details.GetString("set", ""))
}

func TestNewTemplateNotFoundError(t *testing.T) {
	err := NewTemplateNotFoundError("missing-template")
	require.NotNil(t, err)
	assert.Equal(t, apierror.NotFound, err.Kind())
	assert.Contains(t, err.Error(), "missing-template")
	assert.True(t, errors.Is(err, ErrTemplateNotFound))

	details := err.Details()
	require.NotNil(t, details)
	assert.Equal(t, "missing-template", details.GetString("template", ""))
}

func TestNewSetNotEmptyError(t *testing.T) {
	err := NewSetNotEmptyError("active-set", 5)
	require.NotNil(t, err)
	assert.Equal(t, apierror.Invalid, err.Kind())
	assert.Contains(t, err.Error(), "active-set")
	assert.Contains(t, err.Error(), "not empty")
	assert.True(t, errors.Is(err, ErrSetNotEmpty))

	details := err.Details()
	require.NotNil(t, details)
	assert.Equal(t, "active-set", details.GetString("set", ""))
	assert.Equal(t, 5, details.GetInt("template_count", 0))
}

func TestNewRenderFailedError(t *testing.T) {
	cause := errors.New("syntax error in template")
	err := NewRenderFailedError(cause)
	require.NotNil(t, err)
	assert.Equal(t, apierror.Internal, err.Kind())
	assert.Contains(t, err.Error(), "template render failed")
	assert.True(t, errors.Is(err, ErrRenderFailed))
}

func TestSentinelErrors(t *testing.T) {
	assert.NotNil(t, ErrTemplateNotFound)
	assert.NotNil(t, ErrSetNotFound)
	assert.NotNil(t, ErrSetNotEmpty)
	assert.NotNil(t, ErrRenderFailed)
}
