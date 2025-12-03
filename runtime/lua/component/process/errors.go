package process

import (
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

func (e *Error) Error() string               { return e.message }
func (e *Error) Kind() apierror.Kind         { return e.kind }
func (e *Error) Retryable() apierror.Ternary { return e.retryable }
func (e *Error) Details() attrs.Attributes   { return e.details }
func (e *Error) Unwrap() error               { return e.cause }

func NewInvalidEntryKindError(got, expected string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid entry kind " + got + ", expected " + expected,
		retryable: apierror.False,
	}
}

func NewUnpackConfigError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to unpack process config",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewAddProcessNodeError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to add process node",
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

func NewUpdateProcessNodeError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to update process node",
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

func NewDeleteProcessNodeError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to delete process node",
		retryable: apierror.False,
		cause:     cause,
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

func NewTranscoderNotFoundError() *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "transcoder not found in context",
		retryable: apierror.False,
	}
}

func NewUnmarshalConfigError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to unmarshal config",
		retryable: apierror.False,
		cause:     cause,
	}
}
