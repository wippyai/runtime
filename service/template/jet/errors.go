package jet

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

type StructuredError struct {
	kind      apierror.Kind
	message   string
	retryable apierror.Ternary
	details   attrs.Attributes
	cause     error
}

func (e *StructuredError) Error() string               { return e.message }
func (e *StructuredError) Kind() apierror.Kind         { return e.kind }
func (e *StructuredError) Retryable() apierror.Ternary { return e.retryable }
func (e *StructuredError) Details() attrs.Attributes   { return e.details }
func (e *StructuredError) Unwrap() error               { return e.cause }

func newUnsupportedKindError(kind string) error {
	return &StructuredError{
		kind:      apierror.KindInvalid,
		message:   "unsupported entry kind: " + kind,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"kind": kind}),
	}
}

func newDecodeConfigError(cause error) error {
	return &StructuredError{
		kind:      apierror.KindInvalid,
		message:   "failed to decode template config",
		retryable: apierror.False,
		cause:     cause,
	}
}

func newSetConfigDecodeError(cause error) error {
	return &StructuredError{
		kind:      apierror.KindInvalid,
		message:   "failed to decode set config",
		retryable: apierror.False,
		cause:     cause,
	}
}

func newTemplateExistsError(name, set string) error {
	return &StructuredError{
		kind:      apierror.KindAlreadyExists,
		message:   "template " + name + " already exists in set " + set,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"template": name, "set": set}),
	}
}

func newCreateTemplateError(cause error) error {
	return &StructuredError{
		kind:      apierror.KindInternal,
		message:   "failed to create template",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

func newSetAlreadyExistsError(id string) error {
	return &StructuredError{
		kind:      apierror.KindAlreadyExists,
		message:   "template set " + id + " already exists",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"id": id}),
	}
}

func newCreateSetError(cause error) error {
	return &StructuredError{
		kind:      apierror.KindInternal,
		message:   "failed to create template set",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

func newRemoveTemplateError(cause error) error {
	return &StructuredError{
		kind:      apierror.KindInternal,
		message:   "failed to remove template from source set",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

func newAddTemplateError(cause error) error {
	return &StructuredError{
		kind:      apierror.KindInternal,
		message:   "failed to add template to target set",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

func newTemplateNameExistsError(name, set string) error {
	return &StructuredError{
		kind:      apierror.KindAlreadyExists,
		message:   "template name " + name + " already exists in set " + set,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"template": name, "set": set}),
	}
}

func newRemoveOldTemplateError(cause error) error {
	return &StructuredError{
		kind:      apierror.KindInternal,
		message:   "failed to remove old template",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

func newAddTemplateWithNewNameError(cause error) error {
	return &StructuredError{
		kind:      apierror.KindInternal,
		message:   "failed to add template with new name",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

func newUpdateTemplateError(cause error) error {
	return &StructuredError{
		kind:      apierror.KindInternal,
		message:   "failed to update template",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

func newDeleteTemplateError(cause error) error {
	return &StructuredError{
		kind:      apierror.KindInternal,
		message:   "failed to remove template",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

func newUpdateSetError(cause error) error {
	return &StructuredError{
		kind:      apierror.KindInternal,
		message:   "failed to update template set",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

func newMigrateTemplateError(name string, cause error) error {
	return &StructuredError{
		kind:      apierror.KindInternal,
		message:   "failed to migrate template " + name,
		retryable: apierror.Unknown,
		details:   attrs.NewBagFrom(map[string]any{"template": name}),
		cause:     cause,
	}
}

func newTemplateExistsInSetError(name string) error {
	return &StructuredError{
		kind:      apierror.KindAlreadyExists,
		message:   "template " + name + " already exists in set",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"template": name}),
	}
}

func newGetCompiledTemplateError(cause error) error {
	return &StructuredError{
		kind:      apierror.KindInternal,
		message:   "failed to get compiled template",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

func newUnmarshalPayloadError(cause error) error {
	return &StructuredError{
		kind:      apierror.KindInvalid,
		message:   "failed to unmarshal payload",
		retryable: apierror.False,
		cause:     cause,
	}
}

func newUnsupportedAccessModeError(mode string) error {
	return &StructuredError{
		kind:      apierror.KindInvalid,
		message:   "unsupported access mode: " + mode,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"mode": mode}),
	}
}

func newSetNotFoundError(setID string) error {
	return &StructuredError{
		kind:      apierror.KindNotFound,
		message:   "template set not found: " + setID,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"set": setID}),
	}
}

func newTemplateNotFoundError(templateID string) error {
	return &StructuredError{
		kind:      apierror.KindNotFound,
		message:   "template not found: " + templateID,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"template": templateID}),
	}
}

func newSetNotEmptyError(setID string, count int) error {
	return &StructuredError{
		kind:      apierror.KindInvalid,
		message:   "set " + setID + " is not empty",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"set": setID, "template_count": count}),
	}
}

func newRenderFailedError(cause error) error {
	return &StructuredError{
		kind:      apierror.KindInternal,
		message:   "template render failed",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}
