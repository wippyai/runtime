// SPDX-License-Identifier: MPL-2.0

package store

import (
	apierror "github.com/wippyai/runtime/api/error"
)

// Sentinel errors.
var (
	ErrKeyNotFound     = apierror.New(apierror.NotFound, "key not found").WithRetryable(apierror.False)
	ErrKeyExists       = apierror.New(apierror.AlreadyExists, "key already exists").WithRetryable(apierror.False)
	ErrInvalidKey      = apierror.New(apierror.Invalid, "invalid key format").WithRetryable(apierror.False)
	ErrInvalidOptions  = apierror.New(apierror.Invalid, "invalid store options").WithRetryable(apierror.False)
	ErrUnsupported     = apierror.New(apierror.Invalid, "operation not supported by this store").WithRetryable(apierror.False)
	ErrVersionMismatch = apierror.New(apierror.Conflict, "version mismatch").WithRetryable(apierror.True)
)
