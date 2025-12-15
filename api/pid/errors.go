package pid

import apierror "github.com/wippyai/runtime/api/error"

// ErrInvalidPIDFormat is returned when a PID string cannot be parsed.
var ErrInvalidPIDFormat = apierror.New(apierror.KindInvalid, "invalid pid format").WithRetryable(apierror.False)
