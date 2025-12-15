package temporal

import apierror "github.com/wippyai/runtime/api/error"

var (
	ErrAddressRequired                         = apierror.New(apierror.KindInvalid, "address is required").WithRetryable(apierror.False)
	ErrAPIKeySourceRequired                    = apierror.New(apierror.KindInvalid, "API key source is required").WithRetryable(apierror.False)
	ErrAPIKeySourceConflict                    = apierror.New(apierror.KindInvalid, "multiple API key sources specified").WithRetryable(apierror.False)
	ErrMTLSCertRequired                        = apierror.New(apierror.KindInvalid, "mTLS certificate is required").WithRetryable(apierror.False)
	ErrMTLSKeyRequired                         = apierror.New(apierror.KindInvalid, "mTLS key is required").WithRetryable(apierror.False)
	ErrMTLSCertConflict                        = apierror.New(apierror.KindInvalid, "multiple mTLS certificate sources specified").WithRetryable(apierror.False)
	ErrMTLSKeyConflict                         = apierror.New(apierror.KindInvalid, "multiple mTLS key sources specified").WithRetryable(apierror.False)
	ErrTLSConfigConflict                       = apierror.New(apierror.KindInvalid, "cannot use insecure skip verify with server name").WithRetryable(apierror.False)
	ErrConnectionTimeoutInvalid                = apierror.New(apierror.KindInvalid, "connection timeout must be >= 0").WithRetryable(apierror.False)
	ErrKeepAliveTimeInvalid                    = apierror.New(apierror.KindInvalid, "keep alive time must be >= 0").WithRetryable(apierror.False)
	ErrKeepAliveTimeoutInvalid                 = apierror.New(apierror.KindInvalid, "keep alive timeout must be >= 0").WithRetryable(apierror.False)
	ErrHealthCheckIntervalInvalid              = apierror.New(apierror.KindInvalid, "health check interval must be > 0 when enabled").WithRetryable(apierror.False)
	ErrClientReferenceEmpty                    = apierror.New(apierror.KindInvalid, "client reference is required").WithRetryable(apierror.False)
	ErrTaskQueueEmpty                          = apierror.New(apierror.KindInvalid, "task queue is required").WithRetryable(apierror.False)
	ErrMaxConcurrentActivityInvalid            = apierror.New(apierror.KindInvalid, "max concurrent activity execution size must be > 0").WithRetryable(apierror.False)
	ErrMaxConcurrentWorkflowInvalid            = apierror.New(apierror.KindInvalid, "max concurrent workflow task execution size must be > 0").WithRetryable(apierror.False)
	ErrMaxConcurrentWorkflowTaskPollersInvalid = apierror.New(apierror.KindInvalid, "max concurrent workflow task pollers cannot be 1").WithRetryable(apierror.False)
	ErrWorkerActivitiesPerSecondInvalid        = apierror.New(apierror.KindInvalid, "worker activities per second cannot be negative").WithRetryable(apierror.False)
	ErrWorkerLocalActivitiesPerSecondInvalid   = apierror.New(apierror.KindInvalid, "worker local activities per second cannot be negative").WithRetryable(apierror.False)
	ErrTaskQueueActivitiesPerSecondInvalid     = apierror.New(apierror.KindInvalid, "task queue activities per second cannot be negative").WithRetryable(apierror.False)
	ErrDisableWorkflowWorkerConflict           = apierror.New(apierror.KindInvalid, "cannot disable workflow worker and use local activity worker only simultaneously").WithRetryable(apierror.False)
)

func NewInvalidConnectionTimeoutError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid connection timeout duration").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidKeepAliveTimeError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid keep alive time duration").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidKeepAliveTimeoutError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid keep alive timeout duration").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidHealthCheckIntervalError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid health check interval duration").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidAuthTypeError(authType AuthType) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid auth type: "+string(authType)).WithRetryable(apierror.False)
}

func NewInvalidStickyScheduleToStartTimeoutError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid sticky schedule to start timeout").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidWorkerStopTimeoutError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid worker stop timeout").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidDeadlockDetectionTimeoutError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid deadlock detection timeout").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidMaxHeartbeatThrottleIntervalError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid max heartbeat throttle interval").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidDefaultHeartbeatThrottleIntervalError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid default heartbeat throttle interval").WithCause(cause).WithRetryable(apierror.False)
}
