// Package pack provides compressed binary serialization for registry entries.
package pack

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"reflect"

	"github.com/hashicorp/go-msgpack/v2/codec"
	"github.com/klauspost/compress/zstd"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/boot/hash"
)

const (
	// Magic header identifying Wippy pack files
	magic = "WIPPYPACK"
	// Current format version
	version byte = 0x01
	// Hash length in bytes (SHA-256 hex = 64 chars)
	hashLen = 64
)

// encodedPayload is an intermediate structure for msgpack serialization
type encodedPayload struct {
	Format payload.Format
	Data   any
}

// encodedEntry is an intermediate structure for msgpack serialization
type encodedEntry struct {
	ID   registry.ID
	Kind string
	Meta registry.Metadata
	Data *encodedPayload
}

// Packer handles packing and unpacking of registry entries.
type Packer struct {
	transcoder payload.Transcoder
	hasher     *hash.Hasher
	handle     *codec.MsgpackHandle
}

// New creates a new Packer.
func New(transcoder payload.Transcoder) *Packer {
	mh := &codec.MsgpackHandle{}
	mh.MapType = reflect.TypeOf(map[string]any(nil))
	mh.SliceType = reflect.TypeOf([]any(nil))
	mh.RawToString = true
	mh.Canonical = true // Ensures deterministic encoding by sorting map keys

	return &Packer{
		transcoder: transcoder,
		hasher:     hash.New(transcoder),
		handle:     mh,
	}
}

// normalizePayload converts payloads to a format suitable for serialization
func (p *Packer) normalizePayload(pl payload.Payload) (payload.Payload, error) {
	if pl == nil {
		return nil, nil
	}

	switch pl.Format() {
	case payload.Golang, payload.String, payload.Bytes, payload.Error:
		return pl, nil
	case payload.JSON, payload.YAML, payload.Lua:
		return p.transcoder.Transcode(pl, payload.JSON)
	default:
		return p.transcoder.Transcode(pl, payload.JSON)
	}
}

// Pack serializes entries to a compressed binary file with embedded hash.
// File format: [magic(8)][version(1)][hash(64)][zstd(msgpack(entries))]
func (p *Packer) Pack(entries []registry.Entry, path string) error {
	// Convert entries to encoded format and normalize payloads
	normalizedEntries := make([]registry.Entry, len(entries))
	encodedEntries := make([]encodedEntry, len(entries))
	for i, entry := range entries {
		var encPayload *encodedPayload
		var normalizedPayload payload.Payload

		if entry.Data != nil {
			normalized, err := p.normalizePayload(entry.Data)
			if err != nil {
				return fmt.Errorf("normalize payload for entry %d: %w", i, err)
			}
			normalizedPayload = normalized
			encPayload = &encodedPayload{
				Format: normalized.Format(),
				Data:   normalized.Data(),
			}
		}

		normalizedEntries[i] = registry.Entry{
			ID:   entry.ID,
			Kind: entry.Kind,
			Meta: entry.Meta,
			Data: normalizedPayload,
		}

		encodedEntries[i] = encodedEntry{
			ID:   entry.ID,
			Kind: entry.Kind,
			Meta: entry.Meta,
			Data: encPayload,
		}
	}

	// Compute hash of normalized entries
	entryHash, err := p.hasher.Hash(normalizedEntries)
	if err != nil {
		return fmt.Errorf("compute hash: %w", err)
	}

	// Serialize entries with msgpack
	var msgpackBuf bytes.Buffer
	encoder := codec.NewEncoder(&msgpackBuf, p.handle)
	if err := encoder.Encode(encodedEntries); err != nil {
		return fmt.Errorf("msgpack encode: %w", err)
	}

	// Compress with zstd at best compression level
	var compressedBuf bytes.Buffer
	zstdEncoder, err := zstd.NewWriter(&compressedBuf, zstd.WithEncoderLevel(zstd.SpeedBestCompression))
	if err != nil {
		return fmt.Errorf("create zstd encoder: %w", err)
	}

	if _, err := zstdEncoder.Write(msgpackBuf.Bytes()); err != nil {
		zstdEncoder.Close()
		return fmt.Errorf("zstd compress: %w", err)
	}

	if err := zstdEncoder.Close(); err != nil {
		return fmt.Errorf("close zstd encoder: %w", err)
	}

	// Build file content: magic + version + hash + compressed data
	var fileBuf bytes.Buffer
	fileBuf.WriteString(magic)
	fileBuf.WriteByte(version)
	fileBuf.WriteString(entryHash)
	fileBuf.Write(compressedBuf.Bytes())

	// Write to file
	if err := os.WriteFile(path, fileBuf.Bytes(), 0644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

// Unpack reads and verifies a pack file, returning the entries.
// Returns error if file is corrupted or hash verification fails.
func (p *Packer) Unpack(path string) ([]registry.Entry, error) {
	// Read file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	// Verify minimum size
	minSize := len(magic) + 1 + hashLen
	if len(data) < minSize {
		return nil, fmt.Errorf("file too small: %d bytes, expected at least %d", len(data), minSize)
	}

	// Verify magic header
	if string(data[:len(magic)]) != magic {
		return nil, fmt.Errorf("invalid magic header: expected %q", magic)
	}
	offset := len(magic)

	// Read and verify version
	fileVersion := data[offset]
	offset++
	if fileVersion != version {
		return nil, fmt.Errorf("unsupported version: %d (expected %d)", fileVersion, version)
	}

	// Extract stored hash
	storedHash := string(data[offset : offset+hashLen])
	offset += hashLen

	// Decompress data
	zstdDecoder, err := zstd.NewReader(bytes.NewReader(data[offset:]))
	if err != nil {
		return nil, fmt.Errorf("create zstd decoder: %w", err)
	}
	defer zstdDecoder.Close()

	var decompressedBuf bytes.Buffer
	if _, err := io.Copy(&decompressedBuf, zstdDecoder); err != nil {
		return nil, fmt.Errorf("zstd decompress: %w", err)
	}

	// Unmarshal encoded entries
	var encodedEntries []encodedEntry
	decoder := codec.NewDecoder(&decompressedBuf, p.handle)
	if err := decoder.Decode(&encodedEntries); err != nil {
		return nil, fmt.Errorf("msgpack decode: %w", err)
	}

	// Convert encoded entries back to registry entries
	entries := make([]registry.Entry, len(encodedEntries))
	for i, encEntry := range encodedEntries {
		var pl payload.Payload
		if encEntry.Data != nil {
			pl = payload.NewPayload(encEntry.Data.Data, encEntry.Data.Format)
		}

		entries[i] = registry.Entry{
			ID:   encEntry.ID,
			Kind: encEntry.Kind,
			Meta: encEntry.Meta,
			Data: pl,
		}
	}

	// Verify hash
	computedHash, err := p.hasher.Hash(entries)
	if err != nil {
		return nil, fmt.Errorf("compute hash: %w", err)
	}

	if computedHash != storedHash {
		return nil, fmt.Errorf("hash mismatch: stored=%s computed=%s", storedHash, computedHash)
	}

	return entries, nil
}

// verifyChecksum computes SHA-256 of data and compares with expected hex string.
func verifyChecksum(data []byte, expectedHex string) bool {
	hash := sha256.Sum256(data)
	computed := fmt.Sprintf("%x", hash)
	return computed == expectedHex
}
