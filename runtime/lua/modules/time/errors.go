package time

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrDurationRequired = apierror.New(apierror.Invalid, "duration is required").WithRetryable(apierror.False)

	ErrInvalidDuration = apierror.New(apierror.Invalid, "invalid duration format").WithRetryable(apierror.False)

	ErrTimerNotFound = apierror.New(apierror.NotFound, "timer not found").WithRetryable(apierror.False)
)

func NewParseDurationError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to parse duration").WithCause(cause).WithRetryable(apierror.False)
}

var (
	ErrDurationNumberOrStringExpected = apierror.New(apierror.Invalid, "duration must be number or string").WithRetryable(apierror.False)
)

func NewInvalidDurationType(typeName string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid duration type: "+typeName).WithRetryable(apierror.False)
}

func NewInvalidValueType(actual string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid value type: "+actual).WithRetryable(apierror.False)
}
