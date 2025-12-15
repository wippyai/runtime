package cmd

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrInvalidRegistryFormat = apierror.New(apierror.KindInvalid, "invalid registry format").WithRetryable(apierror.False)

	ErrRegistryNotFound = apierror.New(apierror.KindNotFound, "registry not found").WithRetryable(apierror.False)

	ErrInvalidEntryFormat = apierror.New(apierror.KindInvalid, "invalid entry format").WithRetryable(apierror.False)

	ErrEntryNotFound = apierror.New(apierror.KindNotFound, "entry not found").WithRetryable(apierror.False)

	ErrEmptyPattern = apierror.New(apierror.KindInvalid, "pattern cannot be empty").WithRetryable(apierror.False)

	ErrInvalidManifestFormat = apierror.New(apierror.KindInvalid, "invalid manifest format").WithRetryable(apierror.False)

	ErrManifestNotFound = apierror.New(apierror.KindNotFound, "manifest not found").WithRetryable(apierror.False)

	ErrInvalidProjectDir = apierror.New(apierror.KindInvalid, "invalid project directory").WithRetryable(apierror.False)

	ErrNoProjectDir = apierror.New(apierror.KindNotFound, "no project directory found").WithRetryable(apierror.False)

	ErrManifestExists = apierror.New(apierror.KindAlreadyExists, "manifest already exists").WithRetryable(apierror.False)

	ErrNoRuntimeDir = apierror.New(apierror.KindNotFound, "no .wippy directory found").WithRetryable(apierror.False)

	ErrEmptyManifestName = apierror.New(apierror.KindInvalid, "manifest name cannot be empty").WithRetryable(apierror.False)

	ErrInvalidFilter = apierror.New(apierror.KindInvalid, "invalid filter").WithRetryable(apierror.False)

	ErrProcessManagerNotAvailable = apierror.New(apierror.KindInternal, "process manager not available").WithRetryable(apierror.False)

	ErrTranscoderNotFound = apierror.New(apierror.KindNotFound, "transcoder not found").WithRetryable(apierror.False)

	ErrDependencyResolverNotFound = apierror.New(apierror.KindNotFound, "dependency resolver not found").WithRetryable(apierror.False)
)

func NewInvalidIDFormatError(id string) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("invalid ID format: %s", id)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

func NewReadManifestError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to read manifest").WithCause(cause).WithRetryable(apierror.False)
}

func NewDecodeManifestError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "failed to decode manifest").WithCause(cause).WithRetryable(apierror.False)
}

func NewEncodeManifestError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to encode manifest").WithCause(cause).WithRetryable(apierror.False)
}

func NewWriteManifestError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to write manifest").WithCause(cause).WithRetryable(apierror.False)
}

func NewCreateProjectDirError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to create project directory").WithCause(cause).WithRetryable(apierror.False)
}

func NewGlobError(pattern string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, fmt.Sprintf("failed to glob pattern: %s", pattern)).
		WithCause(cause).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"pattern": pattern}))
}

func NewCompileRegexpError(pattern string, cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("failed to compile regexp: %s", pattern)).
		WithCause(cause).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"pattern": pattern}))
}

func NewReadFileError(path string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, fmt.Sprintf("failed to read file: %s", path)).
		WithCause(cause).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"path": path}))
}

func NewCreateLoggerError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to create logger").WithCause(cause).WithRetryable(apierror.False)
}

func NewLoadLockFileError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to load lock file").WithCause(cause).WithRetryable(apierror.False)
}

func NewWriteLockFileError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to write lock file").WithCause(cause).WithRetryable(apierror.False)
}

func NewInitAppError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to initialize app").WithCause(cause).WithRetryable(apierror.False)
}

func NewInitFailedError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "initialization failed").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidLockFileError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid lock file").WithCause(cause).WithRetryable(apierror.False)
}

func NewParseModuleNameError(name string, cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "failed to parse module name: "+name).WithCause(cause).WithRetryable(apierror.False)
}

func NewCheckModuleError(module string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to check module: "+module).WithCause(cause).WithRetryable(apierror.False)
}

func NewModuleMissingHashError(module string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "module missing hash: "+module).WithRetryable(apierror.False)
}

func NewDownloadModuleError(module string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to download module: "+module).WithCause(cause).WithRetryable(apierror.False)
}

