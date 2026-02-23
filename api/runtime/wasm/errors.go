// SPDX-License-Identifier: MPL-2.0

package wasm

import apierror "github.com/wippyai/runtime/api/error"

var (
	ErrSourceRequired = apierror.New(apierror.Invalid, "source is required").WithRetryable(apierror.False)

	ErrMethodRequired = apierror.New(apierror.Invalid, "method is required").WithRetryable(apierror.False)

	ErrEmptyImportName = apierror.New(apierror.Invalid, "import :name cannot be empty").WithRetryable(apierror.False)

	ErrFSRequired = apierror.New(apierror.Invalid, "fs is required").WithRetryable(apierror.False)

	ErrPathRequired = apierror.New(apierror.Invalid, "path is required").WithRetryable(apierror.False)

	ErrHashRequired = apierror.New(apierror.Invalid, "hash is required").WithRetryable(apierror.False)

	ErrInvalidPoolType = apierror.New(apierror.Invalid, "invalid pool type").WithRetryable(apierror.False)

	ErrInvalidPoolSize = apierror.New(apierror.Invalid, "pool.size must be greater than 0 for non-flex pools").WithRetryable(apierror.False)

	ErrInvalidWorkerPoolSize = apierror.New(apierror.Invalid, "pool.size must be greater than 0 for worker pools").WithRetryable(apierror.False)

	ErrInvalidPoolConfig = apierror.New(apierror.Invalid, "pool values cannot be negative").WithRetryable(apierror.False)

	ErrInvalidTransportType = apierror.New(apierror.Invalid, "invalid transport type").WithRetryable(apierror.False)

	ErrInvalidExecutionLimit = apierror.New(apierror.Invalid, "limits.max_execution_ms cannot be negative").WithRetryable(apierror.False)

	ErrWASICwdMustBeAbsolute = apierror.New(apierror.Invalid, "wasi.cwd must be absolute").WithRetryable(apierror.False)

	ErrWASIEnvIDRequired = apierror.New(apierror.Invalid, "wasi.env[].id is required").WithRetryable(apierror.False)

	ErrWASIEnvNameRequired = apierror.New(apierror.Invalid, "wasi.env[].name is required").WithRetryable(apierror.False)

	ErrWASIEnvNameDuplicate = apierror.New(apierror.Invalid, "wasi.env[].name must be unique").WithRetryable(apierror.False)

	ErrWASIMountFSRequired = apierror.New(apierror.Invalid, "wasi.mounts[].fs is required").WithRetryable(apierror.False)

	ErrWASIMountGuestRequired = apierror.New(apierror.Invalid, "wasi.mounts[].guest is required").WithRetryable(apierror.False)

	ErrWASIMountGuestMustBeAbsolute = apierror.New(apierror.Invalid, "wasi.mounts[].guest must be absolute").WithRetryable(apierror.False)

	ErrWASIMountGuestDuplicate = apierror.New(apierror.Invalid, "wasi.mounts[].guest must be unique").WithRetryable(apierror.False)
)
