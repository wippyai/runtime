// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"fmt"

	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrRegistryNotFound = apierror.New(apierror.NotFound, "registry not found").WithRetryable(apierror.False)

	ErrProcessManagerNotAvailable = apierror.New(apierror.Internal, "process manager not available").WithRetryable(apierror.False)

	ErrTranscoderNotFound = apierror.New(apierror.NotFound, "transcoder not found").WithRetryable(apierror.False)

	ErrDependencyResolverNotFound = apierror.New(apierror.NotFound, "dependency resolver not found").WithRetryable(apierror.False)
)

func NewCreateLoggerError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create logger").WithCause(cause).WithRetryable(apierror.False)
}

func NewLoadLockFileError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to load lock file").WithCause(cause).WithRetryable(apierror.False)
}

func NewWriteLockFileError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to write lock file").WithCause(cause).WithRetryable(apierror.False)
}

func NewInitAppError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to initialize app").WithCause(cause).WithRetryable(apierror.False)
}

func NewInitFailedError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "initialization failed").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidLockFileError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid lock file").WithCause(cause).WithRetryable(apierror.False)
}

func NewParseModuleNameError(name string, cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to parse module name: "+name).WithCause(cause).WithRetryable(apierror.False)
}

func NewCheckModuleError(module string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to check module: "+module).WithCause(cause).WithRetryable(apierror.False)
}

func NewModuleMissingHashError(module string) apierror.Error {
	return apierror.New(apierror.Invalid, "module missing hash: "+module).WithRetryable(apierror.False)
}

func NewDownloadModuleError(module string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to download module: "+module).WithCause(cause).WithRetryable(apierror.False)
}

func NewNoContentDownloadedError(module string) apierror.Error {
	return apierror.New(apierror.Internal, "no content downloaded for module: "+module).WithRetryable(apierror.False)
}

func NewStoreModuleError(module string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to store module: "+module).WithCause(cause).WithRetryable(apierror.False)
}

func NewExtractModuleError(module string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to extract module: "+module).WithCause(cause).WithRetryable(apierror.False)
}

func NewLockFileNotFoundError(cause error) apierror.Error {
	return apierror.New(apierror.NotFound, "lock file not found").WithCause(cause).WithRetryable(apierror.False)
}

func NewEnsureModulesInstalledError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to ensure modules installed").WithCause(cause).WithRetryable(apierror.False)
}

func NewLoadEntriesError(path string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to load entries from: "+path).WithCause(cause).WithRetryable(apierror.False)
}

func NewExecutePipelineError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to execute pipeline").WithCause(cause).WithRetryable(apierror.False)
}

func NewParseMetadataError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to parse metadata").WithCause(cause).WithRetryable(apierror.False)
}

func NewCreatePackFileError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create pack file").WithCause(cause).WithRetryable(apierror.False)
}

func NewPackWithResourcesError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to pack with resources").WithCause(cause).WithRetryable(apierror.False)
}

func NewPackEntriesError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to pack entries").WithCause(cause).WithRetryable(apierror.False)
}

func NewClosePackFileError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to close pack file").WithCause(cause).WithRetryable(apierror.False)
}

func NewPackIntegrityError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "pack integrity verification failed").WithCause(cause).WithRetryable(apierror.False)
}

func NewStatOutputFileError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to stat output file").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidMetadataFormatError(format string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid metadata format: "+format).WithRetryable(apierror.False)
}

func NewEmptyMetadataKeyError(flag string) apierror.Error {
	return apierror.New(apierror.Invalid, "metadata key cannot be empty: "+flag).WithRetryable(apierror.False)
}

func NewReservedMetadataNamespaceError(key string) apierror.Error {
	return apierror.New(apierror.Invalid, "reserved metadata namespace: "+key).WithRetryable(apierror.False)
}

func NewInitializeBootstrapContextError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to initialize bootstrap context").WithCause(cause).WithRetryable(apierror.False)
}

func NewCreateLoaderError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create loader").WithCause(cause).WithRetryable(apierror.False)
}

func NewLoadComponentsError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to load components").WithCause(cause).WithRetryable(apierror.False)
}

func NewStartComponentsError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to start components").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidOverrideError(override string, cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid override: "+override).WithCause(cause).WithRetryable(apierror.False)
}

func NewMissingSeparatorError(separator string, format string) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("missing separator '%s', expected format: %s", separator, format)).WithRetryable(apierror.False)
}

func NewEmptyFieldError(field string) apierror.Error {
	return apierror.New(apierror.Invalid, field+" cannot be empty").WithRetryable(apierror.False)
}

func NewInvalidExecSpecError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid exec spec").WithCause(cause).WithRetryable(apierror.False)
}

func NewStartProcessError(hostID string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to start process on host: "+hostID).WithCause(cause).WithRetryable(apierror.False)
}

func NewOpenPackFileError(path string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to open pack file: "+path).WithCause(cause).WithRetryable(apierror.False)
}

func NewCreatePackReaderError(path string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create pack reader: "+path).WithCause(cause).WithRetryable(apierror.False)
}

func NewReadEntriesError(path string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to read entries from: "+path).WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidExistingLockFileError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid existing lock file").WithCause(cause).WithRetryable(apierror.False)
}

func NewLoadEntriesFromSourceError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to load entries from source").WithCause(cause).WithRetryable(apierror.False)
}

func NewCreateHubClientError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create hub client").WithCause(cause).WithRetryable(apierror.False)
}

func NewBuildDependencyGraphError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to build dependency graph").WithCause(cause).WithRetryable(apierror.False)
}

func NewInstallFailedError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "install failed").WithCause(cause).WithRetryable(apierror.False)
}

func NewLintFailedError(errors, warnings int) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("lint failed: %d errors, %d warnings", errors, warnings)).WithRetryable(apierror.False)
}
