// SPDX-License-Identifier: MPL-2.0

package env

import apierror "github.com/wippyai/runtime/api/error"

var (
	ErrVariableNotFound = apierror.New(apierror.NotFound, "environment variable not found").WithRetryable(apierror.False)

	ErrStorageNotFound = apierror.New(apierror.NotFound, "environment storage backend not found").WithRetryable(apierror.False)

	ErrVariableReadOnly = apierror.New(apierror.PermissionDenied, "environment variable is read-only").WithRetryable(apierror.False)

	ErrInvalidVariableName = apierror.New(apierror.Invalid, "invalid environment variable name").WithRetryable(apierror.False)

	ErrInvalidStorageID = apierror.New(apierror.Invalid, "invalid storage ID format, must have both namespace and name").WithRetryable(apierror.False)

	ErrEmptyStorageList = apierror.New(apierror.Invalid, "router storage must have at least one storage").WithRetryable(apierror.False)

	ErrStorageReadOnly = apierror.New(apierror.PermissionDenied, "storage is read-only").WithRetryable(apierror.False)

	ErrNoStorages = apierror.New(apierror.Invalid, "at least one storage must be provided").WithRetryable(apierror.False)

	ErrEmptyFilePath = apierror.New(apierror.Invalid, "file path must not be empty").WithRetryable(apierror.False)
)
