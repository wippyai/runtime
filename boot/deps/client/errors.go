package client

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrNoContentDownloaded = apierror.New(apierror.KindInternal, "no content downloaded").WithRetryable(apierror.False)
	ErrNoMatchingVersion   = apierror.New(apierror.KindNotFound, "no matching version found").WithRetryable(apierror.False)
)

func NewListOrganizationsError(cause error) apierror.Error {
	return apierror.New(apierror.KindUnavailable, "list organizations").WithCause(cause)
}

func NewOrganizationNotFoundError(orgName string) apierror.Error {
	return apierror.New(apierror.KindNotFound, "organization not found").
		WithDetails(attrs.NewBagFrom(map[string]any{"organization": orgName}))
}

func NewListModulesError(cause error) apierror.Error {
	return apierror.New(apierror.KindUnavailable, "list modules").WithCause(cause)
}

func NewModuleNotFoundError(moduleName string) apierror.Error {
	return apierror.New(apierror.KindNotFound, "module not found").
		WithDetails(attrs.NewBagFrom(map[string]any{"module": moduleName}))
}

func NewListModuleLabelsError(cause error) apierror.Error {
	return apierror.New(apierror.KindUnavailable, "list module labels").WithCause(cause)
}

func NewNoLabelsFoundError(moduleID string) apierror.Error {
	return apierror.New(apierror.KindNotFound, "no labels found for module").
		WithDetails(attrs.NewBagFrom(map[string]any{"module_id": moduleID}))
}

func NewDownloadCommitsError(cause error) apierror.Error {
	return apierror.New(apierror.KindUnavailable, "download commits").WithCause(cause)
}

func NewGetOrganizationsError(cause error) apierror.Error {
	return apierror.New(apierror.KindUnavailable, "get organizations").WithCause(cause)
}

func NewGetModulesError(cause error) apierror.Error {
	return apierror.New(apierror.KindUnavailable, "get modules").WithCause(cause)
}

func NewGetLabelsError(cause error) apierror.Error {
	return apierror.New(apierror.KindUnavailable, "get labels").WithCause(cause)
}

func NewParseConstraintError(constraint string, cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "parse constraint").
		WithDetails(attrs.NewBagFrom(map[string]any{"constraint": constraint})).
		WithCause(cause)
}

func NewFetchManifestError(cause error) apierror.Error {
	return apierror.New(apierror.KindUnavailable, "fetch manifest").WithCause(cause)
}

func NewDownloadCommitError(commitID string, cause error) apierror.Error {
	return apierror.New(apierror.KindUnavailable, "download commit").
		WithDetails(attrs.NewBagFrom(map[string]any{"commit_id": commitID})).
		WithCause(cause)
}

func NewCreateInMemoryFSError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "create in-memory fs").WithCause(cause)
}

func NewLoadEntriesFromFSError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "load entries from fs").WithCause(cause)
}

func NewExtractDependenciesError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "extract dependencies").WithCause(cause)
}

func NewUnmarshalDependencyError(entryID string, cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "unmarshal dependency entry").
		WithDetails(attrs.NewBagFrom(map[string]any{"entry_id": entryID})).
		WithCause(cause)
}

func NewAbsolutePathNotAllowedError(path string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "absolute path not allowed").
		WithDetails(attrs.NewBagFrom(map[string]any{"path": path}))
}

func NewInvalidPathError(path string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid path").
		WithDetails(attrs.NewBagFrom(map[string]any{"path": path}))
}
