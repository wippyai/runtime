// Package pack provides interfaces for working with Wippy pack archives.
package pack

import (
	"io/fs"

	"github.com/wippyai/runtime/api/registry"
)

// Resource type constants reserved for future use
// const (
// 	ResourceTree = "tree" // Filesystem tree
// 	ResourceBlob = "blob" // Large binary blob
// )

// Pack provides read-only access to pack file resources.
type Pack interface {
	// GetFS returns a filesystem for the given resource ID.
	GetFS(id registry.ID) (fs.ReadDirFS, error)

	// GetBlob returns blob reader for the given resource ID.
	GetBlob(id registry.ID) (BlobReader, error)

	// ListResources returns metadata for all resources in pack.
	ListResources() ([]ResourceInfo, error)

	// GetEntries returns registry entries from pack.
	GetEntries() ([]registry.Entry, error)

	// GetMetadata returns pack metadata.
	GetMetadata() (registry.Metadata, error)
}

// ResourceInfo contains lightweight resource metadata.
type ResourceInfo struct {
	ID        registry.ID       `json:"id"`
	Type      string            `json:"type"` // tree or blob
	Meta      registry.Metadata `json:"meta"`
	Hash      string            `json:"hash"`       // SHA-256 content hash
	Size      uint64            `json:"size"`       // Total uncompressed size
	FileCount uint32            `json:"file_count"` // For trees only
}

// BlobReader provides access to blob data.
type BlobReader interface {
	// ReadAt reads len(p) bytes from offset into p.
	ReadAt(p []byte, offset int64) (n int, err error)

	// Size returns total blob size in bytes.
	Size() int64

	// Hash returns SHA-256 content hash.
	Hash() string

	// Close releases resources.
	Close() error
}
