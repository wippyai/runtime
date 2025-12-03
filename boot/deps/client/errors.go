package client

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

var (
	ErrNoContentDownloaded = &Error{
		kind:    apierror.KindInternal,
		message: "no content downloaded",
	}
	ErrNoMatchingVersion = &Error{
		kind:    apierror.KindNotFound,
		message: "no matching version found",
	}
)

func NewListOrganizationsError(cause error) *Error {
	return &Error{
		kind:    apierror.KindUnavailable,
		message: "list organizations",
		cause:   cause,
	}
}

func NewOrganizationNotFoundError(orgName string) *Error {
	return &Error{
		kind:    apierror.KindNotFound,
		message: "organization not found",
		details: attrs.NewBagFrom(map[string]any{"organization": orgName}),
	}
}

func NewListModulesError(cause error) *Error {
	return &Error{
		kind:    apierror.KindUnavailable,
		message: "list modules",
		cause:   cause,
	}
}

func NewModuleNotFoundError(moduleName string) *Error {
	return &Error{
		kind:    apierror.KindNotFound,
		message: "module not found",
		details: attrs.NewBagFrom(map[string]any{"module": moduleName}),
	}
}

func NewListModuleLabelsError(cause error) *Error {
	return &Error{
		kind:    apierror.KindUnavailable,
		message: "list module labels",
		cause:   cause,
	}
}

func NewNoLabelsFoundError(moduleID string) *Error {
	return &Error{
		kind:    apierror.KindNotFound,
		message: "no labels found for module",
		details: attrs.NewBagFrom(map[string]any{"module_id": moduleID}),
	}
}

func NewDownloadCommitsError(cause error) *Error {
	return &Error{
		kind:    apierror.KindUnavailable,
		message: "download commits",
		cause:   cause,
	}
}

func NewGetOrganizationsError(cause error) *Error {
	return &Error{
		kind:    apierror.KindUnavailable,
		message: "get organizations",
		cause:   cause,
	}
}

func NewGetModulesError(cause error) *Error {
	return &Error{
		kind:    apierror.KindUnavailable,
		message: "get modules",
		cause:   cause,
	}
}

func NewGetLabelsError(cause error) *Error {
	return &Error{
		kind:    apierror.KindUnavailable,
		message: "get labels",
		cause:   cause,
	}
}

func NewParseConstraintError(constraint string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "parse constraint",
		details: attrs.NewBagFrom(map[string]any{"constraint": constraint}),
		cause:   cause,
	}
}

func NewFetchManifestError(cause error) *Error {
	return &Error{
		kind:    apierror.KindUnavailable,
		message: "fetch manifest",
		cause:   cause,
	}
}

func NewDownloadCommitError(commitID string, cause error) *Error {
	return &Error{
		kind:    apierror.KindUnavailable,
		message: "download commit",
		details: attrs.NewBagFrom(map[string]any{"commit_id": commitID}),
		cause:   cause,
	}
}

func NewCreateInMemoryFSError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "create in-memory fs",
		cause:   cause,
	}
}

func NewLoadEntriesFromFSError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "load entries from fs",
		cause:   cause,
	}
}

func NewExtractDependenciesError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "extract dependencies",
		cause:   cause,
	}
}

func NewUnmarshalDependencyError(entryID string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "unmarshal dependency entry",
		details: attrs.NewBagFrom(map[string]any{"entry_id": entryID}),
		cause:   cause,
	}
}

func NewAbsolutePathNotAllowedError(path string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "absolute path not allowed",
		details: attrs.NewBagFrom(map[string]any{"path": path}),
	}
}

func NewInvalidPathError(path string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "invalid path",
		details: attrs.NewBagFrom(map[string]any{"path": path}),
	}
}
