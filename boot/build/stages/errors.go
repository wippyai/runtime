package stages

import (
	"fmt"

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

var (
	ErrTranscoderNotFound = &Error{
		kind:    apierror.KindInternal,
		message: "transcoder not found in context",
	}
	ErrEmptyKey = &Error{
		kind:    apierror.KindInvalid,
		message: "empty key",
	}
	ErrEmptyNamespace = &Error{
		kind:    apierror.KindInvalid,
		message: "empty namespace",
	}
	ErrEmptyEntryName = &Error{
		kind:    apierror.KindInvalid,
		message: "empty entry name",
	}
	ErrEmptyPath = &Error{
		kind:    apierror.KindInvalid,
		message: "empty path",
	}
	ErrNoMatchingEntries = &Error{
		kind:    apierror.KindNotFound,
		message: "no matching entries found",
	}
	ErrNoValueAvailable = &Error{
		kind:    apierror.KindNotFound,
		message: "no value available: no dependency parameter found and no default value specified",
	}
)

func NewInvalidNamespacePatternError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "invalid namespace pattern",
		cause:   cause,
	}
}

func NewInvalidEntryPatternError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "invalid entry pattern",
		cause:   cause,
	}
}

func NewInvalidEntryPatternFormatError(pattern string, reason string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("invalid entry pattern '%s': %s", pattern, reason),
	}
}

func NewLoadDirectoryError(dir string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("failed to load directory %s", dir),
		cause:   cause,
	}
}

func NewInvalidKeyError(key string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("invalid key %s", key),
		cause:   cause,
	}
}

func NewEntryNotFoundError(namespace, entryName string) *Error {
	return &Error{
		kind:    apierror.KindNotFound,
		message: fmt.Sprintf("no entry found for %s:%s", namespace, entryName),
	}
}

func NewSetValueError(namespace, entryName, path string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("failed to set %s:%s at %s", namespace, entryName, path),
		cause:   cause,
	}
}

func NewOverrideErrors(errs []error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("override errors: %v", errs),
	}
}

func NewMissingSeparatorError(separator, expected string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("missing %s separator (expected %s)", separator, expected),
	}
}

func NewMissingFieldError(field string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("missing %s", field),
	}
}

func NewDecodeRequirementError(entryID string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("failed to decode requirement %s", entryID),
		cause:   cause,
	}
}

func NewDecodeDependencyError(entryID string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("failed to decode dependency %s", entryID),
		cause:   cause,
	}
}

func NewRequirementError(requirementName, namespace string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("requirement %s in namespace %s failed", requirementName, namespace),
		cause:   cause,
	}
}

func NewNoTargetsError(requirementID string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("invalid requirement %s: no targets defined in requirement definition", requirementID),
	}
}

func NewRequirementTargetError(requirement, targetEntry, path string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("requirement %s, target entry=%s path=%s failed", requirement, targetEntry, path),
		cause:   cause,
	}
}

func NewParameterConflictError(conflicts string) *Error {
	return &Error{
		kind:    apierror.KindConflict,
		message: fmt.Sprintf("parameter conflict: multiple dependencies define different values: %s", conflicts),
	}
}

func NewAppendToEntryError(entryID string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("failed to append to entry %s", entryID),
		cause:   cause,
	}
}

func NewSetValueInEntryError(entryID string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("failed to set value in entry %s", entryID),
		cause:   cause,
	}
}

func NewReadDumpFileError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "read dump file",
		cause:   cause,
	}
}

func NewUnmarshalDumpFileError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "unmarshal dump file",
		cause:   cause,
	}
}

func NewConvertEntryError(entryID string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("convert entry %s", entryID),
		cause:   cause,
	}
}

// Bytecode stage errors

var (
	ErrBytecodeNoData = &Error{
		kind:    apierror.KindInvalid,
		message: "entry has no data",
	}
	ErrBytecodeInvalidData = &Error{
		kind:    apierror.KindInvalid,
		message: "entry data is not a map",
	}
	ErrBytecodeNoSource = &Error{
		kind:    apierror.KindInvalid,
		message: "entry has no source field",
	}
	ErrBytecodeUnsupportedKind = &Error{
		kind:    apierror.KindInvalid,
		message: "unsupported entry kind for bytecode compilation",
	}
)

func NewBytecodeCompileError(entryID fmt.Stringer, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("failed to compile bytecode for %s", entryID),
		cause:   cause,
	}
}

func NewBytecodeTransformError(entryID fmt.Stringer, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("failed to transform entry %s to bytecode config", entryID),
		cause:   cause,
	}
}

func NewBytecodeParseError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "failed to parse Lua source",
		cause:   cause,
	}
}

func NewBytecodeCompileLuaError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to compile Lua source",
		cause:   cause,
	}
}

func NewBytecodeDumpError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to dump bytecode",
		cause:   cause,
	}
}

func NewBytecodeTranscodeError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to transcode entry data",
		cause:   cause,
	}
}
