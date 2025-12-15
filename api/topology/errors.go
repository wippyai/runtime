package topology

import apierror "github.com/wippyai/runtime/api/error"

// Sentinel errors for topology operations.
var (
	ErrNameAlreadyRegistered = apierror.New(apierror.KindAlreadyExists, "name already registered").WithRetryable(apierror.False)
	ErrPIDAlreadyRegistered  = apierror.New(apierror.KindAlreadyExists, "pid already registered").WithRetryable(apierror.False)
	ErrPIDNotFound           = apierror.New(apierror.KindNotFound, "pid not found").WithRetryable(apierror.False)
	ErrPIDNotRegistered      = apierror.New(apierror.KindNotFound, "pid not registered").WithRetryable(apierror.False)
	ErrAlreadyMonitoring     = apierror.New(apierror.KindAlreadyExists, "already monitoring pid").WithRetryable(apierror.False)
)
