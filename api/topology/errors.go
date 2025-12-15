package topology

import apierror "github.com/wippyai/runtime/api/error"

// Sentinel errors for topology operations.
var (
	ErrNameAlreadyRegistered = apierror.New(apierror.AlreadyExists, "name already registered").WithRetryable(apierror.False)
	ErrPIDAlreadyRegistered  = apierror.New(apierror.AlreadyExists, "pid already registered").WithRetryable(apierror.False)
	ErrPIDNotFound           = apierror.New(apierror.NotFound, "pid not found").WithRetryable(apierror.False)
	ErrPIDNotRegistered      = apierror.New(apierror.NotFound, "pid not registered").WithRetryable(apierror.False)
	ErrAlreadyMonitoring     = apierror.New(apierror.AlreadyExists, "already monitoring pid").WithRetryable(apierror.False)
)
