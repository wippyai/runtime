package time

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrDurationRequired = apierror.New(apierror.KindInvalid, "duration is required").WithRetryable(apierror.False)

	ErrInvalidDuration = apierror.New(apierror.KindInvalid, "invalid duration format").WithRetryable(apierror.False)

	ErrTimerNotFound = apierror.New(apierror.KindNotFound, "timer not found").WithRetryable(apierror.False)
)

func NewParseDurationError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "failed to parse duration").WithCause(cause).WithRetryable(apierror.False)
}

var (
	ErrDurationNumberOrStringExpected = apierror.New(apierror.KindInvalid, "duration must be number or string").WithRetryable(apierror.False)
)

func NewInvalidDurationType(typeName string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid duration type: "+typeName).WithRetryable(apierror.False)
}

func NewInvalidValueType(actual string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid value type: "+actual).WithRetryable(apierror.False)
}
