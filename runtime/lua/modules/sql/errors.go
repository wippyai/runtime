package sql

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrConnectionRequired = apierror.New(apierror.Invalid, "connection ID is required").WithRetryable(apierror.False)

	ErrQueryRequired = apierror.New(apierror.Invalid, "query is required").WithRetryable(apierror.False)
)

func NewConnectionNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.NotFound, "connection not found: "+id).WithRetryable(apierror.False)
}

func NewInvalidParametersTypeError(actualType string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid parameters type: "+actualType).WithRetryable(apierror.False)
}
