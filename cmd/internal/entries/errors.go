package entries

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrRegistryClientNotFound = apierror.New(apierror.NotFound, "registry client not found").WithRetryable(apierror.False)

	ErrModuleMissingHash = apierror.New(apierror.Invalid, "module missing hash").WithRetryable(apierror.False)

	ErrNoContentDownloaded = apierror.New(apierror.Internal, "no content downloaded").WithRetryable(apierror.False)

	ErrTranscoderNotFound = apierror.New(apierror.NotFound, "transcoder not found").WithRetryable(apierror.False)

	ErrLoaderNotFound = apierror.New(apierror.NotFound, "loader not found").WithRetryable(apierror.False)

	ErrRegistryNotFound = apierror.New(apierror.NotFound, "registry not found").WithRetryable(apierror.False)

	ErrResolverNotFound = apierror.New(apierror.NotFound, "resolver not found").WithRetryable(apierror.False)
)

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

func NewBuildChangeSetError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to build change set").WithCause(cause).WithRetryable(apierror.False)
}

func NewApplyEntriesError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to apply entries").WithCause(cause).WithRetryable(apierror.False)
}

func NewExtractModuleError(module string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to extract module: "+module).WithCause(cause).WithRetryable(apierror.False)
}

func NewRegisterEmbedResourcesError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to register embed resources").WithCause(cause).WithRetryable(apierror.False)
}

func NewOpenWappError(path string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to open wapp: "+path).WithCause(cause).WithRetryable(apierror.False)
}

func NewReadWappError(path string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to read wapp: "+path).WithCause(cause).WithRetryable(apierror.False)
}
