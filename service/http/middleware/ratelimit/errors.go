package ratelimit

import apierror "github.com/wippyai/runtime/api/error"

func NewInvalidDurationError(s string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid duration: "+s)
}

func NewInvalidDurationValueError(s string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid duration value: "+s)
}

func NewInvalidDurationUnitError(unit string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid duration unit: "+unit+" (use s, m, or h)")
}
