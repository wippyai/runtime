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
