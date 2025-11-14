// Package pack provides compressed binary serialization for registry entries.
package pack

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"reflect"

	"github.com/hashicorp/go-msgpack/v2/codec"
	"github.com/klauspost/compress/zstd"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/hash"
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

// normalizePayload converts payloads to Golang format for consistent serialization.
// This ensures bit-stable hashing since all payloads are stored as map[string]any
// which msgpack can encode/decode deterministically.
func (p *Packer) normalizePayload(pl payload.Payload) (payload.Payload, error) {
	if pl == nil {
		return nil, nil
	}

	// Keep String, Bytes, Error as-is (primitive types)
	if pl.Format() == payload.String || pl.Format() == payload.Bytes || pl.Format() == payload.Error {
		return pl, nil
	}

	// Already Golang format - no conversion needed
	if pl.Format() == payload.Golang {
		return pl, nil
	}

	// Convert all other formats (JSON, YAML, Lua) to Golang format
	// This matches what the mutator does and ensures consistent hashing
	return p.transcoder.Transcode(pl, payload.Golang)
}

// Pack serializes entries to a compressed binary file with embedded hash.
// File format: [magic(8)][version(1)][hash(64)][zstd(msgpack({metadata, entries}))]
// Hash is computed over both metadata and entries.
func (p *Packer) Pack(entries []registry.Entry, path string, metadata registry.Metadata) error {
	// Convert entries to encoded format and normalize payloads
	encodedEntries := make([]encodedEntry, len(entries))
	normalizedEntries := make([]registry.Entry, len(entries))

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

		// Store normalized entry for hashing
		normalizedEntries[i] = registry.Entry{
			ID:   entry.ID,
			Kind: entry.Kind,
			Meta: entry.Meta,
			Data: normalizedPayload,
		}

		// Store encoded entry for serialization (uses SAME normalized format)
		encodedEntries[i] = encodedEntry{
			ID:   entry.ID,
			Kind: entry.Kind,
			Meta: entry.Meta,
			Data: encPayload,
		}
	}

	// Create pack data structure with metadata and entries
	packData := map[string]any{
		"metadata": metadata,
		"entries":  encodedEntries,
	}

	// Serialize pack data (metadata + entries) with msgpack
	var msgpackBuf bytes.Buffer
	encoder := codec.NewEncoder(&msgpackBuf, p.handle)
	if err := encoder.Encode(packData); err != nil {
		return fmt.Errorf("msgpack encode: %w", err)
	}

	// Compute SHA-256 hash of serialized msgpack data
	// This verifies pack file integrity, not content-addressable hashing
	hashBytes := sha256.Sum256(msgpackBuf.Bytes())
	entryHash := hex.EncodeToString(hashBytes[:])

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

// Unpack reads and verifies a pack file, returning the entries and metadata.
// Returns error if file is corrupted or hash verification fails.
func (p *Packer) Unpack(path string) ([]registry.Entry, registry.Metadata, error) {
	// Read file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read file: %w", err)
	}

	// Verify minimum size
	minSize := len(magic) + 1 + hashLen
	if len(data) < minSize {
		return nil, nil, fmt.Errorf("file too small: %d bytes, expected at least %d", len(data), minSize)
	}

	// Verify magic header
	if string(data[:len(magic)]) != magic {
		return nil, nil, fmt.Errorf("invalid magic header: expected %q", magic)
	}
	offset := len(magic)

	// Read and verify version
	fileVersion := data[offset]
	offset++
	if fileVersion != version {
		return nil, nil, fmt.Errorf("unsupported version: %d (expected %d)", fileVersion, version)
	}

	// Extract stored hash
	storedHash := string(data[offset : offset+hashLen])
	offset += hashLen

	// Decompress data
	zstdDecoder, err := zstd.NewReader(bytes.NewReader(data[offset:]))
	if err != nil {
		return nil, nil, fmt.Errorf("create zstd decoder: %w", err)
	}
	defer zstdDecoder.Close()

	var decompressedBuf bytes.Buffer
	if _, err := io.Copy(&decompressedBuf, zstdDecoder); err != nil {
		return nil, nil, fmt.Errorf("zstd decompress: %w", err)
	}

	// Compute hash of decompressed msgpack data before decoding
	msgpackData := decompressedBuf.Bytes()
	hashBytes := sha256.Sum256(msgpackData)
	computedHash := hex.EncodeToString(hashBytes[:])

	// Unmarshal pack data (metadata + entries)
	var packData map[string]any
	decoder := codec.NewDecoder(&decompressedBuf, p.handle)
	if err := decoder.Decode(&packData); err != nil {
		return nil, nil, fmt.Errorf("msgpack decode: %w", err)
	}

	// Extract metadata
	var metadata registry.Metadata
	if metaData, ok := packData["metadata"]; ok {
		if metaMap, ok := metaData.(map[string]any); ok {
			metadata = registry.Metadata(metaMap)
		}
	}

	// Extract entries
	encodedEntries, ok := packData["entries"].([]any)
	if !ok {
		return nil, nil, fmt.Errorf("invalid pack data: entries not found")
	}

	// Convert encoded entries back to registry entries
	entries := make([]registry.Entry, len(encodedEntries))
	for i, encEntryData := range encodedEntries {
		encEntryMap, ok := encEntryData.(map[string]any)
		if !ok {
			return nil, nil, fmt.Errorf("invalid entry format at index %d", i)
		}

		var entry registry.Entry

		// Extract ID
		if idData, ok := encEntryMap["ID"]; ok {
			if idMap, ok := idData.(map[string]any); ok {
				ns, _ := idMap["ns"].(string)
				name, _ := idMap["name"].(string)
				entry.ID = registry.ID{NS: ns, Name: name}
			}
		}

		// Extract Kind
		if kind, ok := encEntryMap["Kind"].(string); ok {
			entry.Kind = kind
		}

		// Extract Meta
		if metaData, ok := encEntryMap["Meta"].(map[string]any); ok {
			entry.Meta = registry.Metadata(metaData)
		}

		// Extract Data
		if dataMap, ok := encEntryMap["Data"].(map[string]any); ok {
			if dataMap != nil {
				format := payload.Format(0)
				// Handle different types msgpack might use for Format field
				switch f := dataMap["Format"].(type) {
				case string:
					format = payload.Format(f)
				case float64:
					format = payload.Format(int(f))
				case int64:
					format = payload.Format(f)
				case int:
					format = payload.Format(f)
				case uint8:
					format = payload.Format(f)
				}
				data := dataMap["Data"]
				entry.Data = payload.NewPayload(data, format)
			}
		}

		entries[i] = entry
	}

	// Verify hash (already computed earlier before decoding)
	if computedHash != storedHash {
		return nil, nil, fmt.Errorf("hash mismatch: stored=%s computed=%s", storedHash, computedHash)
	}

	return entries, metadata, nil
}

