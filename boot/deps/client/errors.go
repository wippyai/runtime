// SPDX-License-Identifier: MPL-2.0

package client

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrNoContentDownloaded = apierror.New(apierror.Internal, "no content downloaded").WithRetryable(apierror.False)
	ErrNoMatchingVersion   = apierror.New(apierror.NotFound, "no matching version found").WithRetryable(apierror.False)
)

func NewListOrganizationsError(cause error) apierror.Error {
	return apierror.New(apierror.Unavailable, "list organizations").WithCause(cause)
}

func NewOrganizationNotFoundError(orgName string) apierror.Error {
	return apierror.New(apierror.NotFound, "organization not found").
		WithDetails(attrs.NewBagFrom(map[string]any{"organization": orgName}))
}

func NewListModulesError(cause error) apierror.Error {
	return apierror.New(apierror.Unavailable, "list modules").WithCause(cause)
}

func NewModuleNotFoundError(moduleName string) apierror.Error {
	return apierror.New(apierror.NotFound, "module not found").
		WithDetails(attrs.NewBagFrom(map[string]any{"module": moduleName}))
}

func NewListModuleLabelsError(cause error) apierror.Error {
	return apierror.New(apierror.Unavailable, "list module labels").WithCause(cause)
}

func NewNoLabelsFoundError(moduleID string) apierror.Error {
	return apierror.New(apierror.NotFound, "no labels found for module").
		WithDetails(attrs.NewBagFrom(map[string]any{"module_id": moduleID}))
}

func NewDownloadCommitsError(cause error) apierror.Error {
	return apierror.New(apierror.Unavailable, "download commits").WithCause(cause)
}

func NewGetOrganizationsError(cause error) apierror.Error {
	return apierror.New(apierror.Unavailable, "get organizations").WithCause(cause)
}

func NewGetModulesError(cause error) apierror.Error {
	return apierror.New(apierror.Unavailable, "get modules").WithCause(cause)
}

func NewGetLabelsError(cause error) apierror.Error {
	return apierror.New(apierror.Unavailable, "get labels").WithCause(cause)
}

func NewParseConstraintError(constraint string, cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "parse constraint").
		WithDetails(attrs.NewBagFrom(map[string]any{"constraint": constraint})).
		WithCause(cause)
}

func NewFetchManifestError(cause error) apierror.Error {
	return apierror.New(apierror.Unavailable, "fetch manifest").WithCause(cause)
}

func NewDownloadCommitError(commitID string, cause error) apierror.Error {
	return apierror.New(apierror.Unavailable, "download commit").
		WithDetails(attrs.NewBagFrom(map[string]any{"commit_id": commitID})).
		WithCause(cause)
}

func NewCreateInMemoryFSError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "create in-memory fs").WithCause(cause)
}

func NewLoadEntriesFromFSError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "load entries from fs").WithCause(cause)
}

func NewExtractDependenciesError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "extract dependencies").WithCause(cause)
}

func NewUnmarshalDependencyError(entryID string, cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "unmarshal dependency entry").
		WithDetails(attrs.NewBagFrom(map[string]any{"entry_id": entryID})).
		WithCause(cause)
}

func NewAbsolutePathNotAllowedError(path string) apierror.Error {
	return apierror.New(apierror.Invalid, "absolute path not allowed").
		WithDetails(attrs.NewBagFrom(map[string]any{"path": path}))
}

func NewInvalidPathError(path string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid path").
		WithDetails(attrs.NewBagFrom(map[string]any{"path": path}))
}
