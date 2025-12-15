package lua

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrProcessNotInitialized = apierror.New(apierror.Internal, "process not initialized").WithRetryable(apierror.False)

	ErrProcessContextNotAvailable = apierror.New(apierror.Internal, "process context not available").WithRetryable(apierror.False)

	ErrStateNotInitialized = apierror.New(apierror.Internal, "process state not initialized - use Factory.Create()").WithRetryable(apierror.False)

	ErrCouldNotAccessRegistry = apierror.New(apierror.Internal, "could not access registry").WithRetryable(apierror.False)
)

func NewUnpackConfigError(component string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to unpack "+component+" config").WithCause(cause).WithRetryable(apierror.False)
}

func NewUnmarshalConfigError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to unmarshal config").
		WithCause(cause).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()}))
}

func NewCompileError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to compile").WithCause(cause).WithRetryable(apierror.False)
}

func NewAddNodeError(component string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to add "+component+" node").WithCause(cause).WithRetryable(apierror.False)
}

func NewUpdateNodeError(component string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to update "+component+" node").WithCause(cause).WithRetryable(apierror.False)
}

func NewDeleteNodeError(component string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to delete "+component+" node").WithCause(cause).WithRetryable(apierror.False)
}

func NewRegisterFactoryError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to register factory").WithCause(cause).WithRetryable(apierror.False)
}

func NewUpdateFactoryError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to update factory").WithCause(cause).WithRetryable(apierror.False)
}

func NewCreatePoolError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create pool").WithCause(cause).WithRetryable(apierror.False)
}

func NewReplacePoolError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to replace pool").WithCause(cause).WithRetryable(apierror.False)
}

func NewModuleInitError(name string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to initialize module: "+name).WithCause(cause).WithRetryable(apierror.False)
}

func NewRegisterCallerError(id fmt.Stringer, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to register function caller: "+id.String()).WithCause(cause).WithRetryable(apierror.False)
}

func NewUnregisterCallerError(id fmt.Stringer, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to unregister function caller: "+id.String()).WithCause(cause).WithRetryable(apierror.False)
}

func NewLoadBytecodeError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to load bytecode").WithCause(cause).WithRetryable(apierror.False)
}

func NewUndumpBytecodeError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to undump bytecode").
		WithCause(cause).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()}))
}

func NewStoreResourcesError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to store resources").WithCause(cause).WithRetryable(apierror.False)
}

func NewLoadScriptError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to load script").WithCause(cause).WithRetryable(apierror.False)
}

func NewExecuteScriptError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to execute script").WithCause(cause).WithRetryable(apierror.False)
}

func NewOperationError(operation string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, operation).WithCause(cause).WithRetryable(apierror.False)
}

func NewRuntimeError(message string) apierror.Error {
	return apierror.New(apierror.Internal, message).WithRetryable(apierror.False)
}

func NewRegistryTableError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to access registry table").WithCause(cause).WithRetryable(apierror.False)
}

func NewRegistryAddError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to add to registry").WithCause(cause).WithRetryable(apierror.False)
}

func NewScriptReturnError(message string) apierror.Error {
	return apierror.New(apierror.Internal, message).WithRetryable(apierror.False)
}

func NewTranscodeError(message string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, message+": "+cause.Error()).WithCause(cause).WithRetryable(apierror.False)
}

func NewConversionError(message string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, message+": "+cause.Error()).WithCause(cause).WithRetryable(apierror.False)
}

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
