// Package storage provides interfaces and types for interacting with cloud storage services.
package storage

import (
	"context"
	"io"

	"github.com/ponyruntime/pony/api/event"
)

const (
	// System represents the cloud storage system identifier.
	System event.System = "cloudstorage"

	// Register is the event type for registering a new storage provider.
	Register event.Kind = "cloudstorage.register"
	// Delete is the event type for removing a storage provider.
	Delete event.Kind = "cloudstorage.delete"

	// Accept is the event type for accepting a storage operation.
	Accept event.Kind = "cloudstorage.accept"
	// Reject is the event type for rejecting a storage operation.
	Reject event.Kind = "cloudstorage.reject"
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

	// Storage defines the interface for cloud storage providers.
	Storage interface {
		// ListObjects lists objects in a bucket with the given options.
		ListObjects(ctx context.Context, bucket string, opts *ListObjectsOptions) (*ListObjectsResult, error)

		// DownloadObject retrieves an object from a bucket and writes it to w.
		DownloadObject(ctx context.Context, bucket, key string, w io.Writer) error

		// UploadObject uploads an object to a bucket.
		UploadObject(ctx context.Context, bucket, key string, content io.Reader) error

		// DeleteObjects removes multiple objects from a bucket.
		DeleteObjects(ctx context.Context, bucket string, keys []string) error
	}
)
