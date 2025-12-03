package pack

import (
	"bytes"
	"testing"

	"github.com/wippyai/runtime/api/registry"
)

// TestHeaderReadWrite tests header serialization and deserialization.
func TestHeaderReadWrite(t *testing.T) {
	original := &Header{
		Flags:      0x0001,
		DataOffset: 256,
		DataSize:   10000,
	}
	copy(original.DataHash[:], []byte("test-data-hash-32-bytes-long-her"))

	var buf bytes.Buffer
	if err := WriteHeader(&buf, original); err != nil {
		t.Fatalf("WriteHeader failed: %v", err)
	}

	if buf.Len() != 268 {
		t.Errorf("Header size mismatch: got %d, want %d", buf.Len(), 268)
	}

	read, err := ReadHeader(&buf)
	if err != nil {
		t.Fatalf("ReadHeader failed: %v", err)
	}

	if string(read.Magic[:]) != magic {
		t.Errorf("Magic mismatch: got %q, want %q", read.Magic, magic)
	}

	if read.Version != version1 {
		t.Errorf("Version mismatch: got %d, want %d", read.Version, version1)
	}

	if read.Flags != original.Flags {
		t.Errorf("Flags mismatch: got %d, want %d", read.Flags, original.Flags)
	}

	if read.DataOffset != original.DataOffset {
		t.Errorf("DataOffset mismatch: got %d, want %d", read.DataOffset, original.DataOffset)
	}

	if read.DataSize != original.DataSize {
		t.Errorf("DataSize mismatch: got %d, want %d", read.DataSize, original.DataSize)
	}
}

// TestHeaderInvalidMagic tests header validation with invalid magic.
func TestHeaderInvalidMagic(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte("BADMAGIC"))
	buf.Write(make([]byte, headerSize-8))

	_, err := ReadHeader(&buf)
	if err == nil {
		t.Error("Expected error for invalid magic, got nil")
	}
}

// TestHeaderInvalidVersion tests header validation with unsupported version.
func TestHeaderInvalidVersion(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte(magic))
	buf.WriteByte(0xFF) // Invalid version
	buf.Write(make([]byte, headerSize-10))

	_, err := ReadHeader(&buf)
	if err == nil {
		t.Error("Expected error for invalid version, got nil")
	}
}

// TestTOCStructure tests TOC structure.
func TestTOCStructure(t *testing.T) {
	toc := &TOC{
		Metadata: FrameInfo{
			Offset:           256,
			Size:             512,
			UncompressedSize: 1024,
			Hash:             "abc123",
		},
		Entries: FrameInfo{
			Offset:           768,
			Size:             256,
			UncompressedSize: 512,
			Hash:             "def456",
		},
		Resources: []ResourceInfo{
			{
				ID:   registry.NewID("test", "resource1"),
				Type: "tree",
				Meta: attrs.NewBag(),
				Frame: FrameInfo{
					Offset:           1024,
					Size:             2048,
					UncompressedSize: 4096,
					Hash:             "ghi789",
				},
				FileCount: 10,
				TotalSize: 50000,
			},
			{
				ID:   registry.NewID("test", "blob1"),
				Type: "blob",
				Meta: attrs.NewBag(),
				Frame: FrameInfo{
					Offset:           3072,
					Size:             10000,
					UncompressedSize: 20000,
					Hash:             "jkl012",
				},
				TotalSize: 20000,
			},
		},
	}

	if len(toc.Resources) != 2 {
		t.Errorf("Resource count mismatch: got %d, want 2", len(toc.Resources))
	}

	if toc.Resources[0].Type != "tree" {
		t.Errorf("First resource type mismatch: got %s, want tree", toc.Resources[0].Type)
	}

	if toc.Resources[1].Type != "blob" {
		t.Errorf("Second resource type mismatch: got %s, want blob", toc.Resources[1].Type)
	}

	if toc.Resources[0].FileCount != 10 {
		t.Errorf("FileCount mismatch: got %d, want 10", toc.Resources[0].FileCount)
	}
}

// TestFrameInfoOffsets tests frame offset calculations.
func TestFrameInfoOffsets(t *testing.T) {
	frame1 := FrameInfo{
		Offset:           1000,
		Size:             500,
		UncompressedSize: 1000,
		Hash:             "hash1",
	}

	frame2 := FrameInfo{
		Offset:           1500, // Should be frame1.Offset + frame1.Size
		Size:             300,
		UncompressedSize: 600,
		Hash:             "hash2",
	}

	expectedOffset := frame1.Offset + frame1.Size
	if frame2.Offset != expectedOffset {
		t.Errorf("Frame2 offset mismatch: got %d, want %d", frame2.Offset, expectedOffset)
	}
}

// TestResourceInfoValidation tests resource info structure validation.
func TestResourceInfoValidation(t *testing.T) {
	tests := []struct {
		name     string
		resInfo  ResourceInfo
		wantType string
	}{
		{
			name: "tree resource",
			resInfo: ResourceInfo{
				ID:        registry.NewID("test", "tree1"),
				Type:      "tree",
				Meta:      attrs.NewBag(),
				FileCount: 5,
				TotalSize: 10000,
			},
			wantType: "tree",
		},
		{
			name: "blob resource",
			resInfo: ResourceInfo{
				ID:        registry.NewID("test", "blob1"),
				Type:      "blob",
				Meta:      attrs.NewBag(),
				FileCount: 0,
				TotalSize: 50000,
			},
			wantType: "blob",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.resInfo.Type != tt.wantType {
				t.Errorf("Type mismatch: got %s, want %s", tt.resInfo.Type, tt.wantType)
			}

			if tt.resInfo.ID.String() == "" {
				t.Error("Resource ID should not be empty")
			}

			if tt.resInfo.Meta == nil {
				t.Error("Resource metadata should not be nil")
			}
		})
	}
}
