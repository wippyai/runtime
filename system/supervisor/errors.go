package supervisor

import (
	"time"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrStartTimeout = apierror.New(apierror.KindTimeout, "service start timed out").WithRetryable(apierror.True)
)

func NewInvalidDurationError(field string, cause error) apierror.Error {
	return apierror.E(
		apierror.KindInvalid,
		"invalid "+field+" duration format",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"field": field}),
		cause,
	)
}

func NewServiceNotFoundError(serviceID string) apierror.Error {
	return apierror.E(
		apierror.KindNotFound,
		"service "+serviceID+" not found",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"service_id": serviceID}),
		nil,
	)
}

func NewSubscriberError(err error) apierror.Error {
	return apierror.E(
		apierror.KindInternal,
		"failed to create event subscriber: "+err.Error(),
		apierror.True,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

func NewDependencyResolveError(serviceID string, err error) apierror.Error {
	return apierror.E(
		apierror.KindInternal,
		"failed to resolve dependencies for "+serviceID+": "+err.Error(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"service_id": serviceID, "cause": err.Error()}),
		err,
	)
}

func NewStartOperationsError(err error) apierror.Error {
	return apierror.E(
		apierror.KindInternal,
		"failed to build start operations: "+err.Error(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

func NewTransitionError(err error) apierror.Error {
	return apierror.E(
		apierror.KindInternal,
		"failed to execute transitions: "+err.Error(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

func NewStopError(err error) apierror.Error {
	return apierror.E(
		apierror.KindInternal,
		"failed to stop service: "+err.Error(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

func NewServiceStartError(serviceID string, err error) apierror.Error {
	return apierror.E(
		apierror.KindInternal,
		"failed to start service "+serviceID+": "+err.Error(),
		apierror.True,
		attrs.NewBagFrom(map[string]any{"service_id": serviceID, "cause": err.Error()}),
		err,
	)
}

func NewServiceStopError(serviceID string, err error) apierror.Error {
	return apierror.E(
		apierror.KindInternal,
		"failed to stop service "+serviceID+": "+err.Error(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"service_id": serviceID, "cause": err.Error()}),
		err,
	)
}

func NewStartSequenceError(err error) apierror.Error {
	return apierror.E(
		apierror.KindInternal,
		"start sequence failed: "+err.Error(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

func NewStopSequenceError(err error) apierror.Error {
	return apierror.E(
		apierror.KindInternal,
		"stop sequence failed: "+err.Error(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

func NewDependencyLevelsError(phase string, err error) apierror.Error {
	return apierror.E(
		apierror.KindInternal,
		"failed to determine "+phase+" dependency levels: "+err.Error(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"phase": phase, "cause": err.Error()}),
		err,
	)
}

func NewMultiStartError(count int, firstErr error) apierror.Error {
	return apierror.E(
		apierror.KindInternal,
		"start failed for multiple services: "+firstErr.Error(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"failed_count": count, "first_error": firstErr.Error()}),
		firstErr,
	)
}

func NewMultiStopError(count int, firstErr error) apierror.Error {
	return apierror.E(
		apierror.KindInternal,
		"stop failed for multiple services: "+firstErr.Error(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"failed_count": count, "first_error": firstErr.Error()}),
		firstErr,
	)
}

func NewCommitRemoveError(serviceID string, err error) apierror.Error {
	return apierror.E(
		apierror.KindInternal,
		"failed to remove service "+serviceID+" during commit: "+err.Error(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"service_id": serviceID, "cause": err.Error()}),
		err,
	)
}

func NewCommitRegisterError(serviceID string, err error) apierror.Error {
	return apierror.E(
		apierror.KindInternal,
		"failed to register service "+serviceID+" during commit: "+err.Error(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"service_id": serviceID, "cause": err.Error()}),
		err,
	)
}

func NewStopTimeoutError(timeout time.Duration) apierror.Error {
	return apierror.E(
		apierror.KindTimeout,
		"service stop timed out after "+timeout.String(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"timeout": timeout.String()}),
		nil,
	)
}

func NewSupervisorStoppedError(err error) apierror.Error {
	return apierror.E(
		apierror.KindUnavailable,
		"supervisor is stopped: "+err.Error(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}
