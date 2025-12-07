package lua

import (
	"errors"
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

type Error struct {
	kind      apierror.Kind
	message   string
	retryable apierror.Ternary
	details   attrs.Attributes
	cause     error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return e.message + ": " + e.cause.Error()
	}
	return e.message
}
func (e *Error) Kind() apierror.Kind         { return e.kind }
func (e *Error) Retryable() apierror.Ternary { return e.retryable }
func (e *Error) Details() attrs.Attributes   { return e.details }
func (e *Error) Unwrap() error               { return e.cause }

var (
	ErrSourceRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "source is required",
		retryable: apierror.False,
	}

	ErrMethodRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "method is required",
		retryable: apierror.False,
	}

	ErrEmptyImportAlias = &Error{
		kind:      apierror.KindInvalid,
		message:   "import alias cannot be empty",
		retryable: apierror.False,
	}

	ErrEmptyModule = &Error{
		kind:      apierror.KindInvalid,
		message:   "module cannot be empty",
		retryable: apierror.False,
	}

	ErrFSRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "fs is required",
		retryable: apierror.False,
	}

	ErrPathRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "path is required",
		retryable: apierror.False,
	}

	ErrHashRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "hash is required",
		retryable: apierror.False,
	}
)

func NewInvalidPoolSizeError() *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "pool.size must be greater than 0 for non-flex pools",
		retryable: apierror.False,
	}
}

func NewInvalidWorkerPoolSizeError() *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "pool.size must be greater than 0 for worker pools",
		retryable: apierror.False,
	}
}

func NewEmptyImportNameError() *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "import :name cannot be empty",
		retryable: apierror.False,
	}
}

func NewModuleNamespaceError() *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "module cannot have a namespace",
		retryable: apierror.False,
	}
}

// Component errors

var (
	ErrTranscoderNotFound = &Error{
		kind:      apierror.KindNotFound,
		message:   "transcoder not found in context",
		retryable: apierror.False,
	}
)

func NewInvalidEntryKindError(got, expected string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid entry kind " + got + ", expected " + expected,
		retryable: apierror.False,
	}
}

func NewUnpackConfigError(component string, cause error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to unpack " + component + " config",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewUnmarshalConfigError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to unmarshal config",
		retryable: apierror.False,
		cause:     cause,
		details:   attrs.NewBagFrom(map[string]any{"cause": cause.Error()}),
	}
}

func NewValidationError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid configuration: " + cause.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": cause.Error()}),
	}
}

func NewCompileError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to compile",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewAddNodeError(component string, cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to add " + component + " node",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewUpdateNodeError(component string, cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to update " + component + " node",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewDeleteNodeError(component string, cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to delete " + component + " node",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewRegisterFactoryError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to register factory",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewUpdateFactoryError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to update factory",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewCreatePoolError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to create pool",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewReplacePoolError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to replace pool",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewPoolNotFoundError(id string) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "pool not found: " + id,
		retryable: apierror.False,
	}
}

func NewUnknownPoolTypeError(poolType string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "unknown pool type: " + poolType,
		retryable: apierror.False,
	}
}

// Bytecode errors

func NewLoadBytecodeError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to load bytecode",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewUndumpBytecodeError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to undump bytecode",
		retryable: apierror.False,
		cause:     cause,
		details:   attrs.NewBagFrom(map[string]any{"cause": cause.Error()}),
	}
}

func NewHashVerificationError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "hash verification failed",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewFilesystemNotFoundError(fsID string) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "filesystem not found: " + fsID,
		retryable: apierror.False,
	}
}

func NewOpenFileError(path string, cause error) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "failed to open file: " + path,
		retryable: apierror.False,
		cause:     cause,
		details:   attrs.NewBagFrom(map[string]any{"cause": cause.Error()}),
	}
}

func NewInvalidHashFormatError(hash string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid hash format: " + hash,
		retryable: apierror.False,
	}
}

func NewUnsupportedHashAlgorithmError(algorithm string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "unsupported hash algorithm: " + algorithm,
		retryable: apierror.False,
	}
}

func NewHashMismatchError(expected, actual string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "hash mismatch: expected " + expected + ", got " + actual,
		retryable: apierror.False,
	}
}

// Engine errors

var (
	ErrChannelNotFound = &Error{
		kind:      apierror.KindNotFound,
		message:   "channel not found",
		retryable: apierror.False,
	}

	ErrProcessNotInitialized = &Error{
		kind:      apierror.KindInternal,
		message:   "process not initialized",
		retryable: apierror.False,
	}

	ErrTaskNotFound = &Error{
		kind:      apierror.KindNotFound,
		message:   "task not found",
		retryable: apierror.False,
	}

	ErrProcessContextNotAvailable = &Error{
		kind:      apierror.KindInternal,
		message:   "process context not available",
		retryable: apierror.False,
	}

	ErrNoScriptOrProto = &Error{
		kind:      apierror.KindInvalid,
		message:   "no script or proto provided",
		retryable: apierror.False,
	}

	ErrStateNotInitialized = &Error{
		kind:      apierror.KindInternal,
		message:   "process state not initialized - use Factory.Create()",
		retryable: apierror.False,
	}
)

func NewTopicAlreadySubscribedError(topic string) *Error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "topic \"" + topic + "\" already subscribed",
		retryable: apierror.False,
	}
}

func NewStoreResourcesError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to store resources",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewLoadScriptError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to load script",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewExecuteScriptError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to execute script",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewMethodNotFoundError(method string) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "method \"" + method + "\" not found in module",
		retryable: apierror.False,
	}
}

func NewTaskNotFoundForChannelError(cause error) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "task not found for channel result",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewOperationError(operation string, cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   operation,
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewScriptReturnError(message string) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   message,
		retryable: apierror.False,
	}
}

// Payload errors

func NewInvalidFormatError(message string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   message,
		retryable: apierror.False,
	}
}

func NewInvalidTypeError(message string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   message,
		retryable: apierror.False,
	}
}

func NewTranscodeError(message string, cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   message,
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewConversionError(message string, cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   message,
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewUnsupportedTypeError(message string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   message,
		retryable: apierror.False,
	}
}

// DeadlockError indicates all coroutines are blocked with no pending operations.
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
