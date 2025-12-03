package cmd

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

func NewCreateLoggerError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to create logger",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewInitAppError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "init app",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewLoadLockFileError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "load lock file",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewInvalidLockFileError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid lock file",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewWriteLockFileError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "write lock file",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewParseModuleNameError(moduleName string, err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "parse module name",
		retryable: apierror.False,
		details: attrs.Bag{
			"module": moduleName,
		},
		cause: err,
	}
}

func NewDownloadModuleError(moduleName string, err error) *Error {
	return &Error{
		kind:      apierror.KindUnavailable,
		message:   "download module",
		retryable: apierror.True,
		details: attrs.Bag{
			"module": moduleName,
		},
		cause: err,
	}
}

func NewStoreModuleError(moduleName string, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "store module",
		retryable: apierror.False,
		details: attrs.Bag{
			"module": moduleName,
		},
		cause: err,
	}
}

func NewParseMetadataError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "parse metadata",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewLoadEntriesError(path string, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "load entries from path",
		retryable: apierror.False,
		details: attrs.Bag{
			"path": path,
		},
		cause: err,
	}
}

func NewExecutePipelineError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "execute pipeline",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewLoadComponentsError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "load components",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewStartComponentsError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "start components",
		retryable: apierror.False,
		cause:     err,
	}
}

var (
	ErrInvalidMetadataFormat = &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid metadata format, expected key=value",
		retryable: apierror.False,
	}

	ErrEmptyMetadataKey = &Error{
		kind:      apierror.KindInvalid,
		message:   "empty metadata key",
		retryable: apierror.False,
	}

	ErrNoContentDownloaded = &Error{
		kind:      apierror.KindInternal,
		message:   "no content downloaded for module",
		retryable: apierror.True,
	}

	ErrModuleMissingHash = &Error{
		kind:      apierror.KindInvalid,
		message:   "module has no hash in lock file",
		retryable: apierror.False,
	}

	ErrTranscoderNotFound = &Error{
		kind:      apierror.KindInternal,
		message:   "transcoder not found",
		retryable: apierror.False,
	}

	ErrRegistryNotFound = &Error{
		kind:      apierror.KindInternal,
		message:   "registry not found",
		retryable: apierror.False,
	}

	ErrDependencyResolverNotFound = &Error{
		kind:      apierror.KindInternal,
		message:   "dependency resolver not found",
		retryable: apierror.False,
	}

	ErrProcessManagerNotAvailable = &Error{
		kind:      apierror.KindUnavailable,
		message:   "process manager not available",
		retryable: apierror.False,
	}
)

func NewCreateLoaderError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "create loader",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewInitializeBootstrapContextError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "initialize bootstrap context",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewLoadEntriesFromSourceError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "load entries from source",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewCreateManifestBridgeError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "create manifest bridge",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewBuildDependencyGraphError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "build dependency graph",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewDependencyConflictsError(count int) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "dependency conflicts detected",
		retryable: apierror.False,
		details: attrs.NewBagFrom(map[string]any{
			"conflict_count": count,
		}),
	}
}

func NewUpdateConflictsError(count int) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "update not possible: conflicts detected",
		retryable: apierror.False,
		details: attrs.NewBagFrom(map[string]any{
			"conflict_count": count,
		}),
	}
}

func NewInitFailedError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "init failed",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewInstallFailedError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "install failed",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewCheckModuleError(moduleName string, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "check module",
		retryable: apierror.False,
		details: attrs.NewBagFrom(map[string]any{
			"module": moduleName,
		}),
		cause: err,
	}
}

func NewLockFileNotFoundError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "lock file not found",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewEnsureModulesInstalledError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "ensure modules installed",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewCreatePackFileError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "create pack file",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewPackWithResourcesError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "pack with resources",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewPackEntriesError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "pack entries",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewClosePackFileError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "close pack file",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewStatOutputFileError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "stat output file",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewInvalidMetadataFormatError(flag string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid metadata format, expected key=value",
		retryable: apierror.False,
		details: attrs.NewBagFrom(map[string]any{
			"flag": flag,
		}),
	}
}

func NewEmptyMetadataKeyError(flag string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "empty metadata key",
		retryable: apierror.False,
		details: attrs.NewBagFrom(map[string]any{
			"flag": flag,
		}),
	}
}

func NewReservedMetadataNamespaceError(key string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "reserved metadata namespace",
		retryable: apierror.False,
		details: attrs.NewBagFrom(map[string]any{
			"key": key,
		}),
	}
}

func NewOpenPackFileError(packFile string, err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "open pack file",
		retryable: apierror.False,
		details: attrs.NewBagFrom(map[string]any{
			"pack_file": packFile,
		}),
		cause: err,
	}
}

func NewCreatePackReaderError(packFile string, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "create pack reader",
		retryable: apierror.False,
		details: attrs.NewBagFrom(map[string]any{
			"pack_file": packFile,
		}),
		cause: err,
	}
}

func NewReadEntriesError(packFile string, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "read entries from pack",
		retryable: apierror.False,
		details: attrs.NewBagFrom(map[string]any{
			"pack_file": packFile,
		}),
		cause: err,
	}
}

func NewRegisterPackError(packFile string, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "register pack",
		retryable: apierror.False,
		details: attrs.NewBagFrom(map[string]any{
			"pack_file": packFile,
		}),
		cause: err,
	}
}

func NewStartRuntimeServicesError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "start runtime services",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewBuildChangeSetError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "build change set",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewApplyEntriesError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "apply entries",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewInvalidOverrideError(override string, err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid override",
		retryable: apierror.False,
		details: attrs.NewBagFrom(map[string]any{
			"override": override,
		}),
		cause: err,
	}
}

func NewInvalidExecSpecError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid exec spec",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewStartProcessError(hostID string, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to start process",
		retryable: apierror.False,
		details: attrs.NewBagFrom(map[string]any{
			"host_id": hostID,
		}),
		cause: err,
	}
}

func NewMissingSeparatorError(separator, expected string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "missing separator",
		retryable: apierror.False,
		details: attrs.NewBagFrom(map[string]any{
			"separator": separator,
			"expected":  expected,
		}),
	}
}

func NewEmptyFieldError(field string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "empty field",
		retryable: apierror.False,
		details: attrs.NewBagFrom(map[string]any{
			"field": field,
		}),
	}
}

func NewInvalidExistingLockFileError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid existing lock file",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewNoContentDownloadedError(moduleName string) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "no content downloaded for module",
		retryable: apierror.True,
		details: attrs.NewBagFrom(map[string]any{
			"module": moduleName,
		}),
	}
}

func NewModuleMissingHashError(moduleName string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "module has no hash in lock file",
		retryable: apierror.False,
		details: attrs.NewBagFrom(map[string]any{
			"module": moduleName,
		}),
	}
}
