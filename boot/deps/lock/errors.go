package lock

import (
	"fmt"

	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrModulesDirectoryEmpty = apierror.New(apierror.KindInvalid, "directories.modules cannot be empty").WithRetryable(apierror.False)
	ErrSrcDirectoryEmpty     = apierror.New(apierror.KindInvalid, "directories.src cannot be empty").WithRetryable(apierror.False)
	ErrSrcDirectoryRoot      = apierror.New(apierror.KindInvalid, `directories.src cannot be "." (root directory) - this causes duplicate loading of vendor modules. Use a specific subdirectory like "./src" instead`).WithRetryable(apierror.False)
	ErrModuleNameEmpty       = apierror.New(apierror.KindInvalid, "module name cannot be empty").WithRetryable(apierror.False)
	ErrOrganizationEmpty     = apierror.New(apierror.KindInvalid, "organization part cannot be empty").WithRetryable(apierror.False)
	ErrModulePartEmpty       = apierror.New(apierror.KindInvalid, "module part cannot be empty").WithRetryable(apierror.False)
	ErrReplacementFromEmpty  = apierror.New(apierror.KindInvalid, "replacement has empty 'from' field").WithRetryable(apierror.False)
)

func NewInvalidModuleError(moduleName string, cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("invalid module %q", moduleName)).WithCause(cause)
}

func NewModuleEmptyVersionError(moduleName string) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("module %q has empty version", moduleName))
}

func NewInvalidReplacementsError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid replacements").WithCause(cause)
}

func NewReplacementToEmptyError(from string) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("replacement %q has empty 'to' field", from))
}

func NewReplacementFromInvalidError(from string, cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("replacement 'from' field %q is invalid", from)).WithCause(cause)
}

func NewReplacementPathNotExistError(path string) apierror.Error {
	return apierror.New(apierror.KindNotFound, fmt.Sprintf("replacement path %q does not exist", path))
}

func NewCheckReplacementPathError(path string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, fmt.Sprintf("failed to check replacement path %q", path)).WithCause(cause)
}

func NewInvalidFormatError(name string) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("invalid format (expected org/module, got %q)", name))
}

func NewResolveAbsolutePathError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to resolve absolute path").WithCause(cause)
}

func NewReadLockFileError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to read lock file").WithCause(cause)
}

func NewStatLockFileError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to stat lock file").WithCause(cause)
}

func NewReadFileError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to read file").WithCause(cause)
}

func NewUnmarshalYAMLError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "failed to unmarshal yaml").WithCause(cause)
}

func NewMarshalYAMLError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to marshal yaml").WithCause(cause)
}

func NewWriteFileError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to write file").WithCause(cause)
}
