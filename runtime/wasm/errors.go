// Package wasm provides WASM runtime integration for the wippy runtime.
package wasm

import (
	"fmt"
	"strings"

	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrRuntimeNotStarted = apierror.New(apierror.Internal, "wasm runtime is not started").WithRetryable(apierror.False)

	ErrTransportRegistryNotFound = apierror.New(apierror.NotFound, "wasm transport registry not found in context").WithRetryable(apierror.False)

	ErrTranscoderNotFound = apierror.New(apierror.NotFound, "payload transcoder not found in context").WithRetryable(apierror.False)
)

func NewUnpackConfigError(component string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to unpack "+component+" config").WithCause(cause).WithRetryable(apierror.False)
}

func NewValidationError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid configuration").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidEntryKindError(got string, expected ...string) apierror.Error {
	msg := "invalid entry kind " + got
	if len(expected) > 0 {
		msg += ", expected " + strings.Join(expected, " or ")
	}
	return apierror.New(apierror.Invalid, msg).WithRetryable(apierror.False)
}

func NewPoolNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.NotFound, "pool not found: "+id).WithRetryable(apierror.False)
}

func NewUnknownPoolTypeError(poolType string) apierror.Error {
	return apierror.New(apierror.Invalid, "unknown pool type: "+poolType).WithRetryable(apierror.False)
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

func NewLoadWATError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to load wat module").WithCause(cause).WithRetryable(apierror.False)
}

func NewLoadWASMError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to load wasm module").WithCause(cause).WithRetryable(apierror.False)
}

func NewCompileModuleError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to compile wasm module").WithCause(cause).WithRetryable(apierror.False)
}

func NewFilesystemReadError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to read wasm module from filesystem").WithCause(cause).WithRetryable(apierror.False)
}

func NewHashVerificationError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "hash verification failed").WithCause(cause).WithRetryable(apierror.False)
}

func NewFilesystemNotFoundError(fsID string) apierror.Error {
	return apierror.New(apierror.NotFound, "filesystem not found: "+fsID).WithRetryable(apierror.False)
}

func NewOpenFileError(path string, cause error) apierror.Error {
	return apierror.New(apierror.NotFound, "failed to open file: "+path).WithCause(cause).WithRetryable(apierror.False)
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

func NewInstantiateModuleError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to instantiate wasm module").WithCause(cause).WithRetryable(apierror.False)
}

func NewCallMethodError(method string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to call wasm method: "+method).WithCause(cause).WithRetryable(apierror.False)
}

func NewUnsupportedTransportError(transport string) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported transport: "+transport).WithRetryable(apierror.False)
}

func NewTransportNotFoundError(transport string) apierror.Error {
	return apierror.New(apierror.NotFound, "transport not found: "+transport).WithRetryable(apierror.False)
}

func NewTransportTypeError(transport string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid transport implementation for: "+transport).WithRetryable(apierror.False)
}

func NewTransportPrepareError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to prepare transport input").WithCause(cause).WithRetryable(apierror.False)
}

func NewTransportEncodeError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to encode transport result").WithCause(cause).WithRetryable(apierror.False)
}

func NewTranscodePayloadError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to transcode payload").WithCause(cause).WithRetryable(apierror.False)
}