// UnpackBytes unpacks entries and metadata from raw pack bytes.
// Returns error if data is corrupted or hash verification fails.
func (p *Packer) UnpackBytes(data []byte) ([]registry.Entry, registry.Metadata, error) {
	// Verify minimum size
	minSize := len(magic) + 1 + hashLen
	if len(data) < minSize {
		return nil, nil, fmt.Errorf("data too small: %d bytes, expected at least %d", len(data), minSize)
	}

	// Verify magic header
	if string(data[:len(magic)]) != magic {
		return nil, nil, fmt.Errorf("invalid magic header: expected %q", magic)
	}
	offset := len(magic)

	// Read and verify version
	fileVersion := data[offset]
	offset++
	if fileVersion != version {
		return nil, nil, fmt.Errorf("unsupported version: %d (expected %d)", fileVersion, version)
	}

	// Extract stored hash
	storedHash := string(data[offset : offset+hashLen])
	offset += hashLen

	// Decompress data
	zstdDecoder, err := zstd.NewReader(bytes.NewReader(data[offset:]))
	if err != nil {
		return nil, nil, fmt.Errorf("create zstd decoder: %w", err)
	}
	defer zstdDecoder.Close()

	var decompressedBuf bytes.Buffer
	if _, err := io.Copy(&decompressedBuf, zstdDecoder); err != nil {
		return nil, nil, fmt.Errorf("zstd decompress: %w", err)
	}

	// Compute hash of decompressed msgpack data before decoding
	msgpackData := decompressedBuf.Bytes()
	hashBytes := sha256.Sum256(msgpackData)
	computedHash := hex.EncodeToString(hashBytes[:])

	// Unmarshal pack data (metadata + entries)
	var packData map[string]any
	decoder := codec.NewDecoder(&decompressedBuf, p.handle)
	if err := decoder.Decode(&packData); err != nil {
		return nil, nil, fmt.Errorf("msgpack decode: %w", err)
	}

	// Extract metadata
	var metadata registry.Metadata
	if metaData, ok := packData["metadata"]; ok {
		if metaMap, ok := metaData.(map[string]any); ok {
			metadata = registry.Metadata(metaMap)
		}
	}

	// Extract entries
	encodedEntries, ok := packData["entries"].([]any)
	if !ok {
		return nil, nil, fmt.Errorf("invalid pack data: entries not found")
	}

	// Convert encoded entries back to registry entries
	entries := make([]registry.Entry, len(encodedEntries))
	for i, encEntryData := range encodedEntries {
		encEntryMap, ok := encEntryData.(map[string]any)
		if !ok {
			return nil, nil, fmt.Errorf("invalid entry format at index %d", i)
		}

		var entry registry.Entry

		// Extract ID
		if idData, ok := encEntryMap["ID"]; ok {
			if idMap, ok := idData.(map[string]any); ok {
				ns, _ := idMap["ns"].(string)
				name, _ := idMap["name"].(string)
				entry.ID = registry.ID{NS: ns, Name: name}
			}
		}

		// Extract Kind
		if kind, ok := encEntryMap["Kind"].(string); ok {
			entry.Kind = kind
		}

		// Extract Meta
		if metaData, ok := encEntryMap["Meta"].(map[string]any); ok {
			entry.Meta = registry.Metadata(metaData)
		}

		// Extract Data
		if dataMap, ok := encEntryMap["Data"].(map[string]any); ok {
			if dataMap != nil {
				format := payload.Format(0)
				// Handle different types msgpack might use for Format field
				switch f := dataMap["Format"].(type) {
				case string:
					format = payload.Format(f)
				case float64:
					format = payload.Format(int(f))
				case int64:
					format = payload.Format(f)
				case int:
					format = payload.Format(f)
				case uint8:
					format = payload.Format(f)
				}
				data := dataMap["Data"]
				entry.Data = payload.NewPayload(data, format)
			}
		}

		entries[i] = entry
	}

	// Verify hash
	if computedHash != storedHash {
		return nil, nil, fmt.Errorf("hash mismatch: stored=%s computed=%s", storedHash, computedHash)
	}

	return entries, metadata, nil
}

// verifyChecksum computes SHA-256 of data and compares with expected hex string.
func verifyChecksum(data []byte, expectedHex string) bool {
	hash := sha256.Sum256(data)
	computed := fmt.Sprintf("%x", hash)
	return computed == expectedHex
}
