package entries

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrNoEntriesProvided = apierror.New(apierror.KindInvalid, "no entries provided").WithRetryable(apierror.False)

	ErrRegistryClientNotFound = apierror.New(apierror.KindNotFound, "registry client not found").WithRetryable(apierror.False)

	ErrModuleMissingHash = apierror.New(apierror.KindInvalid, "module missing hash").WithRetryable(apierror.False)

	ErrNoContentDownloaded = apierror.New(apierror.KindInternal, "no content downloaded").WithRetryable(apierror.False)

	ErrTranscoderNotFound = apierror.New(apierror.KindNotFound, "transcoder not found").WithRetryable(apierror.False)

	ErrLoaderNotFound = apierror.New(apierror.KindNotFound, "loader not found").WithRetryable(apierror.False)

	ErrRegistryNotFound = apierror.New(apierror.KindNotFound, "registry not found").WithRetryable(apierror.False)

	ErrResolverNotFound = apierror.New(apierror.KindNotFound, "resolver not found").WithRetryable(apierror.False)
)

func NewLoadEntriesError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to load entries").WithCause(cause).WithRetryable(apierror.False)
}

func NewValidateEntriesError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "failed to validate entries").WithCause(cause).WithRetryable(apierror.False)
}

func NewLoadLockFileError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to load lock file").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidLockFileError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid lock file").WithCause(cause).WithRetryable(apierror.False)
}

func NewEnsureModulesInstalledError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to ensure modules installed").WithCause(cause).WithRetryable(apierror.False)
}

func NewLoadEntriesFromPathsError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to load entries from paths").WithCause(cause).WithRetryable(apierror.False)
}

func NewLoadEntriesToRegistryError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to load entries to registry").WithCause(cause).WithRetryable(apierror.False)
}

func NewDownloadModuleError(module string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to download module: "+module).WithCause(cause).WithRetryable(apierror.False)
}

func NewStoreModuleError(module string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to store module: "+module).WithCause(cause).WithRetryable(apierror.False)
}

func NewLoadFromPathError(path string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to load from path: "+path).WithCause(cause).WithRetryable(apierror.False)
}

func NewExecutePipelineError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to execute pipeline").WithCause(cause).WithRetryable(apierror.False)
}

func NewGetCurrentVersionError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to get current version").WithCause(cause).WithRetryable(apierror.False)
}

func NewLoadStateError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to load state").WithCause(cause).WithRetryable(apierror.False)
}