func NewNoContentDownloadedError(module string) apierror.Error {
	return apierror.New(apierror.KindInternal, "no content downloaded for module: "+module).WithRetryable(apierror.False)
}

func NewStoreModuleError(module string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to store module: "+module).WithCause(cause).WithRetryable(apierror.False)
}

func NewLockFileNotFoundError(cause error) apierror.Error {
	return apierror.New(apierror.KindNotFound, "lock file not found").WithCause(cause).WithRetryable(apierror.False)
}

func NewEnsureModulesInstalledError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to ensure modules installed").WithCause(cause).WithRetryable(apierror.False)
}

func NewLoadEntriesError(path string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to load entries from: "+path).WithCause(cause).WithRetryable(apierror.False)
}

func NewExecutePipelineError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to execute pipeline").WithCause(cause).WithRetryable(apierror.False)
}

func NewParseMetadataError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "failed to parse metadata").WithCause(cause).WithRetryable(apierror.False)
}

func NewCreatePackFileError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to create pack file").WithCause(cause).WithRetryable(apierror.False)
}

func NewPackWithResourcesError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to pack with resources").WithCause(cause).WithRetryable(apierror.False)
}

func NewPackEntriesError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to pack entries").WithCause(cause).WithRetryable(apierror.False)
}

func NewClosePackFileError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to close pack file").WithCause(cause).WithRetryable(apierror.False)
}

func NewStatOutputFileError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to stat output file").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidMetadataFormatError(format string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid metadata format: "+format).WithRetryable(apierror.False)
}

func NewEmptyMetadataKeyError(flag string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "metadata key cannot be empty: "+flag).WithRetryable(apierror.False)
}

func NewReservedMetadataNamespaceError(key string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "reserved metadata namespace: "+key).WithRetryable(apierror.False)
}

func NewInitializeBootstrapContextError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to initialize bootstrap context").WithCause(cause).WithRetryable(apierror.False)
}

func NewCreateLoaderError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to create loader").WithCause(cause).WithRetryable(apierror.False)
}

func NewLoadComponentsError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to load components").WithCause(cause).WithRetryable(apierror.False)
}

func NewStartComponentsError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to start components").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidOverrideError(override string, cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid override: "+override).WithCause(cause).WithRetryable(apierror.False)
}

func NewMissingSeparatorError(separator string, format string) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("missing separator '%s', expected format: %s", separator, format)).WithRetryable(apierror.False)
}

func NewEmptyFieldError(field string) apierror.Error {
	return apierror.New(apierror.KindInvalid, field+" cannot be empty").WithRetryable(apierror.False)
}

func NewInvalidExecSpecError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid exec spec").WithCause(cause).WithRetryable(apierror.False)
}

func NewStartProcessError(hostID string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to start process on host: "+hostID).WithCause(cause).WithRetryable(apierror.False)
}

func NewOpenPackFileError(path string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to open pack file: "+path).WithCause(cause).WithRetryable(apierror.False)
}

func NewCreatePackReaderError(path string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to create pack reader: "+path).WithCause(cause).WithRetryable(apierror.False)
}

func NewReadEntriesError(path string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to read entries from: "+path).WithCause(cause).WithRetryable(apierror.False)
}

func NewRegisterPackError(path string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to register pack: "+path).WithCause(cause).WithRetryable(apierror.False)
}

func NewStartRuntimeServicesError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to start runtime services").WithCause(cause).WithRetryable(apierror.False)
}

func NewBuildChangeSetError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to build change set").WithCause(cause).WithRetryable(apierror.False)
}

func NewApplyEntriesError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to apply entries").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidExistingLockFileError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid existing lock file").WithCause(cause).WithRetryable(apierror.False)
}

func NewLoadEntriesFromSourceError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to load entries from source").WithCause(cause).WithRetryable(apierror.False)
}

func NewCreateManifestBridgeError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to create manifest bridge").WithCause(cause).WithRetryable(apierror.False)
}

func NewBuildDependencyGraphError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to build dependency graph").WithCause(cause).WithRetryable(apierror.False)
}

func NewDependencyConflictsError(count int) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("dependency conflicts detected (%d)", count)).WithRetryable(apierror.False)
}

func NewInstallFailedError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "install failed").WithCause(cause).WithRetryable(apierror.False)
}

func NewUpdateConflictsError(count int) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("update conflicts detected (%d)", count)).WithRetryable(apierror.False)
}
