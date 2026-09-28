// SPDX-License-Identifier: MPL-2.0

package kv

import apierror "github.com/wippyai/runtime/api/error"

// Engine sentinel errors.
var (
	ErrKeyNotFound     = apierror.New(apierror.NotFound, "key not found").WithRetryable(apierror.False)
	ErrLeaseNotFound   = apierror.New(apierror.NotFound, "lease not found").WithRetryable(apierror.False)
	ErrLeaseExpired    = apierror.New(apierror.Invalid, "lease has expired").WithRetryable(apierror.False)
	ErrVersionMismatch = apierror.New(apierror.Invalid, "version mismatch").WithRetryable(apierror.True)
	ErrKVClosed        = apierror.New(apierror.Unavailable, "kv is closed").WithRetryable(apierror.False)
	ErrUnsupported     = apierror.New(apierror.Invalid, "operation not supported by this backend").WithRetryable(apierror.False)
	ErrWatchOverflow   = apierror.New(apierror.Unavailable, "kv watch exceeded its buffering limit").WithRetryable(apierror.True)
	ErrWatchReset      = apierror.New(apierror.Unavailable, "kv watch source was restored").WithRetryable(apierror.True)
	ErrWatchClosed     = apierror.New(apierror.Canceled, "kv watch was closed").WithRetryable(apierror.False)
	ErrWatchLimit      = apierror.New(apierror.Unavailable, "kv watch subscription limit reached").WithRetryable(apierror.True)
)
