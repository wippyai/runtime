package template

import (
	"errors"
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	servicetemplate "github.com/wippyai/runtime/api/service/template"
)

// Re-export sentinel errors from API for package-local use.
var (
	ErrTemplateNotFound = servicetemplate.ErrTemplateNotFound
	ErrSetNotFound      = servicetemplate.ErrSetNotFound
	ErrSetNotEmpty      = servicetemplate.ErrSetNotEmpty
	ErrRenderFailed     = errors.New("template rendering failed")
)

func NewUnsupportedKindError(kind string) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("unsupported entry kind: %s", kind)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"kind": kind}))
}

func NewDecodeConfigError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to decode template config").WithCause(cause).WithRetryable(apierror.False)
}

func NewSetConfigDecodeError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to decode set config").WithCause(cause).WithRetryable(apierror.False)
}

func NewTemplateExistsError(name, set string) apierror.Error {
	return apierror.New(apierror.AlreadyExists, fmt.Sprintf("template %s already exists in set %s", name, set)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"template": name, "set": set}))
}

func NewCreateTemplateError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create template").WithCause(cause).WithRetryable(apierror.Unspecified)
}

func NewSetAlreadyExistsError(id string) apierror.Error {
	return apierror.New(apierror.AlreadyExists, fmt.Sprintf("template set %s already exists", id)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

func NewCreateSetError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create template set").WithCause(cause).WithRetryable(apierror.Unspecified)
}

func NewRemoveTemplateError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to remove template from source set").WithCause(cause).WithRetryable(apierror.Unspecified)
}

func NewAddTemplateError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to add template to target set").WithCause(cause).WithRetryable(apierror.Unspecified)
}

func NewTemplateNameExistsError(name, set string) apierror.Error {
	return apierror.New(apierror.AlreadyExists, fmt.Sprintf("template name %s already exists in set %s", name, set)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"template": name, "set": set}))
}

func NewRemoveOldTemplateError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to remove old template").WithCause(cause).WithRetryable(apierror.Unspecified)
}

func NewAddTemplateWithNewNameError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to add template with new name").WithCause(cause).WithRetryable(apierror.Unspecified)
}

func NewUpdateTemplateError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to update template").WithCause(cause).WithRetryable(apierror.Unspecified)
}

func NewDeleteTemplateError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to remove template").WithCause(cause).WithRetryable(apierror.Unspecified)
}

func NewUpdateSetError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to update template set").WithCause(cause).WithRetryable(apierror.Unspecified)
}

func NewMigrateTemplateError(name string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("failed to migrate template %s", name)).
		WithRetryable(apierror.Unspecified).
		WithDetails(attrs.NewBagFrom(map[string]any{"template": name})).
		WithCause(cause)
}

func NewTemplateExistsInSetError(name string) apierror.Error {
	return apierror.New(apierror.AlreadyExists, fmt.Sprintf("template %s already exists in set", name)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"template": name}))
}

func NewGetCompiledTemplateError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to get compiled template").WithCause(cause).WithRetryable(apierror.Unspecified)
}

func NewUnmarshalPayloadError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to unmarshal payload").WithCause(cause).WithRetryable(apierror.False)
}

func NewUnsupportedAccessModeError(mode string) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("unsupported access mode: %s", mode)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"mode": mode}))
}

func NewSetNotFoundError(setID string) apierror.Error {
	return apierror.New(apierror.NotFound, fmt.Sprintf("template set not found: %s", setID)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"set": setID})).
		WithCause(ErrSetNotFound)
}

func NewTemplateNotFoundError(templateID string) apierror.Error {
	return apierror.New(apierror.NotFound, fmt.Sprintf("template not found: %s", templateID)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"template": templateID})).
		WithCause(ErrTemplateNotFound)
}

func NewSetNotEmptyError(setID string, count int) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("set %s is not empty", setID)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"set": setID, "template_count": count})).
		WithCause(ErrSetNotEmpty)
}

func NewRenderFailedError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "template render failed").
		WithRetryable(apierror.Unspecified).
		WithCause(fmt.Errorf("%w: %w", ErrRenderFailed, cause))
}
