// SPDX-License-Identifier: MPL-2.0

// Package cloudstorage provides interfaces and types for interacting with cloud storage services.
package cloudstorage

import (
	"context"
	"errors"
	"io"
	"time"
)

// ErrPreconditionFailed is returned when an If-Match or If-None-Match precondition
// is not satisfied (HTTP 412 from the underlying provider).
var ErrPreconditionFailed = errors.New("cloudstorage: precondition failed")

// ErrNotFound is returned when the requested object does not exist
// (HTTP 404 / SDK NoSuchKey / NotFound from the underlying provider).
var ErrNotFound = errors.New("cloudstorage: not found")

type (
	// Owner identifies the owner of an object (when surfaced by the provider).
	Owner struct {
		ID          string
		DisplayName string
	}

	// ObjectMetadata represents metadata for an object stored in cloud storage.
	ObjectMetadata struct {
		LastModified time.Time
		Owner        *Owner
		UserMetadata map[string]string
		Key          string
		ContentType  string
		ETag         string
		StorageClass string
		VersionID    string
		Size         int64
	}

	// HeadObjectResult contains the full metadata for a single object,
	// including provider-specific user metadata.
	HeadObjectResult struct {
		LastModified time.Time
		UserMetadata map[string]string
		// Headers carries the raw response headers from the provider, with
		// lowercased keys. Multi-valued headers are joined with ", " per
		// RFC 7230 §3.2.2. Useful for accessing provider-specific fields
		// not modeled as typed fields above (e.g. x-amz-tagging-count,
		// x-amz-replication-status, x-amz-server-side-encryption).
		Headers            map[string]string
		ContentType        string
		ETag               string
		CacheControl       string
		ContentDisposition string
		ContentEncoding    string
		StorageClass       string
		VersionID          string
		Size               int64
	}

	// ListObjectsOptions defines options for listing objects.
	ListObjectsOptions struct {
		Prefix            string
		ContinuationToken string
		MaxKeys           int
		// IncludeOwner asks the provider to populate Owner on each result.
		// On S3 this maps to FetchOwner=true.
		IncludeOwner bool
		// IncludeVersions switches to a versioned listing (S3: ListObjectVersions)
		// so that VersionID is filled per item.
		IncludeVersions bool
	}

	// ListObjectsResult contains the results of a list operation.
	ListObjectsResult struct {
		NextContinuationToken string
		Objects               []ObjectMetadata
		IsTruncated           bool
	}

	// PresignedGetOptions contains options for generating presigned download URLs.
	PresignedGetOptions struct {
		// Expiration is the duration after which the URL expires.
		Expiration time.Duration
	}

	// PresignedPutOptions contains options for generating presigned upload URLs.
	PresignedPutOptions struct {
		ContentType   string
		Expiration    time.Duration
		ContentLength int64
	}

	// DownloadOptions contains options for downloading objects.
	DownloadOptions struct {
		// Range specifies a byte range to retrieve (e.g., "bytes=0-1023" for first 1KB).
		Range string
		// IfMatch returns the object only if its current ETag matches.
		IfMatch string
		// IfNoneMatch returns the object only if its current ETag does NOT match.
		IfNoneMatch string
	}

	// UploadOptions contains options for uploading objects.
	UploadOptions struct {
		Metadata map[string]string
		// Headers passes through arbitrary HTTP request headers to the
		// provider. Useful for provider-specific options that are not
		// modeled as typed fields above (e.g. x-amz-tagging,
		// x-amz-server-side-encryption, x-amz-website-redirect-location).
		// Headers are sent verbatim and participate in request signing.
		Headers            map[string]string
		ContentType        string
		CacheControl       string
		ContentDisposition string
		ContentEncoding    string
		// IfMatch uploads only if the existing object's ETag matches.
		IfMatch string
		// IfNoneMatch uploads only if no object exists ("*") or its ETag does not match.
		IfNoneMatch string
	}

	// Storage defines the interface for cloud storage providers.
	Storage interface {
		// ListObjects lists objects with the given options.
		ListObjects(ctx context.Context, opts *ListObjectsOptions) (*ListObjectsResult, error)

		// HeadObject returns full metadata for a single object, including user metadata.
		HeadObject(ctx context.Context, key string) (*HeadObjectResult, error)

		// DownloadObject retrieves an object and writes it to w.
		DownloadObject(ctx context.Context, key string, w io.Writer, opts *DownloadOptions) error

		// UploadObject uploads an object.
		UploadObject(ctx context.Context, key string, content io.Reader, opts *UploadOptions) error

		// DeleteObjects removes multiple objects.
		DeleteObjects(ctx context.Context, keys []string) error

		// PresignedGetURL generates a presigned URL for downloading an object.
		PresignedGetURL(ctx context.Context, key string, opts *PresignedGetOptions) (string, error)

		// PresignedPutURL generates a presigned URL for uploading an object.
		PresignedPutURL(ctx context.Context, key string, opts *PresignedPutOptions) (string, error)
	}
)
