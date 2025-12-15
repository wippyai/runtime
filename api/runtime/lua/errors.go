package lua

import (
	"fmt"

	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrSourceRequired = apierror.New(apierror.Invalid, "source is required").WithRetryable(apierror.False)

	ErrMethodRequired = apierror.New(apierror.Invalid, "method is required").WithRetryable(apierror.False)

	ErrEmptyImportAlias = apierror.New(apierror.Invalid, "import alias cannot be empty").WithRetryable(apierror.False)

	ErrEmptyModule = apierror.New(apierror.Invalid, "module cannot be empty").WithRetryable(apierror.False)

	ErrFSRequired = apierror.New(apierror.Invalid, "fs is required").WithRetryable(apierror.False)

	ErrPathRequired = apierror.New(apierror.Invalid, "path is required").WithRetryable(apierror.False)

	ErrHashRequired = apierror.New(apierror.Invalid, "hash is required").WithRetryable(apierror.False)

	ErrTranscoderNotFound = apierror.New(apierror.NotFound, "transcoder not found in context").WithRetryable(apierror.False)

	ErrChannelNotFound = apierror.New(apierror.NotFound, "channel not found").WithRetryable(apierror.False)

	ErrTaskNotFound = apierror.New(apierror.NotFound, "task not found").WithRetryable(apierror.False)

	ErrNoScriptOrProto = apierror.New(apierror.Invalid, "no script or proto provided").WithRetryable(apierror.False)
)

func NewInvalidPoolSizeError() apierror.Error {
	return apierror.New(apierror.Invalid, "pool.size must be greater than 0 for non-flex pools").WithRetryable(apierror.False)
}

func NewInvalidWorkerPoolSizeError() apierror.Error {
	return apierror.New(apierror.Invalid, "pool.size must be greater than 0 for worker pools").WithRetryable(apierror.False)
}

func NewEmptyImportNameError() apierror.Error {
	return apierror.New(apierror.Invalid, "import :name cannot be empty").WithRetryable(apierror.False)
}

func NewModuleNamespaceError() apierror.Error {
	return apierror.New(apierror.Invalid, "module cannot have a namespace").WithRetryable(apierror.False)
}

type DeadlockError struct {
	ThreadCount int
	Message     string
}

func (e *DeadlockError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("deadlock: %s (threads=%d)", e.Message, e.ThreadCount)
	}
	return fmt.Sprintf("deadlock: all %d coroutines blocked with no pending operations", e.ThreadCount)
}
