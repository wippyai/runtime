package temporal

import apierror "github.com/wippyai/runtime/api/error"

func NewInvalidConnectionTimeoutError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid connection timeout duration").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidKeepAliveTimeError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid keep alive time duration").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidKeepAliveTimeoutError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid keep alive timeout duration").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidHealthCheckIntervalError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid health check interval duration").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidAuthTypeError(authType string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid auth type: "+authType).WithRetryable(apierror.False)
}

func NewInvalidStickyScheduleToStartTimeoutError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid sticky schedule to start timeout").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidWorkerStopTimeoutError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid worker stop timeout").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidDeadlockDetectionTimeoutError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid deadlock detection timeout").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidMaxHeartbeatThrottleIntervalError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid max heartbeat throttle interval").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidDefaultHeartbeatThrottleIntervalError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid default heartbeat throttle interval").WithCause(cause).WithRetryable(apierror.False)
}
