package exec

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

func NewUnsupportedEntryKindError(kind registry.Kind) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("unsupported entry kind: %s", kind)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"kind": kind}))
}

func NewExecutorAlreadyExistsError(id string) apierror.Error {
	return apierror.New(apierror.AlreadyExists, fmt.Sprintf("executor %s already exists", id)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"executor_id": id}))
}

func NewExecutorNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.NotFound, fmt.Sprintf("executor %s not found", id)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"executor_id": id}))
}

func NewConfigDecodeError(err error) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("failed to decode configuration: %v", err)).
		WithRetryable(apierror.False).
		WithCause(err)
}

func NewExecutorCreateError(err error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("failed to create executor: %v", err)).
		WithRetryable(apierror.True).
		WithCause(err)
}
