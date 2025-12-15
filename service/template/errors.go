package template

import (
	"errors"
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrTemplateNotFound = errors.New("template not found")
	ErrSetNotFound      = errors.New("template set not found")
	ErrRenderFailed     = errors.New("template rendering failed")
	ErrSetNotEmpty      = errors.New("template set is not empty")
)

var (
	ErrEmptySource = apierror.New(apierror.KindInvalid, "template source cannot be empty").WithRetryable(apierror.False)

	ErrEmptySetName = apierror.New(apierror.KindInvalid, "template set name cannot be empty").WithRetryable(apierror.False)

	ErrEmptyDelimiters = apierror.New(apierror.KindInvalid, "template delimiters cannot be empty").WithRetryable(apierror.False)

	ErrEmptyCommentDelimiters = apierror.New(apierror.KindInvalid, "comment delimiters cannot be empty").WithRetryable(apierror.False)

	ErrConflictingDelimiters = apierror.New(apierror.KindInvalid, "template and comment delimiters must be different").WithRetryable(apierror.False)

	ErrEmptyExtensions = apierror.New(apierror.KindInvalid, "template extensions cannot be empty").WithRetryable(apierror.False)
)

func NewUnsupportedKindError(kind string) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("unsupported entry kind: %s", kind)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"kind": kind}))
}

func NewDecodeConfigError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "failed to decode template config").WithCause(cause).WithRetryable(apierror.False)
}

func NewSetConfigDecodeError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "failed to decode set config").WithCause(cause).WithRetryable(apierror.False)
}

func NewTemplateExistsError(name, set string) apierror.Error {
	return apierror.New(apierror.KindAlreadyExists, fmt.Sprintf("template %s already exists in set %s", name, set)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"template": name, "set": set}))
}

func NewCreateTemplateError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to create template").WithCause(cause).WithRetryable(apierror.Unknown)
}

func NewSetAlreadyExistsError(id string) apierror.Error {
	return apierror.New(apierror.KindAlreadyExists, fmt.Sprintf("template set %s already exists", id)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

func NewCreateSetError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to create template set").WithCause(cause).WithRetryable(apierror.Unknown)
}

func NewRemoveTemplateError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to remove template from source set").WithCause(cause).WithRetryable(apierror.Unknown)
}

func NewAddTemplateError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to add template to target set").WithCause(cause).WithRetryable(apierror.Unknown)
}

func NewTemplateNameExistsError(name, set string) apierror.Error {
	return apierror.New(apierror.KindAlreadyExists, fmt.Sprintf("template name %s already exists in set %s", name, set)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"template": name, "set": set}))
}

func NewRemoveOldTemplateError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to remove old template").WithCause(cause).WithRetryable(apierror.Unknown)
}

func NewAddTemplateWithNewNameError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to add template with new name").WithCause(cause).WithRetryable(apierror.Unknown)
}

func NewUpdateTemplateError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to update template").WithCause(cause).WithRetryable(apierror.Unknown)
}

func NewDeleteTemplateError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to remove template").WithCause(cause).WithRetryable(apierror.Unknown)
}

func NewUpdateSetError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to update template set").WithCause(cause).WithRetryable(apierror.Unknown)
}

func NewMigrateTemplateError(name string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, fmt.Sprintf("failed to migrate template %s", name)).
		WithRetryable(apierror.Unknown).
		WithDetails(attrs.NewBagFrom(map[string]any{"template": name})).
		WithCause(cause)
}

func NewTemplateExistsInSetError(name string) apierror.Error {
	return apierror.New(apierror.KindAlreadyExists, fmt.Sprintf("template %s already exists in set", name)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"template": name}))
}

func NewGetCompiledTemplateError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to get compiled template").WithCause(cause).WithRetryable(apierror.Unknown)
}

func NewUnmarshalPayloadError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "failed to unmarshal payload").WithCause(cause).WithRetryable(apierror.False)
}

func NewUnsupportedAccessModeError(mode string) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("unsupported access mode: %s", mode)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"mode": mode}))
}

func NewSetNotFoundError(setID string) apierror.Error {
	return apierror.New(apierror.KindNotFound, fmt.Sprintf("template set not found: %s", setID)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"set": setID})).
		WithCause(ErrSetNotFound)
}

func NewTemplateNotFoundError(templateID string) apierror.Error {
	return apierror.New(apierror.KindNotFound, fmt.Sprintf("template not found: %s", templateID)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"template": templateID})).
		WithCause(ErrTemplateNotFound)
}

func NewSetNotEmptyError(setID string, count int) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("set %s is not empty", setID)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"set": setID, "template_count": count})).
		WithCause(ErrSetNotEmpty)
}

func NewRenderFailedError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "template render failed").
		WithRetryable(apierror.Unknown).
		WithCause(fmt.Errorf("%w: %w", ErrRenderFailed, cause))
}
