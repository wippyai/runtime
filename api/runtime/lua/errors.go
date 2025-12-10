package lua

import (
	"errors"
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

type Error struct {
	Msg         string
	ErrKind     apierror.Kind
	IsRetryable apierror.Ternary
	ErrDetails  attrs.Attributes
	ErrCause    error
}

func (e *Error) Error() string {
	if e.ErrCause != nil {
		return e.Msg + ": " + e.ErrCause.Error()
	}
	return e.Msg
}
func (e *Error) Kind() apierror.Kind         { return e.ErrKind }
func (e *Error) Retryable() apierror.Ternary { return e.IsRetryable }
func (e *Error) Details() attrs.Attributes   { return e.ErrDetails }
func (e *Error) Unwrap() error               { return e.ErrCause }

// Implement apierror.Error interface
var _ apierror.Error = (*Error)(nil)

var (
	ErrSourceRequired = &Error{
		ErrKind:     apierror.KindInvalid,
		Msg:         "source is required",
		IsRetryable: apierror.False,
	}

	ErrMethodRequired = &Error{
		ErrKind:     apierror.KindInvalid,
		Msg:         "method is required",
		IsRetryable: apierror.False,
	}

	ErrEmptyImportAlias = &Error{
		ErrKind:     apierror.KindInvalid,
		Msg:         "import alias cannot be empty",
		IsRetryable: apierror.False,
	}

	ErrEmptyModule = &Error{
		ErrKind:     apierror.KindInvalid,
		Msg:         "module cannot be empty",
		IsRetryable: apierror.False,
	}

	ErrFSRequired = &Error{
		ErrKind:     apierror.KindInvalid,
		Msg:         "fs is required",
		IsRetryable: apierror.False,
	}

	ErrPathRequired = &Error{
		ErrKind:     apierror.KindInvalid,
		Msg:         "path is required",
		IsRetryable: apierror.False,
	}

	ErrHashRequired = &Error{
		ErrKind:     apierror.KindInvalid,
		Msg:         "hash is required",
		IsRetryable: apierror.False,
	}
)

func NewInvalidPoolSizeError() *Error {
	return &Error{
		ErrKind:     apierror.KindInvalid,
		Msg:         "pool.size must be greater than 0 for non-flex pools",
		IsRetryable: apierror.False,
	}
}

func NewInvalidWorkerPoolSizeError() *Error {
	return &Error{
		ErrKind:     apierror.KindInvalid,
		Msg:         "pool.size must be greater than 0 for worker pools",
		IsRetryable: apierror.False,
	}
}

func NewEmptyImportNameError() *Error {
	return &Error{
		ErrKind:     apierror.KindInvalid,
		Msg:         "import :name cannot be empty",
		IsRetryable: apierror.False,
	}
}

func NewModuleNamespaceError() *Error {
	return &Error{
		ErrKind:     apierror.KindInvalid,
		Msg:         "module cannot have a namespace",
		IsRetryable: apierror.False,
	}
}

// Component errors

var (
	ErrTranscoderNotFound = &Error{
		ErrKind:     apierror.KindNotFound,
		Msg:         "transcoder not found in context",
		IsRetryable: apierror.False,
	}
)

func NewInvalidEntryKindError(got, expected string) *Error {
	return &Error{
		ErrKind:     apierror.KindInvalid,
		Msg:         "invalid entry kind " + got + ", expected " + expected,
		IsRetryable: apierror.False,
	}
}

func NewUnpackConfigError(component string, cause error) *Error {
	return &Error{
		ErrKind:     apierror.KindInvalid,
		Msg:         "failed to unpack " + component + " config",
		IsRetryable: apierror.False,
		ErrCause:    cause,
	}
}

func NewUnmarshalConfigError(cause error) *Error {
	return &Error{
		ErrKind:     apierror.KindInvalid,
		Msg:         "failed to unmarshal config",
		IsRetryable: apierror.False,
		ErrCause:    cause,
		ErrDetails:  attrs.NewBagFrom(map[string]any{"cause": cause.Error()}),
	}
}

func NewValidationError(cause error) *Error {
	return &Error{
		ErrKind:     apierror.KindInvalid,
		Msg:         "invalid configuration: " + cause.Error(),
		IsRetryable: apierror.False,
		ErrDetails:  attrs.NewBagFrom(map[string]any{"cause": cause.Error()}),
	}
}

func NewCompileError(cause error) *Error {
	return &Error{
		ErrKind:     apierror.KindInternal,
		Msg:         "failed to compile",
		IsRetryable: apierror.False,
		ErrCause:    cause,
	}
}

func NewAddNodeError(component string, cause error) *Error {
	return &Error{
		ErrKind:     apierror.KindInternal,
		Msg:         "failed to add " + component + " node",
		IsRetryable: apierror.False,
		ErrCause:    cause,
	}
}

func NewUpdateNodeError(component string, cause error) *Error {
	return &Error{
		ErrKind:     apierror.KindInternal,
		Msg:         "failed to update " + component + " node",
		IsRetryable: apierror.False,
		ErrCause:    cause,
	}
}

func NewDeleteNodeError(component string, cause error) *Error {
	return &Error{
		ErrKind:     apierror.KindInternal,
		Msg:         "failed to delete " + component + " node",
		IsRetryable: apierror.False,
		ErrCause:    cause,
	}
}

func NewRegisterFactoryError(cause error) *Error {
	return &Error{
		ErrKind:     apierror.KindInternal,
		Msg:         "failed to register factory",
		IsRetryable: apierror.False,
		ErrCause:    cause,
	}
}

func NewUpdateFactoryError(cause error) *Error {
	return &Error{
		ErrKind:     apierror.KindInternal,
		Msg:         "failed to update factory",
		IsRetryable: apierror.False,
		ErrCause:    cause,
	}
}

func NewCreatePoolError(cause error) *Error {
	return &Error{
		ErrKind:     apierror.KindInternal,
		Msg:         "failed to create pool",
		IsRetryable: apierror.False,
		ErrCause:    cause,
	}
}

func NewReplacePoolError(cause error) *Error {
	return &Error{
		ErrKind:     apierror.KindInternal,
		Msg:         "failed to replace pool",
		IsRetryable: apierror.False,
		ErrCause:    cause,
	}
}

func NewPoolNotFoundError(id string) *Error {
	return &Error{
		ErrKind:     apierror.KindNotFound,
		Msg:         "pool not found: " + id,
		IsRetryable: apierror.False,
	}
}

func NewUnknownPoolTypeError(poolType string) *Error {
	return &Error{
		ErrKind:     apierror.KindInvalid,
		Msg:         "unknown pool type: " + poolType,
		IsRetryable: apierror.False,
	}
}

func NewRegisterCallerError(id fmt.Stringer, cause error) *Error {
	return &Error{
		ErrKind:     apierror.KindInternal,
		Msg:         "failed to register function caller: " + id.String(),
		IsRetryable: apierror.False,
		ErrCause:    cause,
	}
}

func NewUnregisterCallerError(id fmt.Stringer, cause error) *Error {
	return &Error{
		ErrKind:     apierror.KindInternal,
		Msg:         "failed to unregister function caller: " + id.String(),
		IsRetryable: apierror.False,
		ErrCause:    cause,
	}
}

// Bytecode errors

func NewLoadBytecodeError(cause error) *Error {
	return &Error{
		ErrKind:     apierror.KindInternal,
		Msg:         "failed to load bytecode",
		IsRetryable: apierror.False,
		ErrCause:    cause,
	}
}

func NewUndumpBytecodeError(cause error) *Error {
	return &Error{
		ErrKind:     apierror.KindInternal,
		Msg:         "failed to undump bytecode",
		IsRetryable: apierror.False,
		ErrCause:    cause,
		ErrDetails:  attrs.NewBagFrom(map[string]any{"cause": cause.Error()}),
	}
}

func NewHashVerificationError(cause error) *Error {
	return &Error{
		ErrKind:     apierror.KindInvalid,
		Msg:         "hash verification failed",
		IsRetryable: apierror.False,
		ErrCause:    cause,
	}
}

func NewFilesystemNotFoundError(fsID string) *Error {
	return &Error{
		ErrKind:     apierror.KindNotFound,
		Msg:         "filesystem not found: " + fsID,
		IsRetryable: apierror.False,
	}
}

func NewOpenFileError(path string, cause error) *Error {
	return &Error{
		ErrKind:     apierror.KindNotFound,
		Msg:         "failed to open file: " + path,
		IsRetryable: apierror.False,
		ErrCause:    cause,
		ErrDetails:  attrs.NewBagFrom(map[string]any{"cause": cause.Error()}),
	}
}

func NewInvalidHashFormatError(hash string) *Error {
	return &Error{
		ErrKind:     apierror.KindInvalid,
		Msg:         "invalid hash format: " + hash,
		IsRetryable: apierror.False,
	}
}

func NewUnsupportedHashAlgorithmError(algorithm string) *Error {
	return &Error{
		ErrKind:     apierror.KindInvalid,
		Msg:         "unsupported hash algorithm: " + algorithm,
		IsRetryable: apierror.False,
	}
}

func NewHashMismatchError(expected, actual string) *Error {
	return &Error{
		ErrKind:     apierror.KindInvalid,
		Msg:         "hash mismatch: expected " + expected + ", got " + actual,
		IsRetryable: apierror.False,
	}
}

// Engine errors

var (
	ErrChannelNotFound = &Error{
		ErrKind:     apierror.KindNotFound,
		Msg:         "channel not found",
		IsRetryable: apierror.False,
	}

	ErrProcessNotInitialized = &Error{
		ErrKind:     apierror.KindInternal,
		Msg:         "process not initialized",
		IsRetryable: apierror.False,
	}

	ErrTaskNotFound = &Error{
		ErrKind:     apierror.KindNotFound,
		Msg:         "task not found",
		IsRetryable: apierror.False,
	}

	ErrProcessContextNotAvailable = &Error{
		ErrKind:     apierror.KindInternal,
		Msg:         "process context not available",
		IsRetryable: apierror.False,
	}

	ErrNoScriptOrProto = &Error{
		ErrKind:     apierror.KindInvalid,
		Msg:         "no script or proto provided",
		IsRetryable: apierror.False,
	}

	ErrStateNotInitialized = &Error{
		ErrKind:     apierror.KindInternal,
		Msg:         "process state not initialized - use Factory.Create()",
		IsRetryable: apierror.False,
	}
)

func NewTopicAlreadySubscribedError(topic string) *Error {
	return &Error{
		ErrKind:     apierror.KindAlreadyExists,
		Msg:         "topic \"" + topic + "\" already subscribed",
		IsRetryable: apierror.False,
	}
}

func NewStoreResourcesError(cause error) *Error {
	return &Error{
		ErrKind:     apierror.KindInternal,
		Msg:         "failed to store resources",
		IsRetryable: apierror.False,
		ErrCause:    cause,
	}
}

func NewLoadScriptError(cause error) *Error {
	return &Error{
		ErrKind:     apierror.KindInternal,
		Msg:         "failed to load script",
		IsRetryable: apierror.False,
		ErrCause:    cause,
	}
}

func NewExecuteScriptError(cause error) *Error {
	return &Error{
		ErrKind:     apierror.KindInternal,
		Msg:         "failed to execute script",
		IsRetryable: apierror.False,
		ErrCause:    cause,
	}
}

func NewMethodNotFoundError(method string) *Error {
	return &Error{
		ErrKind:     apierror.KindNotFound,
		Msg:         "method \"" + method + "\" not found in module",
		IsRetryable: apierror.False,
	}
}

func NewTaskNotFoundForChannelError(cause error) *Error {
	return &Error{
		ErrKind:     apierror.KindNotFound,
		Msg:         "task not found for channel result",
		IsRetryable: apierror.False,
		ErrCause:    cause,
	}
}

func NewOperationError(operation string, cause error) *Error {
	return &Error{
		ErrKind:     apierror.KindInternal,
		Msg:         operation,
		IsRetryable: apierror.False,
		ErrCause:    cause,
	}
}

func NewScriptReturnError(message string) *Error {
	return &Error{
		ErrKind:     apierror.KindInternal,
		Msg:         message,
		IsRetryable: apierror.False,
	}
}

// Process module errors

var ErrCouldNotAccessRegistry = &Error{
	ErrKind:     apierror.KindInternal,
	Msg:         "could not access registry",
	IsRetryable: apierror.False,
}

func NewNotAllowedError(action, target string) *Error {
	return &Error{
		ErrKind:     apierror.KindPermissionDenied,
		Msg:         "not allowed to " + action + ": " + target,
		IsRetryable: apierror.False,
	}
}

func NewCouldNotResolveError(pidOrName string) *Error {
	return &Error{
		ErrKind:     apierror.KindNotFound,
		Msg:         "could not resolve '" + pidOrName + "' as PID or registered name",
		IsRetryable: apierror.False,
	}
}

// Payload errors

func NewInvalidFormatError(message string) *Error {
	return &Error{
		ErrKind:     apierror.KindInvalid,
		Msg:         message,
		IsRetryable: apierror.False,
	}
}

func NewInvalidTypeError(message string) *Error {
	return &Error{
		ErrKind:     apierror.KindInvalid,
		Msg:         message,
		IsRetryable: apierror.False,
	}
}

func NewTranscodeError(message string, cause error) *Error {
	return &Error{
		ErrKind:     apierror.KindInternal,
		Msg:         message,
		IsRetryable: apierror.False,
		ErrCause:    cause,
	}
}

func NewConversionError(message string, cause error) *Error {
	return &Error{
		ErrKind:     apierror.KindInternal,
		Msg:         message,
		IsRetryable: apierror.False,
		ErrCause:    cause,
	}
}

func NewUnsupportedTypeError(message string) *Error {
	return &Error{
		ErrKind:     apierror.KindInvalid,
		Msg:         message,
		IsRetryable: apierror.False,
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
