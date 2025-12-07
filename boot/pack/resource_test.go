package pack

import (
	"testing"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
)

// TestTreeResourceStructure tests tree resource basic structure.
func TestTreeResourceStructure(t *testing.T) {
	tree := &TreeResource{
		ID:   registry.NewID("test", "tree1"),
		Meta: attrs.NewBag(),
		Files: map[string]FileEntry{
			"file1.txt": {
				Size:    100,
				Mode:    0644,
				ModTime: 1234567890,
				Hash:    "abc123",
				Location: FileLocation{
					FrameIndex: 2,
					Offset:     0,
				},
			},
			"dir/file2.txt": {
				Size:    200,
				Mode:    0644,
				ModTime: 1234567891,
				Hash:    "def456",
				Location: FileLocation{
					FrameIndex: 2,
					Offset:     100,
				},
			},
		},
		Dirs: map[string][]string{
			"":    {"file1.txt", "dir"},
			"dir": {"file2.txt"},
		},
	}

	if len(tree.Files) != 2 {
		t.Errorf("File count mismatch: got %d, want 2", len(tree.Files))
	}

	if len(tree.Dirs) != 2 {
		t.Errorf("Dir count mismatch: got %d, want 2", len(tree.Dirs))
	}

	if len(tree.Dirs[""]) != 2 {
		t.Errorf("Root dir children mismatch: got %d, want 2", len(tree.Dirs[""]))
	}

	if tree.Files["file1.txt"].Size != 100 {
		t.Errorf("File size mismatch: got %d, want 100", tree.Files["file1.txt"].Size)
	}
}

// TestFileEntrySmallFile tests file entry without chunks.
func TestFileEntrySmallFile(t *testing.T) {
	entry := FileEntry{
		Size:    500,
		Mode:    0644,
		ModTime: 1234567890,
		Hash:    "abc123",
		Location: FileLocation{
			FrameIndex: 2,
			Offset:     0,
			Chunks:     nil,
		},
	}

	if entry.Size != 500 {
		t.Errorf("Size mismatch: got %d, want 500", entry.Size)
	}

	if len(entry.Location.Chunks) != 0 {
		t.Errorf("Expected no chunks for small file, got %d", len(entry.Location.Chunks))
	}

	if entry.Location.FrameIndex != 2 {
		t.Errorf("FrameIndex mismatch: got %d, want 2", entry.Location.FrameIndex)
	}
}

// TestFileEntryLargeFile tests file entry with chunks.
func TestFileEntryLargeFile(t *testing.T) {
	fileSize := 3*ChunkSize + 500000 // 3.5MB file

	chunks := []ChunkInfo{
		{
			Offset:      0,
			Size:        uint32(ChunkSize),
			FrameIndex:  2,
			FrameOffset: 0,
		},
		{
			Offset:      ChunkSize,
			Size:        uint32(ChunkSize),
			FrameIndex:  2,
			FrameOffset: ChunkSize,
		},
		{
			Offset:      2 * ChunkSize,
			Size:        uint32(ChunkSize),
			FrameIndex:  2,
			FrameOffset: 2 * ChunkSize,
		},
		{
			Offset:      3 * ChunkSize,
			Size:        500000,
			FrameIndex:  2,
			FrameOffset: 3 * ChunkSize,
		},
	}

	entry := FileEntry{
		Size:    fileSize,
		Mode:    0644,
		ModTime: 1234567890,
		Hash:    "large-file-hash",
		Location: FileLocation{
			FrameIndex: 2,
			Offset:     0,
			Chunks:     chunks,
		},
	}

	if entry.Size != fileSize {
		t.Errorf("Size mismatch: got %d, want %d", entry.Size, fileSize)
	}

	if len(entry.Location.Chunks) != 4 {
		t.Errorf("Chunk count mismatch: got %d, want 4", len(entry.Location.Chunks))
	}

	// Verify chunk continuity
	var totalSize uint64
	for i, chunk := range entry.Location.Chunks {
		if chunk.Offset != totalSize {
			t.Errorf("Chunk %d offset mismatch: got %d, want %d", i, chunk.Offset, totalSize)
		}
		totalSize += uint64(chunk.Size)
	}

	if totalSize != fileSize {
		t.Errorf("Total chunk size mismatch: got %d, want %d", totalSize, fileSize)
	}
}

