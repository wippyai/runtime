package wasm

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrProcessNotInitialized = apierror.New(apierror.Internal, "process not initialized").WithRetryable(apierror.False)

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

func NewCreatePoolError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create pool").WithCause(cause).WithRetryable(apierror.False)
}

func NewReplacePoolError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to replace pool").WithCause(cause).WithRetryable(apierror.False)
}

func NewRegisterCallerError(id fmt.Stringer, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to register function caller: "+id.String()).WithCause(cause).WithRetryable(apierror.False)
}

func NewLoadWASMError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to load wasm").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidPoolSizeError() apierror.Error {
	return apierror.New(apierror.Invalid, "pool.size must be greater than 0 for non-flex pools").WithRetryable(apierror.False)
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
