package lua

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrProcessNotInitialized = apierror.New(apierror.KindInternal, "process not initialized").WithRetryable(apierror.False)

	ErrProcessContextNotAvailable = apierror.New(apierror.KindInternal, "process context not available").WithRetryable(apierror.False)

	ErrStateNotInitialized = apierror.New(apierror.KindInternal, "process state not initialized - use Factory.Create()").WithRetryable(apierror.False)

	ErrCouldNotAccessRegistry = apierror.New(apierror.KindInternal, "could not access registry").WithRetryable(apierror.False)
)

func NewUnpackConfigError(component string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to unpack "+component+" config").WithCause(cause).WithRetryable(apierror.False)
}

func NewUnmarshalConfigError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to unmarshal config").
		WithCause(cause).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()}))
}

func NewCompileError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to compile").WithCause(cause).WithRetryable(apierror.False)
}

func NewAddNodeError(component string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to add "+component+" node").WithCause(cause).WithRetryable(apierror.False)
}

func NewUpdateNodeError(component string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to update "+component+" node").WithCause(cause).WithRetryable(apierror.False)
}

func NewDeleteNodeError(component string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to delete "+component+" node").WithCause(cause).WithRetryable(apierror.False)
}

func NewRegisterFactoryError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to register factory").WithCause(cause).WithRetryable(apierror.False)
}

func NewUpdateFactoryError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to update factory").WithCause(cause).WithRetryable(apierror.False)
}

func NewCreatePoolError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to create pool").WithCause(cause).WithRetryable(apierror.False)
}

func NewReplacePoolError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to replace pool").WithCause(cause).WithRetryable(apierror.False)
}

func NewModuleInitError(name string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to initialize module: "+name).WithCause(cause).WithRetryable(apierror.False)
}

func NewRegisterCallerError(id fmt.Stringer, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to register function caller: "+id.String()).WithCause(cause).WithRetryable(apierror.False)
}

func NewUnregisterCallerError(id fmt.Stringer, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to unregister function caller: "+id.String()).WithCause(cause).WithRetryable(apierror.False)
}

func NewLoadBytecodeError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to load bytecode").WithCause(cause).WithRetryable(apierror.False)
}

func NewUndumpBytecodeError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to undump bytecode").
		WithCause(cause).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()}))
}

func NewStoreResourcesError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to store resources").WithCause(cause).WithRetryable(apierror.False)
}

func NewLoadScriptError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to load script").WithCause(cause).WithRetryable(apierror.False)
}

func NewExecuteScriptError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to execute script").WithCause(cause).WithRetryable(apierror.False)
}

func NewOperationError(operation string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, operation).WithCause(cause).WithRetryable(apierror.False)
}

func NewRuntimeError(message string) apierror.Error {
	return apierror.New(apierror.KindInternal, message).WithRetryable(apierror.False)
}

func NewRegistryTableError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to access registry table").WithCause(cause).WithRetryable(apierror.False)
}

func NewRegistryAddError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to add to registry").WithCause(cause).WithRetryable(apierror.False)
}

func NewScriptReturnError(message string) apierror.Error {
	return apierror.New(apierror.KindInternal, message).WithRetryable(apierror.False)
}

func NewTranscodeError(message string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, message+": "+cause.Error()).WithCause(cause).WithRetryable(apierror.False)
}

func NewConversionError(message string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, message+": "+cause.Error()).WithCause(cause).WithRetryable(apierror.False)
}
