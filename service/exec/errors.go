package exec

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

func NewUnsupportedEntryKindError(kind registry.Kind) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported entry kind").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"kind": kind}))
}

func NewExecutorAlreadyExistsError(id string) apierror.Error {
	return apierror.New(apierror.AlreadyExists, "executor already exists").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"executor_id": id}))
}

func NewExecutorNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.NotFound, "executor not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"executor_id": id}))
}

func NewConfigDecodeError(err error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to decode configuration").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewExecutorCreateError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create executor").
		WithRetryable(apierror.True).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}
