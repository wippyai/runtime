package pack

import (
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
)

const (
	// ChunkSize is the default chunk size for large files (1MB)
	ChunkSize uint64 = 1024 * 1024
)

// TreeResource represents a filesystem tree in the pack.
type TreeResource struct {
	ID   registry.ID `msgpack:"id"`
	Meta attrs.Bag   `msgpack:"meta"`

	// Path index for O(1) file lookups
	Files map[string]FileEntry `msgpack:"files"` // path -> file info

	// Directory listings for ReadDir
	Dirs map[string][]string `msgpack:"dirs"` // dir path -> child names
}

// FileEntry describes a file in a tree resource.
type FileEntry struct {
	Size           uint64    `msgpack:"size"`
	CompressedSize uint64    `msgpack:"compressed_size,omitempty"` // Size of compressed data (if Compressed=true)
	Mode           uint32    `msgpack:"mode"`                      // File mode bits
	ModTime        int64     `msgpack:"mtime"`                     // Unix timestamp
	Hash           string    `msgpack:"hash"`                      // SHA-256 of content
	Compressed     bool      `msgpack:"compressed"`                // Whether file data is compressed
	Meta           attrs.Bag `msgpack:"meta,omitempty"`            // File metadata (content-type, etc.)

	// Location of file content
	Location FileLocation `msgpack:"location"`
}

// FileLocation describes where file content is stored.
type FileLocation struct {
	FrameIndex uint32 `msgpack:"frame"`  // Which frame contains this file
	Offset     uint64 `msgpack:"offset"` // Offset within uncompressed frame

	// For files > 1MB, chunks are used
	Chunks []ChunkInfo `msgpack:"chunks,omitempty"`
}

// ChunkInfo describes a chunk of a large file.
type ChunkInfo struct {
	Size        uint32 `msgpack:"size"`         // Chunk size
	Offset      uint64 `msgpack:"offset"`       // Offset in original file
	FrameIndex  uint32 `msgpack:"frame"`        // Which frame
	FrameOffset uint64 `msgpack:"frame_offset"` // Offset in frame
}

// BlobResource represents a large binary blob in the pack.
type BlobResource struct {
	ID   registry.ID `msgpack:"id"`
	Meta attrs.Bag   `msgpack:"meta"`
	Size uint64      `msgpack:"size"`
	Hash string      `msgpack:"hash"` // SHA-256

	// For blobs > 1MB, chunks are used
	Chunks []BlobChunk `msgpack:"chunks,omitempty"`
}

// BlobChunk describes a chunk of a blob.
type BlobChunk struct {
	Index       uint32 `msgpack:"idx"`
	Size        uint32 `msgpack:"size"`
	Offset      uint64 `msgpack:"offset"`       // Offset in original blob
	FrameIndex  uint32 `msgpack:"frame"`        // Which frame
	FrameOffset uint64 `msgpack:"frame_offset"` // Offset in frame
}