// TestChunkSizeConstant tests chunk size constant.
func TestChunkSizeConstant(t *testing.T) {
	expectedChunkSize := uint64(1024 * 1024) // 1MB

	if ChunkSize != expectedChunkSize {
		t.Errorf("ChunkSize mismatch: got %d, want %d", ChunkSize, expectedChunkSize)
	}
}

// TestBlobResourceSmall tests small blob without chunks.
func TestBlobResourceSmall(t *testing.T) {
	blob := &BlobResource{
		ID:     registry.NewID("test", "blob1"),
		Meta:   attrs.NewBag(),
		Size:   5000,
		Hash:   "small-blob-hash",
		Chunks: nil,
	}

	if blob.Size != 5000 {
		t.Errorf("Size mismatch: got %d, want 5000", blob.Size)
	}

	if len(blob.Chunks) != 0 {
		t.Errorf("Expected no chunks for small blob, got %d", len(blob.Chunks))
	}
}

// TestBlobResourceLarge tests large blob with chunks.
func TestBlobResourceLarge(t *testing.T) {
	blobSize := 5*ChunkSize + 500000 // 5.5MB blob

	chunks := []BlobChunk{
		{
			Offset:      0,
			Size:        uint32(ChunkSize),
			FrameIndex:  2,
			FrameOffset: 0,
		},
		{
			Offset:      ChunkSize,
			Size:        uint32(ChunkSize),
			FrameIndex:  2,
			FrameOffset: ChunkSize,
		},
		{
			Offset:      2 * ChunkSize,
			Size:        uint32(ChunkSize),
			FrameIndex:  2,
			FrameOffset: 2 * ChunkSize,
		},
		{
			Offset:      3 * ChunkSize,
			Size:        uint32(ChunkSize),
			FrameIndex:  2,
			FrameOffset: 3 * ChunkSize,
		},
		{
			Offset:      4 * ChunkSize,
			Size:        uint32(ChunkSize),
			FrameIndex:  2,
			FrameOffset: 4 * ChunkSize,
		},
		{
			Offset:      5 * ChunkSize,
			Size:        500000,
			FrameIndex:  2,
			FrameOffset: 5 * ChunkSize,
		},
	}

	blob := &BlobResource{
		ID:     registry.NewID("test", "blob2"),
		Meta:   attrs.NewBag(),
		Size:   blobSize,
		Hash:   "large-blob-hash",
		Chunks: chunks,
	}

	if blob.Size != blobSize {
		t.Errorf("Size mismatch: got %d, want %d", blob.Size, blobSize)
	}

	if len(blob.Chunks) != 6 {
		t.Errorf("Chunk count mismatch: got %d, want 6", len(blob.Chunks))
	}

	// Verify chunk continuity
	var totalSize uint64
	for i, chunk := range blob.Chunks {
		if chunk.Offset != totalSize {
			t.Errorf("Chunk %d offset mismatch: got %d, want %d", i, chunk.Offset, totalSize)
		}
		totalSize += uint64(chunk.Size)
	}

	if totalSize != blobSize {
		t.Errorf("Total chunk size mismatch: got %d, want %d", totalSize, blobSize)
	}
}

// TestTreeResourceEmptyDir tests tree with empty directory.
func TestTreeResourceEmptyDir(t *testing.T) {
	tree := &TreeResource{
		ID:   registry.NewID("test", "tree2"),
		Meta: attrs.NewBag(),
		Files: map[string]FileEntry{
			"file.txt": {
				Size:    100,
				Mode:    0644,
				ModTime: 1234567890,
				Hash:    "abc123",
				Location: FileLocation{
					FrameIndex: 2,
					Offset:     0,
				},
			},
		},
		Dirs: map[string][]string{
			"":         {"file.txt", "emptydir"},
			"emptydir": {},
		},
	}

	if len(tree.Dirs["emptydir"]) != 0 {
		t.Errorf("Empty dir should have no children, got %d", len(tree.Dirs["emptydir"]))
	}

	if len(tree.Dirs) != 2 {
		t.Errorf("Dir count mismatch: got %d, want 2", len(tree.Dirs))
	}
}

