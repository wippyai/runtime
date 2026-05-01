// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"strings"
	"time"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrStartTimeout = apierror.New(apierror.Timeout, "service start timed out").WithRetryable(apierror.True)
)

func NewServiceNotFoundError(serviceID string) apierror.Error {
	return apierror.New(apierror.NotFound, "service not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"service_id": serviceID}))
}

func NewSubscriberError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create event subscriber").
		WithRetryable(apierror.True).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewDependencyResolveError(serviceID string, err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to resolve dependencies").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"service_id": serviceID, "cause": err.Error()})).
		WithCause(err)
}

func NewStartOperationsError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to build start operations").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewTransitionError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to execute transitions").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewStopError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to stop service").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewServiceStartError(serviceID string, err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to start service").
		WithRetryable(apierror.True).
		WithDetails(attrs.NewBagFrom(map[string]any{"service_id": serviceID, "cause": err.Error()})).
		WithCause(err)
}

func NewServiceBlockedError(serviceID string, blockers []string) apierror.Error {
	message := "service startup blocked by missing dependencies"
	if serviceID != "" && len(blockers) > 0 {
		message += ": " + serviceID + " requires " + strings.Join(blockers, ", ")
	}

	return apierror.New(apierror.Invalid, message).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"service_id": serviceID, "blockers": blockers}))
}

func NewServiceStopError(serviceID string, err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to stop service").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"service_id": serviceID, "cause": err.Error()})).
		WithCause(err)
}

func NewStartSequenceError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "start sequence failed").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewStopSequenceError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "stop sequence failed").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewDependencyLevelsError(phase string, err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to determine "+phase+" dependency levels").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"phase": phase, "cause": err.Error()})).
		WithCause(err)
}

func NewMultiStartError(count int, firstErr error) apierror.Error {
	return apierror.New(apierror.Internal, "start failed for multiple services").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"failed_count": count, "first_error": firstErr.Error()})).
		WithCause(firstErr)
}

func NewMultiStopError(count int, firstErr error) apierror.Error {
	return apierror.New(apierror.Internal, "stop failed for multiple services").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"failed_count": count, "first_error": firstErr.Error()})).
		WithCause(firstErr)
}

func NewCommitRemoveError(serviceID string, err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to remove service during commit").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"service_id": serviceID, "cause": err.Error()})).
		WithCause(err)
}

func NewCommitRegisterError(serviceID string, err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to register service during commit").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"service_id": serviceID, "cause": err.Error()})).
		WithCause(err)
}

func NewStopTimeoutError(timeout time.Duration) apierror.Error {
	return apierror.New(apierror.Timeout, "service stop timed out").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"timeout": timeout.String()}))
}

func NewSupervisorStoppedError(err error) apierror.Error {
	return apierror.New(apierror.Unavailable, "supervisor is stopped").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}
