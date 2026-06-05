// SPDX-License-Identifier: MPL-2.0

package cdc

import apierror "github.com/wippyai/runtime/api/error"

var (
	ErrHostRequired        = apierror.New(apierror.Invalid, "host is required").WithRetryable(apierror.False)
	ErrInvalidPort         = apierror.New(apierror.Invalid, "port must be greater than 0").WithRetryable(apierror.False)
	ErrDatabaseRequired    = apierror.New(apierror.Invalid, "database is required").WithRetryable(apierror.False)
	ErrUsernameRequired    = apierror.New(apierror.Invalid, "username is required").WithRetryable(apierror.False)
	ErrPasswordRequired    = apierror.New(apierror.Invalid, "password is required").WithRetryable(apierror.False)
	ErrSlotNameRequired    = apierror.New(apierror.Invalid, "slot_name is required").WithRetryable(apierror.False)
	ErrPublicationRequired = apierror.New(apierror.Invalid, "publication or tables is required").WithRetryable(apierror.False)
	ErrInvalidInterval     = apierror.New(apierror.Invalid, "interval must be a non-negative duration (e.g. 10s)").WithRetryable(apierror.False)
)
