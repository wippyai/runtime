package component

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Error implements apierror.Error for component errors
type Error struct {
	kind      apierror.Kind
	message   string
	retryable apierror.Ternary
	details   attrs.Attributes
}

func (e *Error) Error() string               { return e.message }
func (e *Error) Kind() apierror.Kind         { return e.kind }
func (e *Error) Retryable() apierror.Ternary { return e.retryable }
func (e *Error) Details() attrs.Attributes   { return e.details }

// Sentinel errors
var (
	ErrTranscoderNotFound = &Error{
		kind:      apierror.KindNotFound,
		message:   "transcoder not found in context",
		retryable: apierror.False,
	}
)

// NewUnmarshalError creates an error for config unmarshal failures
func NewUnmarshalError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to unmarshal config: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
	}
}

// NewValidationError creates an error for config validation failures
func NewValidationError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid configuration: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
	}
}

// NewFilesystemNotFoundError creates an error for missing filesystem
func NewFilesystemNotFoundError(fsID string) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "filesystem not found: " + fsID,
		retryable: apierror.False,
	}
}

// NewOpenFileError creates an error for file open failures
func NewOpenFileError(path string, err error) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "failed to open file: " + path,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
	}
}

// NewInvalidHashFormatError creates an error for invalid hash format
func NewInvalidHashFormatError(hash string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid hash format: " + hash,
		retryable: apierror.False,
	}
}

// NewUnsupportedHashAlgorithmError creates an error for unsupported hash algorithms
func NewUnsupportedHashAlgorithmError(algorithm string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "unsupported hash algorithm: " + algorithm,
		retryable: apierror.False,
	}
}

// NewHashMismatchError creates an error for hash verification failures
func NewHashMismatchError(expected, actual string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "hash mismatch: expected " + expected + ", got " + actual,
		retryable: apierror.False,
	}
}

// NewUndumpBytecodeError creates an error for bytecode undump failures
func NewUndumpBytecodeError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to undump bytecode: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
	}
}
