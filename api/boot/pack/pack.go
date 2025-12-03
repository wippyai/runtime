// Package pack provides interfaces for working with Wippy pack archives.
package pack

import (
	"io/fs"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
)

type (
	// Pack provides read-only access to pack file resources.
	Pack interface {
		// GetFS returns a filesystem for the given resource ID.
		GetFS(id registry.ID) (fs.ReadDirFS, error)

		// GetBlob returns blob reader for the given resource ID.
		GetBlob(id registry.ID) (BlobReader, error)

		// ListResources returns metadata for all resources in pack.
		ListResources() ([]ResourceInfo, error)

		// GetEntries returns registry entries from pack.
		GetEntries() ([]registry.Entry, error)

		// GetMetadata returns pack metadata.
		GetMetadata() (attrs.Bag, error)
	}

	// ResourceInfo contains lightweight resource metadata.
	ResourceInfo struct {
		ID        registry.ID `json:"id"`
		Type      string      `json:"type"`
		Meta      attrs.Bag   `json:"meta"`
		Hash      string      `json:"hash"`
		Size      uint64      `json:"size"`
		FileCount uint32      `json:"file_count"`
	}

	// BlobReader provides access to blob data.
	BlobReader interface {
		// ReadAt reads len(p) bytes from offset into p.
		ReadAt(p []byte, offset int64) (n int, err error)

		// Size returns total blob size in bytes.
		Size() int64

		// Hash returns SHA-256 content hash.
		Hash() string

		// Close releases resources.
		Close() error
	}
)
