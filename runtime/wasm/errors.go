// SPDX-License-Identifier: MPL-2.0

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

	ErrFunctionRegistryNotFound = apierror.New(apierror.NotFound, "function registry not found in context").WithRetryable(apierror.False)

	ErrDispatcherNotFound = apierror.New(apierror.NotFound, "dispatcher not found").WithRetryable(apierror.False)

	ErrNetServiceNotFound = apierror.New(apierror.NotFound, "network service not found").WithRetryable(apierror.False)

	ErrEnvRegistryNotFound = apierror.New(apierror.NotFound, "env registry not found in context").WithRetryable(apierror.False)

	ErrFSRegistryNotFound = apierror.New(apierror.NotFound, "filesystem registry not found").WithRetryable(apierror.False)
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

func NewRegisterProcessFactoryError(id fmt.Stringer, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to register process factory: "+id.String()).WithCause(cause).WithRetryable(apierror.False)
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

func NewWASIEnvLookupError(id string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to resolve wasi env mapping: "+id).WithCause(cause).WithRetryable(apierror.False)
}

func NewWASIEnvRequiredNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.NotFound, "required wasi env variable not found: "+id).WithRetryable(apierror.False)
}

func NewWASIMountFilesystemNotFoundError(fsID string) apierror.Error {
	return apierror.New(apierror.NotFound, "wasi mount filesystem not found: "+fsID).WithRetryable(apierror.False)
}

func NewTranscodePayloadError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to transcode payload").WithCause(cause).WithRetryable(apierror.False)
}

func NewRegisterHostError(host string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to register wasm host: "+host).WithCause(cause).WithRetryable(apierror.False)
}

func NewAsyncPendingCommandError(op any) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("failed to map wasm async pending operation to dispatcher command: %T", op)).
		WithRetryable(apierror.False)
}

func NewAsyncYieldResultTypeError(data any) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("unsupported wasm yield completion data type: %T", data)).
		WithRetryable(apierror.False)
}

func NewInvalidFunctionTargetError(target string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid function target: "+target).WithRetryable(apierror.False)
}

func NewUnsupportedHostImportError(importID string) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported wasm host import: "+importID).WithRetryable(apierror.False)
}

func NewComponentHostImportError(importID string) apierror.Error {
	return apierror.New(apierror.Invalid, "wasm host import requires component module: "+importID).WithRetryable(apierror.False)
}

func NewFSAccessDeniedError(id string) apierror.Error {
	return apierror.New(apierror.PermissionDenied, "not allowed to access filesystem: "+id).WithRetryable(apierror.False)
}
