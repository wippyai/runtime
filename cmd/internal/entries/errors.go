package entries

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

func NewEnsureModulesInstalledError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "ensure modules installed",
		retryable: apierror.True,
		cause:     err,
	}
}

func NewLoadEntriesFromPathsError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "load entries from paths",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewLoadEntriesToRegistryError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "load entries to registry",
		retryable: apierror.False,
		cause:     err,
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

func NewLoadFromPathError(path string, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "load from path",
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

func NewLoadStateError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "load state",
		retryable: apierror.False,
		cause:     err,
	}
}

var (
	ErrRegistryClientNotFound = &Error{
		kind:      apierror.KindInternal,
		message:   "registry client not found in context",
		retryable: apierror.False,
	}

	ErrTranscoderNotFound = &Error{
		kind:      apierror.KindInternal,
		message:   "transcoder not found in context",
		retryable: apierror.False,
	}

	ErrLoaderNotFound = &Error{
		kind:      apierror.KindInternal,
		message:   "loader not found in context",
		retryable: apierror.False,
	}

	ErrRegistryNotFound = &Error{
		kind:      apierror.KindInternal,
		message:   "registry not found in context",
		retryable: apierror.False,
	}

	ErrResolverNotFound = &Error{
		kind:      apierror.KindInternal,
		message:   "dependency resolver not found in context",
		retryable: apierror.False,
	}

	ErrModuleMissingHash = &Error{
		kind:      apierror.KindInvalid,
		message:   "module has no hash in lock file",
		retryable: apierror.False,
	}

	ErrNoContentDownloaded = &Error{
		kind:      apierror.KindInternal,
		message:   "no content downloaded for module",
		retryable: apierror.True,
	}
)

func NewGetCurrentVersionError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to get current version",
		retryable: apierror.False,
		cause:     err,
	}
}
