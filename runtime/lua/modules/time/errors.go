package time

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrDurationNumberOrStringExpected = apierror.New(apierror.Invalid, "duration must be number or string").WithRetryable(apierror.False)
)

func NewInvalidDurationType(typeName string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid duration type: "+typeName).WithRetryable(apierror.False)
}

func NewInvalidValueType(actual string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid value type: "+actual).WithRetryable(apierror.False)
}
