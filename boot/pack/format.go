// Package pack provides pack file format structures.
package pack

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"reflect"
	"sync"

	"github.com/hashicorp/go-msgpack/v2/codec"
	"github.com/klauspost/compress/zstd"
	"github.com/wippyai/runtime/api/registry"
)

const (
	// Pack format version
	version1 byte = 0x01
	// Header size (binary.Write adds 8 bytes padding after Version field)
	headerSize = 268
	// Footer size
	footerSize = 16

	// Frame indices
	MetadataFrameIndex  = 0
	EntriesFrameIndex   = 1
	ResourceFrameIndex  = 2
	FirstDataFrameIndex = 3
)

// Header represents the pack file header.
type Header struct {
	Magic      [9]byte   // "WIPPYPACK"
	Version    byte      // 0x01
	Flags      uint16    // Reserved flags
	DataOffset uint64    // Offset to data section
	DataSize   uint64    // Total size of data section
	DataHash   [32]byte  // SHA-256 of data section
	Reserved   [208]byte // Reserved for future use
}

// Footer represents the pack file footer.
type Footer struct {
	TOCOffset uint64 // Offset to compressed TOC
	TOCSize   uint64 // Compressed TOC size
}

// ReadHeader reads and validates a pack header.
func ReadHeader(r io.Reader) (*Header, error) {
	h := &Header{}
	if err := binary.Read(r, binary.LittleEndian, h); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	if string(h.Magic[:]) != magic {
		return nil, fmt.Errorf("invalid magic: %q", h.Magic)
	}

	if h.Version != version1 {
		return nil, fmt.Errorf("unsupported version: %d", h.Version)
	}

	return h, nil
}

// WriteHeader writes a pack header.
func WriteHeader(w io.Writer, h *Header) error {
	copy(h.Magic[:], magic)
	h.Version = version1

	if err := binary.Write(w, binary.LittleEndian, h); err != nil {
		return fmt.Errorf("write header: %w", err)
	}

	return nil
}

// ReadFooter reads a pack footer from a seeker.
func ReadFooter(r io.ReadSeeker) (*Footer, error) {
	// Seek to footer position (end - footerSize)
	if _, err := r.Seek(-footerSize, io.SeekEnd); err != nil {
		return nil, fmt.Errorf("seek to footer: %w", err)
	}

	f := &Footer{}
	if err := binary.Read(r, binary.LittleEndian, f); err != nil {
		return nil, fmt.Errorf("read footer: %w", err)
	}

	return f, nil
}

// WriteFooter writes a pack footer.
func WriteFooter(w io.Writer, f *Footer) error {
	if err := binary.Write(w, binary.LittleEndian, f); err != nil {
		return fmt.Errorf("write footer: %w", err)
	}

	return nil
}

// TOC represents the table of contents.
type TOC struct {
	// Pack metadata frame
	Metadata FrameInfo `msgpack:"metadata"`

	// Registry entries frame
	Entries FrameInfo `msgpack:"entries"`

	// Resource frames
	Resources []ResourceInfo `msgpack:"resources"`

	// Data frames (uncompressed, contain file bytes)
	DataFrames []FrameInfo `msgpack:"data_frames"`
}

// FrameInfo describes a data frame location and integrity.
type FrameInfo struct {
	Offset           uint64 `msgpack:"offset"`      // Offset in file
	Size             uint64 `msgpack:"size"`        // Compressed size
	UncompressedSize uint64 `msgpack:"uncomp_size"` // Uncompressed size
	Hash             string `msgpack:"hash"`        // SHA-256 hex
}

// ResourceInfo describes a resource in the pack.
type ResourceInfo struct {
	ID        registry.ID       `msgpack:"id"`
	Type      string            `msgpack:"type"` // "tree" or "blob"
	Meta      registry.Metadata `msgpack:"meta"`
	Frame     FrameInfo         `msgpack:"frame"`
	FileCount uint32            `msgpack:"file_count,omitempty"`
	TotalSize uint64            `msgpack:"total_size,omitempty"`
}

// newMsgpackHandle creates a msgpack handle with standard pack configuration.
func newMsgpackHandle() *codec.MsgpackHandle {
	mh := &codec.MsgpackHandle{}
	mh.MapType = reflect.TypeOf(map[string]interface{}(nil))
	mh.SliceType = nil
	mh.RawToString = true
	mh.Canonical = true
	mh.StructToArray = false
	return mh
}

// zstdDecoderPool pools zstd decoders to reduce allocations.
var zstdDecoderPool = sync.Pool{
	New: func() interface{} {
		decoder, _ := zstd.NewReader(nil)
		return decoder
	},
}

// decompressZstd decompresses zstd-compressed data using pooled decoder.
func decompressZstd(compressed []byte) ([]byte, error) {
	decoder := zstdDecoderPool.Get().(*zstd.Decoder)
	defer zstdDecoderPool.Put(decoder)

	err := decoder.Reset(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("reset zstd decoder: %w", err)
	}

	return io.ReadAll(decoder)
}
