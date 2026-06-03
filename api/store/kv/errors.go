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
)
