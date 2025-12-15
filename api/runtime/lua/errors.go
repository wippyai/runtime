package lua

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
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

func NewInvalidEntryKindError(got, expected string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid entry kind "+got+", expected "+expected).WithRetryable(apierror.False)
}

func NewValidationError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid configuration: "+cause.Error()).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()}))
}

func NewPoolNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.NotFound, "pool not found: "+id).WithRetryable(apierror.False)
}

func NewUnknownPoolTypeError(poolType string) apierror.Error {
	return apierror.New(apierror.Invalid, "unknown pool type: "+poolType).WithRetryable(apierror.False)
}

func NewHashVerificationError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "hash verification failed").WithCause(cause).WithRetryable(apierror.False)
}

func NewFilesystemNotFoundError(fsID string) apierror.Error {
	return apierror.New(apierror.NotFound, "filesystem not found: "+fsID).WithRetryable(apierror.False)
}

func NewOpenFileError(path string, cause error) apierror.Error {
	return apierror.New(apierror.NotFound, "failed to open file: "+path).
		WithCause(cause).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()}))
}

func NewInvalidHashFormatError(hash string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid hash format: "+hash).WithRetryable(apierror.False)
}

func NewUnsupportedHashAlgorithmError(algorithm string) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported hash algorithm: "+algorithm).WithRetryable(apierror.False)
}

func NewHashMismatchError(expected, actual string) apierror.Error {
	return apierror.New(apierror.Invalid, "hash mismatch: expected "+expected+", got "+actual).WithRetryable(apierror.False)
}

func NewTopicAlreadySubscribedError(topic string) apierror.Error {
	return apierror.New(apierror.AlreadyExists, "topic \""+topic+"\" already subscribed").WithRetryable(apierror.False)
}

func NewMethodNotFoundError(method string) apierror.Error {
	return apierror.New(apierror.NotFound, "method \""+method+"\" not found in module").WithRetryable(apierror.False)
}

func NewTaskNotFoundForChannelError(cause error) apierror.Error {
	return apierror.New(apierror.NotFound, "task not found for channel result").WithCause(cause).WithRetryable(apierror.False)
}

func NewNotAllowedError(action, target string) apierror.Error {
	return apierror.New(apierror.PermissionDenied, "not allowed to "+action+": "+target).WithRetryable(apierror.False)
}

func NewCouldNotResolveError(pidOrName string) apierror.Error {
	return apierror.New(apierror.NotFound, "could not resolve '"+pidOrName+"' as PID or registered name").WithRetryable(apierror.False)
}

func NewInvalidFormatError(message string) apierror.Error {
	return apierror.New(apierror.Invalid, message).WithRetryable(apierror.False)
}

func NewInvalidTypeError(message string) apierror.Error {
	return apierror.New(apierror.Invalid, message).WithRetryable(apierror.False)
}

func NewUnsupportedTypeError(message string) apierror.Error {
	return apierror.New(apierror.Invalid, message).WithRetryable(apierror.False)
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
