package lua

import (
	"errors"
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrSourceRequired = apierror.New(apierror.KindInvalid, "source is required").WithRetryable(apierror.False)

	ErrMethodRequired = apierror.New(apierror.KindInvalid, "method is required").WithRetryable(apierror.False)

	ErrEmptyImportAlias = apierror.New(apierror.KindInvalid, "import alias cannot be empty").WithRetryable(apierror.False)

	ErrEmptyModule = apierror.New(apierror.KindInvalid, "module cannot be empty").WithRetryable(apierror.False)

	ErrFSRequired = apierror.New(apierror.KindInvalid, "fs is required").WithRetryable(apierror.False)

	ErrPathRequired = apierror.New(apierror.KindInvalid, "path is required").WithRetryable(apierror.False)

	ErrHashRequired = apierror.New(apierror.KindInvalid, "hash is required").WithRetryable(apierror.False)

	ErrTranscoderNotFound = apierror.New(apierror.KindNotFound, "transcoder not found in context").WithRetryable(apierror.False)

	ErrChannelNotFound = apierror.New(apierror.KindNotFound, "channel not found").WithRetryable(apierror.False)

	ErrTaskNotFound = apierror.New(apierror.KindNotFound, "task not found").WithRetryable(apierror.False)

	ErrNoScriptOrProto = apierror.New(apierror.KindInvalid, "no script or proto provided").WithRetryable(apierror.False)
)

func NewInvalidPoolSizeError() apierror.Error {
	return apierror.New(apierror.KindInvalid, "pool.size must be greater than 0 for non-flex pools").WithRetryable(apierror.False)
}

func NewInvalidWorkerPoolSizeError() apierror.Error {
	return apierror.New(apierror.KindInvalid, "pool.size must be greater than 0 for worker pools").WithRetryable(apierror.False)
}

func NewEmptyImportNameError() apierror.Error {
	return apierror.New(apierror.KindInvalid, "import :name cannot be empty").WithRetryable(apierror.False)
}

func NewModuleNamespaceError() apierror.Error {
	return apierror.New(apierror.KindInvalid, "module cannot have a namespace").WithRetryable(apierror.False)
}

func NewInvalidEntryKindError(got, expected string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid entry kind "+got+", expected "+expected).WithRetryable(apierror.False)
}

func NewValidationError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid configuration: "+cause.Error()).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()}))
}

func NewPoolNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.KindNotFound, "pool not found: "+id).WithRetryable(apierror.False)
}

func NewUnknownPoolTypeError(poolType string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "unknown pool type: "+poolType).WithRetryable(apierror.False)
}

func NewHashVerificationError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "hash verification failed").WithCause(cause).WithRetryable(apierror.False)
}

func NewFilesystemNotFoundError(fsID string) apierror.Error {
	return apierror.New(apierror.KindNotFound, "filesystem not found: "+fsID).WithRetryable(apierror.False)
}

func NewOpenFileError(path string, cause error) apierror.Error {
	return apierror.New(apierror.KindNotFound, "failed to open file: "+path).
		WithCause(cause).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()}))
}

func NewInvalidHashFormatError(hash string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid hash format: "+hash).WithRetryable(apierror.False)
}

func NewUnsupportedHashAlgorithmError(algorithm string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "unsupported hash algorithm: "+algorithm).WithRetryable(apierror.False)
}

func NewHashMismatchError(expected, actual string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "hash mismatch: expected "+expected+", got "+actual).WithRetryable(apierror.False)
}

func NewTopicAlreadySubscribedError(topic string) apierror.Error {
	return apierror.New(apierror.KindAlreadyExists, "topic \""+topic+"\" already subscribed").WithRetryable(apierror.False)
}

func NewMethodNotFoundError(method string) apierror.Error {
	return apierror.New(apierror.KindNotFound, "method \""+method+"\" not found in module").WithRetryable(apierror.False)
}

func NewTaskNotFoundForChannelError(cause error) apierror.Error {
	return apierror.New(apierror.KindNotFound, "task not found for channel result").WithCause(cause).WithRetryable(apierror.False)
}

func NewNotAllowedError(action, target string) apierror.Error {
	return apierror.New(apierror.KindPermissionDenied, "not allowed to "+action+": "+target).WithRetryable(apierror.False)
}

func NewCouldNotResolveError(pidOrName string) apierror.Error {
	return apierror.New(apierror.KindNotFound, "could not resolve '"+pidOrName+"' as PID or registered name").WithRetryable(apierror.False)
}

func NewInvalidFormatError(message string) apierror.Error {
	return apierror.New(apierror.KindInvalid, message).WithRetryable(apierror.False)
}

func NewInvalidTypeError(message string) apierror.Error {
	return apierror.New(apierror.KindInvalid, message).WithRetryable(apierror.False)
}

func NewUnsupportedTypeError(message string) apierror.Error {
	return apierror.New(apierror.KindInvalid, message).WithRetryable(apierror.False)
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

func IsDeadlock(err error) bool {
	var deadlock *DeadlockError
	return errors.As(err, &deadlock)
}
