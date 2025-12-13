package template

import (
	"errors"
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Sentinel errors
var (
	ErrTemplateNotFound = errors.New("template not found")
	ErrSetNotFound      = errors.New("template set not found")
	ErrRenderFailed     = errors.New("template rendering failed")
	ErrSetNotEmpty      = errors.New("template set is not empty")
)

// Error is a structured error for template operations.
type Error struct {
	kind      apierror.Kind
	message   string
	retryable apierror.Ternary
	details   attrs.Attributes
	cause     error
}

func (e *Error) Error() string               { return e.message }
func (e *Error) Kind() apierror.Kind         { return e.kind }
func (e *Error) Retryable() apierror.Ternary { return e.retryable }
func (e *Error) Details() attrs.Attributes   { return e.details }
func (e *Error) Unwrap() error               { return e.cause }

// Validation errors
var (
	ErrEmptySource = &Error{
		kind:      apierror.KindInvalid,
		message:   "template source cannot be empty",
		retryable: apierror.False,
	}

	ErrEmptySetName = &Error{
		kind:      apierror.KindInvalid,
		message:   "template set name cannot be empty",
		retryable: apierror.False,
	}

	ErrEmptyDelimiters = &Error{
		kind:      apierror.KindInvalid,
		message:   "template delimiters cannot be empty",
		retryable: apierror.False,
	}

	ErrEmptyCommentDelimiters = &Error{
		kind:      apierror.KindInvalid,
		message:   "comment delimiters cannot be empty",
		retryable: apierror.False,
	}

	ErrConflictingDelimiters = &Error{
		kind:      apierror.KindInvalid,
		message:   "template and comment delimiters must be different",
		retryable: apierror.False,
	}

	ErrEmptyExtensions = &Error{
		kind:      apierror.KindInvalid,
		message:   "template extensions cannot be empty",
		retryable: apierror.False,
	}
)

// Error constructors

func NewUnsupportedKindError(kind string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   fmt.Sprintf("unsupported entry kind: %s", kind),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"kind": kind}),
	}
}

func NewDecodeConfigError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to decode template config",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewSetConfigDecodeError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to decode set config",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewTemplateExistsError(name, set string) *Error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   fmt.Sprintf("template %s already exists in set %s", name, set),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"template": name, "set": set}),
	}
}

func NewCreateTemplateError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to create template",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

func NewSetAlreadyExistsError(id string) *Error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   fmt.Sprintf("template set %s already exists", id),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"id": id}),
	}
}

func NewCreateSetError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to create template set",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

func NewRemoveTemplateError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to remove template from source set",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

func NewAddTemplateError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to add template to target set",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

func NewTemplateNameExistsError(name, set string) *Error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   fmt.Sprintf("template name %s already exists in set %s", name, set),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"template": name, "set": set}),
	}
}

func NewRemoveOldTemplateError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to remove old template",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

func NewAddTemplateWithNewNameError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to add template with new name",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

func NewUpdateTemplateError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to update template",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

func NewDeleteTemplateError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to remove template",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

func NewUpdateSetError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to update template set",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

func NewMigrateTemplateError(name string, cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   fmt.Sprintf("failed to migrate template %s", name),
		retryable: apierror.Unknown,
		details:   attrs.NewBagFrom(map[string]any{"template": name}),
		cause:     cause,
	}
}

func NewTemplateExistsInSetError(name string) *Error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   fmt.Sprintf("template %s already exists in set", name),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"template": name}),
	}
}

func NewGetCompiledTemplateError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to get compiled template",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

func NewUnmarshalPayloadError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to unmarshal payload",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewUnsupportedAccessModeError(mode string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   fmt.Sprintf("unsupported access mode: %s", mode),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"mode": mode}),
	}
}

func NewSetNotFoundError(setID string) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   fmt.Sprintf("template set not found: %s", setID),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"set": setID}),
		cause:     ErrSetNotFound,
	}
}

func NewTemplateNotFoundError(templateID string) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   fmt.Sprintf("template not found: %s", templateID),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"template": templateID}),
		cause:     ErrTemplateNotFound,
	}
}

func NewSetNotEmptyError(setID string, count int) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   fmt.Sprintf("set %s is not empty", setID),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"set": setID, "template_count": count}),
		cause:     ErrSetNotEmpty,
	}
}

func NewRenderFailedError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "template render failed",
		retryable: apierror.Unknown,
		cause:     fmt.Errorf("%w: %w", ErrRenderFailed, cause),
	}
}
