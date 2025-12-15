package supervisor

import (
	"time"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrStartTimeout = apierror.New(apierror.Timeout, "service start timed out").WithRetryable(apierror.True)
)

func NewServiceNotFoundError(serviceID string) apierror.Error {
	return apierror.E(
		apierror.NotFound,
		"service "+serviceID+" not found",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"service_id": serviceID}),
		nil,
	)
}

func NewSubscriberError(err error) apierror.Error {
	return apierror.E(
		apierror.Internal,
		"failed to create event subscriber",
		apierror.True,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

func NewDependencyResolveError(serviceID string, err error) apierror.Error {
	return apierror.E(
		apierror.Internal,
		"failed to resolve dependencies for "+serviceID,
		apierror.False,
		attrs.NewBagFrom(map[string]any{"service_id": serviceID, "cause": err.Error()}),
		err,
	)
}

func NewStartOperationsError(err error) apierror.Error {
	return apierror.E(
		apierror.Internal,
		"failed to build start operations",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

func NewTransitionError(err error) apierror.Error {
	return apierror.E(
		apierror.Internal,
		"failed to execute transitions",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

func NewStopError(err error) apierror.Error {
	return apierror.E(
		apierror.Internal,
		"failed to stop service",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

func NewServiceStartError(serviceID string, err error) apierror.Error {
	return apierror.E(
		apierror.Internal,
		"failed to start service "+serviceID,
		apierror.True,
		attrs.NewBagFrom(map[string]any{"service_id": serviceID, "cause": err.Error()}),
		err,
	)
}

func NewServiceStopError(serviceID string, err error) apierror.Error {
	return apierror.E(
		apierror.Internal,
		"failed to stop service "+serviceID,
		apierror.False,
		attrs.NewBagFrom(map[string]any{"service_id": serviceID, "cause": err.Error()}),
		err,
	)
}

func NewStartSequenceError(err error) apierror.Error {
	return apierror.E(
		apierror.Internal,
		"start sequence failed",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

func NewStopSequenceError(err error) apierror.Error {
	return apierror.E(
		apierror.Internal,
		"stop sequence failed",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

func NewDependencyLevelsError(phase string, err error) apierror.Error {
	return apierror.E(
		apierror.Internal,
		"failed to determine "+phase+" dependency levels",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"phase": phase, "cause": err.Error()}),
		err,
	)
}

func NewMultiStartError(count int, firstErr error) apierror.Error {
	return apierror.E(
		apierror.Internal,
		"start failed for multiple services",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"failed_count": count, "first_error": firstErr.Error()}),
		firstErr,
	)
}

func NewMultiStopError(count int, firstErr error) apierror.Error {
	return apierror.E(
		apierror.Internal,
		"stop failed for multiple services",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"failed_count": count, "first_error": firstErr.Error()}),
		firstErr,
	)
}

func NewCommitRemoveError(serviceID string, err error) apierror.Error {
	return apierror.E(
		apierror.Internal,
		"failed to remove service "+serviceID+" during commit",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"service_id": serviceID, "cause": err.Error()}),
		err,
	)
}

func NewCommitRegisterError(serviceID string, err error) apierror.Error {
	return apierror.E(
		apierror.Internal,
		"failed to register service "+serviceID+" during commit",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"service_id": serviceID, "cause": err.Error()}),
		err,
	)
}

func NewStopTimeoutError(timeout time.Duration) apierror.Error {
	return apierror.E(
		apierror.Timeout,
		"service stop timed out after "+timeout.String(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"timeout": timeout.String()}),
		nil,
	)
}

func NewSupervisorStoppedError(err error) apierror.Error {
	return apierror.E(
		apierror.Unavailable,
		"supervisor is stopped",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}
