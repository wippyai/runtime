package temporal

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

type Error struct {
	kind      apierror.Kind
	message   string
	retryable apierror.Ternary
	details   attrs.Attributes
	cause     error
}

func (e *Error) Error() string               { return e.message }
func (e *Error) Kind() apierror.Kind         { return e.kind }
func (e *Error) Retryable() apierror.Ternary { return e.retryable }
func (e *Error) Details() attrs.Attributes   { return e.details }
func (e *Error) Unwrap() error               { return e.cause }

var (
	ErrAddressRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "address is required",
		retryable: apierror.False,
	}

	ErrAPIKeySourceRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "api_key auth requires one of: api_key, api_key_env, or api_key_file",
		retryable: apierror.False,
	}

	ErrAPIKeySourceConflict = &Error{
		kind:      apierror.KindInvalid,
		message:   "api_key auth: only one of api_key, api_key_env, or api_key_file should be specified",
		retryable: apierror.False,
	}

	ErrMTLSCertRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "mtls auth requires certificate (cert_file or cert_pem)",
		retryable: apierror.False,
	}

	ErrMTLSKeyRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "mtls auth requires private key (key_file, key_pem, or key_pem_env)",
		retryable: apierror.False,
	}

	ErrMTLSCertConflict = &Error{
		kind:      apierror.KindInvalid,
		message:   "mtls auth: specify either cert_file or cert_pem, not both",
		retryable: apierror.False,
	}

	ErrMTLSKeyConflict = &Error{
		kind:      apierror.KindInvalid,
		message:   "mtls auth: specify only one of key_file, key_pem, or key_pem_env",
		retryable: apierror.False,
	}

	ErrTLSConfigConflict = &Error{
		kind:      apierror.KindInvalid,
		message:   "tls: insecure_skip_verify and server_name are mutually exclusive",
		retryable: apierror.False,
	}

	ErrConnectionTimeoutInvalid = &Error{
		kind:      apierror.KindInvalid,
		message:   "connection_timeout must be positive",
		retryable: apierror.False,
	}

	ErrKeepAliveTimeInvalid = &Error{
		kind:      apierror.KindInvalid,
		message:   "keep_alive_time must be positive",
		retryable: apierror.False,
	}

	ErrKeepAliveTimeoutInvalid = &Error{
		kind:      apierror.KindInvalid,
		message:   "keep_alive_timeout must be positive",
		retryable: apierror.False,
	}

	ErrHealthCheckIntervalInvalid = &Error{
		kind:      apierror.KindInvalid,
		message:   "health_check interval must be positive when enabled",
		retryable: apierror.False,
	}

	ErrClientReferenceEmpty = &Error{
		kind:      apierror.KindInvalid,
		message:   "client reference cannot be empty",
		retryable: apierror.False,
	}

	ErrTaskQueueEmpty = &Error{
		kind:      apierror.KindInvalid,
		message:   "task queue name cannot be empty",
		retryable: apierror.False,
	}

	ErrMaxConcurrentActivityInvalid = &Error{
		kind:      apierror.KindInvalid,
		message:   "max concurrent activity execution must be positive",
		retryable: apierror.False,
	}

	ErrMaxConcurrentWorkflowInvalid = &Error{
		kind:      apierror.KindInvalid,
		message:   "max concurrent workflow execution must be positive",
		retryable: apierror.False,
	}

	ErrMaxConcurrentWorkflowTaskPollersInvalid = &Error{
		kind:      apierror.KindInvalid,
		message:   "max concurrent workflow task pollers cannot be 1 due to sticky/non-sticky queue logic",
		retryable: apierror.False,
	}

	ErrWorkerActivitiesPerSecondInvalid = &Error{
		kind:      apierror.KindInvalid,
		message:   "worker activities per second cannot be negative",
		retryable: apierror.False,
	}

	ErrWorkerLocalActivitiesPerSecondInvalid = &Error{
		kind:      apierror.KindInvalid,
		message:   "worker local activities per second cannot be negative",
		retryable: apierror.False,
	}

	ErrTaskQueueActivitiesPerSecondInvalid = &Error{
		kind:      apierror.KindInvalid,
		message:   "task queue activities per second cannot be negative",
		retryable: apierror.False,
	}

	ErrDisableWorkflowWorkerConflict = &Error{
		kind:      apierror.KindInvalid,
		message:   "cannot set both disable_workflow_worker and local_activity_worker_only",
		retryable: apierror.False,
	}
)

func NewInvalidAuthTypeError(authType AuthType) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   fmt.Sprintf("invalid auth type: %s (must be none, api_key, or mtls)", authType),
		retryable: apierror.False,
	}
}

func NewInvalidConnectionTimeoutError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid connection_timeout duration format",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewInvalidKeepAliveTimeError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid keep_alive_time duration format",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewInvalidKeepAliveTimeoutError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid keep_alive_timeout duration format",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewInvalidHealthCheckIntervalError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid health_check.interval duration format",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewInvalidStickyScheduleToStartTimeoutError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid sticky_schedule_to_start_timeout duration format",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewInvalidWorkerStopTimeoutError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid worker_stop_timeout duration format",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewInvalidDeadlockDetectionTimeoutError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid deadlock_detection_timeout duration format",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewInvalidMaxHeartbeatThrottleIntervalError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid max_heartbeat_throttle_interval duration format",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewInvalidDefaultHeartbeatThrottleIntervalError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid default_heartbeat_throttle_interval duration format",
		retryable: apierror.False,
		cause:     cause,
	}
}
