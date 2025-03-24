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
		// Key is the unique identifier for the object within a bucket.
		Key string

		// Size is the size of the object in bytes.
		Size int64

		// ContentType is the MIME type of the object.
		ContentType string

		// ETag is the entity tag of the object.
		ETag string
	}

	// ListObjectsOptions defines options for listing objects.
	ListObjectsOptions struct {
		// Prefix filters objects that start with this prefix.
		Prefix string

		// MaxKeys is the maximum number of keys to return.
		MaxKeys int

		// ContinuationToken is used for pagination.
		ContinuationToken string
	}

	// ListObjectsResult contains the results of a list operation.
	ListObjectsResult struct {
		// Objects is the list of objects.
		Objects []ObjectMetadata

		// IsTruncated indicates if there are more results.
		IsTruncated bool

		// NextContinuationToken is used to get the next page of results.
		NextContinuationToken string
	}

	// PresignedGetOptions contains options for generating presigned download URLs.
	PresignedGetOptions struct {
		// Expiration is the duration after which the URL expires.
		Expiration time.Duration
	}

	// PresignedPutOptions contains options for generating presigned upload URLs.
	PresignedPutOptions struct {
		// Expiration is the duration after which the URL expires.
		Expiration time.Duration

		// ContentType specifies the content type for the object.
		ContentType string

		// ContentLength specifies the maximum allowed content length.
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
