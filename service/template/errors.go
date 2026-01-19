package template

import (
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
	ErrRenderFailed     = apierror.New(apierror.Internal, "template rendering failed").WithRetryable(apierror.False)
)

func NewUnsupportedKindError(kind string) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported entry kind").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"kind": kind}))
}

func NewDecodeConfigError(cause error) apierror.Error {
	apiErr := apierror.New(apierror.Invalid, "failed to decode template config").WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).WithCause(cause)
	}
	return apiErr
}

func NewSetConfigDecodeError(cause error) apierror.Error {
	apiErr := apierror.New(apierror.Invalid, "failed to decode set config").WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).WithCause(cause)
	}
	return apiErr
}

func NewTemplateExistsError(name, set string) apierror.Error {
	return apierror.New(apierror.AlreadyExists, "template already exists in set").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"template": name, "set": set}))
}

func NewCreateTemplateError(cause error) apierror.Error {
	apiErr := apierror.New(apierror.Internal, "failed to create template").WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).WithCause(cause)
	}
	return apiErr
}

func NewSetAlreadyExistsError(id string) apierror.Error {
	return apierror.New(apierror.AlreadyExists, "template set already exists").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

func NewCreateSetError(cause error) apierror.Error {
	apiErr := apierror.New(apierror.Internal, "failed to create template set").WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).WithCause(cause)
	}
	return apiErr
}

func NewRemoveTemplateError(cause error) apierror.Error {
	apiErr := apierror.New(apierror.Internal, "failed to remove template from source set").WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).WithCause(cause)
	}
	return apiErr
}

func NewAddTemplateError(cause error) apierror.Error {
	apiErr := apierror.New(apierror.Internal, "failed to add template to target set").WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).WithCause(cause)
	}
	return apiErr
}

func NewTemplateNameExistsError(name, set string) apierror.Error {
	return apierror.New(apierror.AlreadyExists, "template name already exists in set").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"template": name, "set": set}))
}

func NewRemoveOldTemplateError(cause error) apierror.Error {
	apiErr := apierror.New(apierror.Internal, "failed to remove old template").WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).WithCause(cause)
	}
	return apiErr
}

func NewAddTemplateWithNewNameError(cause error) apierror.Error {
	apiErr := apierror.New(apierror.Internal, "failed to add template with new name").WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).WithCause(cause)
	}
	return apiErr
}

func NewUpdateTemplateError(cause error) apierror.Error {
	apiErr := apierror.New(apierror.Internal, "failed to update template").WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).WithCause(cause)
	}
	return apiErr
}

func NewDeleteTemplateError(cause error) apierror.Error {
	apiErr := apierror.New(apierror.Internal, "failed to remove template").WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).WithCause(cause)
	}
	return apiErr
}

func NewUpdateSetError(cause error) apierror.Error {
	apiErr := apierror.New(apierror.Internal, "failed to update template set").WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).WithCause(cause)
	}
	return apiErr
}

func NewMigrateTemplateError(name string, cause error) apierror.Error {
	apiErr := apierror.New(apierror.Internal, "failed to migrate template").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"template": name})).
		WithCause(cause)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{
			"template": name,
			"cause":    cause.Error(),
		}))
	}
	return apiErr
}

func NewTemplateExistsInSetError(name string) apierror.Error {
	return apierror.New(apierror.AlreadyExists, "template already exists in set").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"template": name}))
}

func NewGetCompiledTemplateError(cause error) apierror.Error {
	apiErr := apierror.New(apierror.Internal, "failed to get compiled template").WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).WithCause(cause)
	}
	return apiErr
}

func NewUnmarshalPayloadError(cause error) apierror.Error {
	apiErr := apierror.New(apierror.Invalid, "failed to unmarshal payload").WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).WithCause(cause)
	}
	return apiErr
}

func NewUnsupportedAccessModeError(mode string) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported access mode").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"mode": mode}))
}

func NewSetNotFoundError(setID string) apierror.Error {
	return apierror.New(apierror.NotFound, "template set not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"set": setID})).
		WithCause(ErrSetNotFound)
}

func NewTemplateNotFoundError(templateID string) apierror.Error {
	return apierror.New(apierror.NotFound, "template not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"template": templateID})).
		WithCause(ErrTemplateNotFound)
}

func NewSetNotEmptyError(setID string, count int) apierror.Error {
	return apierror.New(apierror.Invalid, "set is not empty").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"set": setID, "template_count": count})).
		WithCause(ErrSetNotEmpty)
}

func NewRenderFailedError(cause error) apierror.Error {
	apiErr := apierror.New(apierror.Internal, "template render failed").
		WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
			WithCause(fmt.Errorf("%w: %w", ErrRenderFailed, cause))
	} else {
		apiErr = apiErr.WithCause(ErrRenderFailed)
	}
	return apiErr
}
