package lock

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
	ErrModulesDirectoryEmpty = &Error{
		kind:    apierror.KindInvalid,
		message: "directories.modules cannot be empty",
	}
	ErrSrcDirectoryEmpty = &Error{
		kind:    apierror.KindInvalid,
		message: "directories.src cannot be empty",
	}
	ErrSrcDirectoryRoot = &Error{
		kind:    apierror.KindInvalid,
		message: `directories.src cannot be "." (root directory) - this causes duplicate loading of vendor modules. Use a specific subdirectory like "./src" instead`,
	}
	ErrModuleNameEmpty = &Error{
		kind:    apierror.KindInvalid,
		message: "module name cannot be empty",
	}
	ErrOrganizationEmpty = &Error{
		kind:    apierror.KindInvalid,
		message: "organization part cannot be empty",
	}
	ErrModulePartEmpty = &Error{
		kind:    apierror.KindInvalid,
		message: "module part cannot be empty",
	}
	ErrReplacementFromEmpty = &Error{
		kind:    apierror.KindInvalid,
		message: "replacement has empty 'from' field",
	}
)

func NewInvalidModuleError(moduleName string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("invalid module %q", moduleName),
		cause:   cause,
	}
}

func NewModuleEmptyVersionError(moduleName string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("module %q has empty version", moduleName),
	}
}

func NewInvalidReplacementsError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "invalid replacements",
		cause:   cause,
	}
}

func NewReplacementToEmptyError(from string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("replacement %q has empty 'to' field", from),
	}
}

func NewReplacementFromInvalidError(from string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("replacement 'from' field %q is invalid", from),
		cause:   cause,
	}
}

func NewReplacementPathNotExistError(path string) *Error {
	return &Error{
		kind:    apierror.KindNotFound,
		message: fmt.Sprintf("replacement path %q does not exist", path),
	}
}

func NewCheckReplacementPathError(path string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("failed to check replacement path %q", path),
		cause:   cause,
	}
}

func NewInvalidFormatError(name string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("invalid format (expected org/module, got %q)", name),
	}
}

func NewResolveAbsolutePathError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to resolve absolute path",
		cause:   cause,
	}
}

func NewReadLockFileError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to read lock file",
		cause:   cause,
	}
}

func NewStatLockFileError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to stat lock file",
		cause:   cause,
	}
}

func NewReadFileError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to read file",
		cause:   cause,
	}
}

func NewUnmarshalYAMLError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "failed to unmarshal yaml",
		cause:   cause,
	}
}

func NewMarshalYAMLError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to marshal yaml",
		cause:   cause,
	}
}

func NewWriteFileError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to write file",
		cause:   cause,
	}
}