// TestTreeResourceNestedDirs tests tree with nested directories.
func TestTreeResourceNestedDirs(t *testing.T) {
	tree := &TreeResource{
		ID:   registry.NewID("test", "tree3"),
		Meta: attrs.NewBag(),
		Files: map[string]FileEntry{
			"a/b/c/file.txt": {
				Size:    100,
				Mode:    0644,
				ModTime: 1234567890,
				Hash:    "abc123",
				Location: FileLocation{
					FrameIndex: 2,
					Offset:     0,
				},
			},
		},
		Dirs: map[string][]string{
			"":      {"a"},
			"a":     {"b"},
			"a/b":   {"c"},
			"a/b/c": {"file.txt"},
		},
	}

	if len(tree.Dirs) != 4 {
		t.Errorf("Dir count mismatch: got %d, want 4", len(tree.Dirs))
	}

	if len(tree.Dirs["a/b/c"]) != 1 {
		t.Errorf("Deepest dir child count mismatch: got %d, want 1", len(tree.Dirs["a/b/c"]))
	}

	if tree.Dirs["a/b/c"][0] != "file.txt" {
		t.Errorf("Deepest dir child mismatch: got %s, want file.txt", tree.Dirs["a/b/c"][0])
	}
}

// TestFileLocationFrameIndex tests frame index for different resources.
func TestFileLocationFrameIndex(t *testing.T) {
	tests := []struct {
		name       string
		frameIndex uint32
		wantValid  bool
	}{
		{"metadata frame", 0, false},
		{"entries frame", 1, false},
		{"first resource", 2, true},
		{"second resource", 3, true},
		{"tenth resource", 11, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loc := FileLocation{
				FrameIndex: tt.frameIndex,
				Offset:     0,
			}

			isResourceFrame := loc.FrameIndex >= 2
			if isResourceFrame != tt.wantValid {
				t.Errorf("Frame %d validity mismatch: got %v, want %v",
					tt.frameIndex, isResourceFrame, tt.wantValid)
			}
		})
	}
}

// TestChunkInfoAlignment tests chunk alignment properties.
func TestChunkInfoAlignment(t *testing.T) {
	_ = 2*ChunkSize + 512*1024 // 2.5MB

	chunks := []ChunkInfo{
		{
			Offset:      0,
			Size:        uint32(ChunkSize),
			FrameIndex:  2,
			FrameOffset: 0,
		},
		{
			Offset:      ChunkSize,
			Size:        uint32(ChunkSize),
			FrameIndex:  2,
			FrameOffset: ChunkSize,
		},
		{
			Offset:      2 * ChunkSize,
			Size:        512 * 1024,
			FrameIndex:  2,
			FrameOffset: 2 * ChunkSize,
		},
	}

	// Verify first chunks are aligned to ChunkSize
	for i := 0; i < len(chunks)-1; i++ {
		if chunks[i].Size != uint32(ChunkSize) {
			t.Errorf("Chunk %d should be full size: got %d, want %d",
				i, chunks[i].Size, ChunkSize)
		}
	}

	// Last chunk can be partial
	lastChunk := chunks[len(chunks)-1]
	if lastChunk.Size >= uint32(ChunkSize) {
		t.Errorf("Last chunk should be partial: got %d", lastChunk.Size)
	}

	// Verify offsets are sequential
	for i := 1; i < len(chunks); i++ {
		expectedOffset := chunks[i-1].Offset + uint64(chunks[i-1].Size)
		if chunks[i].Offset != expectedOffset {
			t.Errorf("Chunk %d offset mismatch: got %d, want %d",
				i, chunks[i].Offset, expectedOffset)
		}
	}
}
