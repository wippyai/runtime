package temporal

import (
	"fmt"

	apierror "github.com/wippyai/runtime/api/error"
	temporalapi "github.com/wippyai/runtime/api/service/temporal"
)

var (
	ErrAddressRequired = apierror.New(apierror.KindInvalid, "address is required").WithRetryable(apierror.False)

	ErrAPIKeySourceRequired = apierror.New(apierror.KindInvalid, "api_key auth requires one of: api_key, api_key_env, or api_key_file").WithRetryable(apierror.False)

	ErrAPIKeySourceConflict = apierror.New(apierror.KindInvalid, "api_key auth: only one of api_key, api_key_env, or api_key_file should be specified").WithRetryable(apierror.False)

	ErrMTLSCertRequired = apierror.New(apierror.KindInvalid, "mtls auth requires certificate (cert_file or cert_pem)").WithRetryable(apierror.False)

	ErrMTLSKeyRequired = apierror.New(apierror.KindInvalid, "mtls auth requires private key (key_file, key_pem, or key_pem_env)").WithRetryable(apierror.False)

	ErrMTLSCertConflict = apierror.New(apierror.KindInvalid, "mtls auth: specify either cert_file or cert_pem, not both").WithRetryable(apierror.False)

	ErrMTLSKeyConflict = apierror.New(apierror.KindInvalid, "mtls auth: specify only one of key_file, key_pem, or key_pem_env").WithRetryable(apierror.False)

	ErrTLSConfigConflict = apierror.New(apierror.KindInvalid, "tls: insecure_skip_verify and server_name are mutually exclusive").WithRetryable(apierror.False)

	ErrConnectionTimeoutInvalid = apierror.New(apierror.KindInvalid, "connection_timeout must be positive").WithRetryable(apierror.False)

	ErrKeepAliveTimeInvalid = apierror.New(apierror.KindInvalid, "keep_alive_time must be positive").WithRetryable(apierror.False)

	ErrKeepAliveTimeoutInvalid = apierror.New(apierror.KindInvalid, "keep_alive_timeout must be positive").WithRetryable(apierror.False)

	ErrHealthCheckIntervalInvalid = apierror.New(apierror.KindInvalid, "health_check interval must be positive when enabled").WithRetryable(apierror.False)

	ErrClientReferenceEmpty = apierror.New(apierror.KindInvalid, "client reference cannot be empty").WithRetryable(apierror.False)

	ErrTaskQueueEmpty = apierror.New(apierror.KindInvalid, "task queue name cannot be empty").WithRetryable(apierror.False)

	ErrMaxConcurrentActivityInvalid = apierror.New(apierror.KindInvalid, "max concurrent activity execution must be positive").WithRetryable(apierror.False)

	ErrMaxConcurrentWorkflowInvalid = apierror.New(apierror.KindInvalid, "max concurrent workflow execution must be positive").WithRetryable(apierror.False)

	ErrMaxConcurrentWorkflowTaskPollersInvalid = apierror.New(apierror.KindInvalid, "max concurrent workflow task pollers cannot be 1 due to sticky/non-sticky queue logic").WithRetryable(apierror.False)

	ErrWorkerActivitiesPerSecondInvalid = apierror.New(apierror.KindInvalid, "worker activities per second cannot be negative").WithRetryable(apierror.False)

	ErrWorkerLocalActivitiesPerSecondInvalid = apierror.New(apierror.KindInvalid, "worker local activities per second cannot be negative").WithRetryable(apierror.False)

	ErrTaskQueueActivitiesPerSecondInvalid = apierror.New(apierror.KindInvalid, "task queue activities per second cannot be negative").WithRetryable(apierror.False)

	ErrDisableWorkflowWorkerConflict = apierror.New(apierror.KindInvalid, "cannot set both disable_workflow_worker and local_activity_worker_only").WithRetryable(apierror.False)
)

func NewInvalidAuthTypeError(authType temporalapi.AuthType) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("invalid auth type: %s (must be none, api_key, or mtls)", authType)).WithRetryable(apierror.False)
}

func NewInvalidConnectionTimeoutError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid connection_timeout duration format").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidKeepAliveTimeError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid keep_alive_time duration format").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidKeepAliveTimeoutError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid keep_alive_timeout duration format").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidHealthCheckIntervalError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid health_check.interval duration format").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidStickyScheduleToStartTimeoutError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid sticky_schedule_to_start_timeout duration format").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidWorkerStopTimeoutError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid worker_stop_timeout duration format").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidDeadlockDetectionTimeoutError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid deadlock_detection_timeout duration format").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidMaxHeartbeatThrottleIntervalError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid max_heartbeat_throttle_interval duration format").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidDefaultHeartbeatThrottleIntervalError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid default_heartbeat_throttle_interval duration format").WithCause(cause).WithRetryable(apierror.False)
}
