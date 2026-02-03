package store

import (
	apierror "github.com/wippyai/runtime/api/error"
)

// Sentinel errors.
var (
	ErrKeyNotFound = apierror.New(apierror.NotFound, "key not found").WithRetryable(apierror.False)
	ErrKeyExists   = apierror.New(apierror.AlreadyExists, "key already exists").WithRetryable(apierror.False)
	ErrInvalidKey  = apierror.New(apierror.Invalid, "invalid key format").WithRetryable(apierror.False)
)
