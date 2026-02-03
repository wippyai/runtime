// Package cloudstorage provides interfaces and types for interacting with cloud storage services.
package cloudstorage

import (
	"context"
	"io"
	"time"
)

type (
	// ObjectMetadata represents metadata for an object stored in cloud storage.
	ObjectMetadata struct {
		Key         string
		ContentType string
		ETag        string
		Size        int64
	}

	// ListObjectsOptions defines options for listing objects.
	ListObjectsOptions struct {
		Prefix            string
		ContinuationToken string
		MaxKeys           int
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
	}

	// Storage defines the interface for cloud storage providers.
	Storage interface {
		// ListObjects lists objects with the given options.
		ListObjects(ctx context.Context, opts *ListObjectsOptions) (*ListObjectsResult, error)

		// DownloadObject retrieves an object and writes it to w.
		DownloadObject(ctx context.Context, key string, w io.Writer, opts *DownloadOptions) error

		// UploadObject uploads an object.
		UploadObject(ctx context.Context, key string, content io.Reader) error

		// DeleteObjects removes multiple objects.
		DeleteObjects(ctx context.Context, keys []string) error

		// PresignedGetURL generates a presigned URL for downloading an object.
		PresignedGetURL(ctx context.Context, key string, opts *PresignedGetOptions) (string, error)

		// PresignedPutURL generates a presigned URL for uploading an object.
		PresignedPutURL(ctx context.Context, key string, opts *PresignedPutOptions) (string, error)
	}
)
