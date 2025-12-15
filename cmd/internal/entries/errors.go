package entries

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrNoEntriesProvided = apierror.New(apierror.Invalid, "no entries provided").WithRetryable(apierror.False)

	ErrRegistryClientNotFound = apierror.New(apierror.NotFound, "registry client not found").WithRetryable(apierror.False)

	ErrModuleMissingHash = apierror.New(apierror.Invalid, "module missing hash").WithRetryable(apierror.False)

	ErrNoContentDownloaded = apierror.New(apierror.Internal, "no content downloaded").WithRetryable(apierror.False)

	ErrTranscoderNotFound = apierror.New(apierror.NotFound, "transcoder not found").WithRetryable(apierror.False)

	ErrLoaderNotFound = apierror.New(apierror.NotFound, "loader not found").WithRetryable(apierror.False)

	ErrRegistryNotFound = apierror.New(apierror.NotFound, "registry not found").WithRetryable(apierror.False)

	ErrResolverNotFound = apierror.New(apierror.NotFound, "resolver not found").WithRetryable(apierror.False)
)

func NewLoadEntriesError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to load entries").WithCause(cause).WithRetryable(apierror.False)
}

func NewValidateEntriesError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to validate entries").WithCause(cause).WithRetryable(apierror.False)
}

func NewLoadLockFileError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to load lock file").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidLockFileError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid lock file").WithCause(cause).WithRetryable(apierror.False)
}

func NewEnsureModulesInstalledError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to ensure modules installed").WithCause(cause).WithRetryable(apierror.False)
}

func NewLoadEntriesFromPathsError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to load entries from paths").WithCause(cause).WithRetryable(apierror.False)
}

func NewLoadEntriesToRegistryError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to load entries to registry").WithCause(cause).WithRetryable(apierror.False)
}

func NewDownloadModuleError(module string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to download module: "+module).WithCause(cause).WithRetryable(apierror.False)
}

func NewStoreModuleError(module string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to store module: "+module).WithCause(cause).WithRetryable(apierror.False)
}

func NewLoadFromPathError(path string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to load from path: "+path).WithCause(cause).WithRetryable(apierror.False)
}

func NewExecutePipelineError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to execute pipeline").WithCause(cause).WithRetryable(apierror.False)
}

func NewGetCurrentVersionError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to get current version").WithCause(cause).WithRetryable(apierror.False)
}

func NewLoadStateError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to load state").WithCause(cause).WithRetryable(apierror.False)
}
