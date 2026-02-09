package wasm

import (
	"errors"
	"strings"
	"testing"

	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

func assertErr(t *testing.T, err apierror.Error, kind apierror.Kind, contains string) {
	t.Helper()
	if err == nil {
		t.Fatal("error is nil")
	}
	if err.Kind() != kind {
		t.Fatalf("Kind() = %v, want %v", err.Kind(), kind)
	}
	if err.Retryable() != apierror.False {
		t.Fatalf("Retryable() = %v, want %v", err.Retryable(), apierror.False)
	}
	if contains != "" && !strings.Contains(err.Error(), contains) {
		t.Fatalf("Error() = %q, want contains %q", err.Error(), contains)
	}
}

func TestErrorConstructors(t *testing.T) {
	cause := errors.New("boom")
	id := registry.NewID("app.test", "fn")

	assertErr(t, NewUnpackConfigError("function.wasm", cause), apierror.Internal, "failed to unpack function.wasm config")
	assertErr(t, NewValidationError(cause), apierror.Invalid, "invalid configuration")
	assertErr(t, NewInvalidEntryKindError("x", "a", "b"), apierror.Invalid, "invalid entry kind x, expected a or b")
	assertErr(t, NewPoolNotFoundError("id"), apierror.NotFound, "pool not found: id")
	assertErr(t, NewUnknownPoolTypeError("burst"), apierror.Invalid, "unknown pool type: burst")
	assertErr(t, NewCreatePoolError(cause), apierror.Internal, "failed to create pool")
	assertErr(t, NewReplacePoolError(cause), apierror.Internal, "failed to replace pool")
	assertErr(t, NewRegisterCallerError(&id, cause), apierror.Internal, "failed to register function caller")
	assertErr(t, NewRegisterProcessFactoryError(&id, cause), apierror.Internal, "failed to register process factory")
	assertErr(t, NewLoadWATError(cause), apierror.Internal, "failed to load wat module")
	assertErr(t, NewLoadWASMError(cause), apierror.Internal, "failed to load wasm module")
	assertErr(t, NewCompileModuleError(cause), apierror.Internal, "failed to compile wasm module")
	assertErr(t, NewFilesystemReadError(cause), apierror.Internal, "failed to read wasm module from filesystem")
	assertErr(t, NewHashVerificationError(cause), apierror.Invalid, "hash verification failed")
	assertErr(t, NewFilesystemNotFoundError("app.fs:data"), apierror.NotFound, "filesystem not found: app.fs:data")
	assertErr(t, NewOpenFileError("/tmp/mod.wasm", cause), apierror.NotFound, "failed to open file: /tmp/mod.wasm")
	assertErr(t, NewInvalidHashFormatError("sha256"), apierror.Invalid, "invalid hash format: sha256")
	assertErr(t, NewUnsupportedHashAlgorithmError("sha1"), apierror.Invalid, "unsupported hash algorithm: sha1")
	assertErr(t, NewHashMismatchError("a", "b"), apierror.Invalid, "hash mismatch: expected a, got b")
	assertErr(t, NewInstantiateModuleError(cause), apierror.Internal, "failed to instantiate wasm module")
	assertErr(t, NewCallMethodError("run", cause), apierror.Internal, "failed to call wasm method: run")
	assertErr(t, NewUnsupportedTransportError("x"), apierror.Invalid, "unsupported transport: x")
	assertErr(t, NewTransportNotFoundError("payload"), apierror.NotFound, "transport not found: payload")
	assertErr(t, NewTransportTypeError("payload"), apierror.Invalid, "invalid transport implementation for: payload")
	assertErr(t, NewTransportPrepareError(cause), apierror.Internal, "failed to prepare transport input")
	assertErr(t, NewTransportEncodeError(cause), apierror.Internal, "failed to encode transport result")
	assertErr(t, NewWASIEnvLookupError("app.env:key", cause), apierror.Internal, "failed to resolve wasi env mapping: app.env:key")
	assertErr(t, NewWASIEnvRequiredNotFoundError("app.env:key"), apierror.NotFound, "required wasi env variable not found: app.env:key")
	assertErr(t, NewWASIMountFilesystemNotFoundError("app.fs:data"), apierror.NotFound, "wasi mount filesystem not found: app.fs:data")
	assertErr(t, NewTranscodePayloadError(cause), apierror.Internal, "failed to transcode payload")
	assertErr(t, NewRegisterHostError("wasi:io/poll", cause), apierror.Internal, "failed to register wasm host: wasi:io/poll")
	assertErr(t, NewInvalidFunctionTargetError("bad"), apierror.Invalid, "invalid function target: bad")
	assertErr(t, NewUnsupportedHostImportError("foo"), apierror.Invalid, "unsupported wasm host import: foo")
	assertErr(t, NewComponentHostImportError("foo"), apierror.Invalid, "wasm host import requires component module: foo")
	assertErr(t, NewFSAccessDeniedError("app.fs:data"), apierror.PermissionDenied, "not allowed to access filesystem: app.fs:data")
	assertErr(t, ErrNetServiceNotFound, apierror.NotFound, "network service not found")
}
