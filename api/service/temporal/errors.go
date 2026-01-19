package temporal

import apierror "github.com/wippyai/runtime/api/error"

var (
	ErrAddressRequired                         = apierror.New(apierror.Invalid, "address is required").WithRetryable(apierror.False)
	ErrAPIKeySourceRequired                    = apierror.New(apierror.Invalid, "API key source is required").WithRetryable(apierror.False)
	ErrAPIKeySourceConflict                    = apierror.New(apierror.Invalid, "multiple API key sources specified").WithRetryable(apierror.False)
	ErrMTLSCertRequired                        = apierror.New(apierror.Invalid, "mTLS certificate is required").WithRetryable(apierror.False)
	ErrMTLSKeyRequired                         = apierror.New(apierror.Invalid, "mTLS key is required").WithRetryable(apierror.False)
	ErrMTLSCertConflict                        = apierror.New(apierror.Invalid, "multiple mTLS certificate sources specified").WithRetryable(apierror.False)
	ErrMTLSKeyConflict                         = apierror.New(apierror.Invalid, "multiple mTLS key sources specified").WithRetryable(apierror.False)
	ErrTLSConfigConflict                       = apierror.New(apierror.Invalid, "cannot use insecure skip verify with server name").WithRetryable(apierror.False)
	ErrConnectionTimeoutInvalid                = apierror.New(apierror.Invalid, "connection timeout must be >= 0").WithRetryable(apierror.False)
	ErrKeepAliveTimeInvalid                    = apierror.New(apierror.Invalid, "keep alive time must be >= 0").WithRetryable(apierror.False)
	ErrKeepAliveTimeoutInvalid                 = apierror.New(apierror.Invalid, "keep alive timeout must be >= 0").WithRetryable(apierror.False)
	ErrHealthCheckIntervalInvalid              = apierror.New(apierror.Invalid, "health check interval must be > 0 when enabled").WithRetryable(apierror.False)
	ErrClientReferenceEmpty                    = apierror.New(apierror.Invalid, "client reference is required").WithRetryable(apierror.False)
	ErrTaskQueueEmpty                          = apierror.New(apierror.Invalid, "task queue is required").WithRetryable(apierror.False)
	ErrMaxConcurrentActivityInvalid            = apierror.New(apierror.Invalid, "max concurrent activity execution size must be > 0").WithRetryable(apierror.False)
	ErrMaxConcurrentWorkflowInvalid            = apierror.New(apierror.Invalid, "max concurrent workflow task execution size must be > 0").WithRetryable(apierror.False)
	ErrMaxConcurrentWorkflowTaskPollersInvalid = apierror.New(apierror.Invalid, "max concurrent workflow task pollers cannot be 1").WithRetryable(apierror.False)
	ErrWorkerActivitiesPerSecondInvalid        = apierror.New(apierror.Invalid, "worker activities per second cannot be negative").WithRetryable(apierror.False)
	ErrWorkerLocalActivitiesPerSecondInvalid   = apierror.New(apierror.Invalid, "worker local activities per second cannot be negative").WithRetryable(apierror.False)
	ErrTaskQueueActivitiesPerSecondInvalid     = apierror.New(apierror.Invalid, "task queue activities per second cannot be negative").WithRetryable(apierror.False)
	ErrDisableWorkflowWorkerConflict           = apierror.New(apierror.Invalid, "cannot disable workflow worker and use local activity worker only simultaneously").WithRetryable(apierror.False)
)

// NewInvalidAuthTypeError reports an unsupported auth type.
func NewInvalidAuthTypeError(authType AuthType) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid auth type: "+string(authType)).WithRetryable(apierror.False)
}
