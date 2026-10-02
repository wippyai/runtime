// SPDX-License-Identifier: MPL-2.0

package app

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/code/cache"
)

const maxEmbeddedLuaCacheBytes = 1 << 30

// LuaCacheSeed is a compressed cache snapshot created while assembling an
// application. The archive digest covers every embedded cache file.
type LuaCacheSeed struct {
	Digest            string
	SchemaVersion     string
	ToolchainIdentity string
	Archive           []byte
}

// LuaCacheIdentity returns the cache schema and toolchain identity linked into
// this runtime. Generated application code uses these values in its seed.
func LuaCacheIdentity() (schema, toolchain string, err error) {
	toolchain, err = code.ToolchainIdentity()
	return code.CacheSchemaVersion(), toolchain, err
}

// seedLuaCache authenticates the complete embedded set once, then indexes its
// bytes in memory. Entry metadata is decoded on demand; no entry is installed,
// rehashed, or pruned on disk. The runtime still checks fingerprints and decodes
// artifacts before use, falling back to compilation on a mismatch.
func seedLuaCache(seed *LuaCacheSeed) (cache.Reader, error) {
	if seed == nil || len(seed.Archive) == 0 {
		return nil, nil
	}
	if len(seed.Archive) > maxEmbeddedLuaCacheBytes {
		return nil, fmt.Errorf("embedded archive exceeds %d bytes", maxEmbeddedLuaCacheBytes)
	}
	schema, toolchain, err := LuaCacheIdentity()
	if err != nil {
		return nil, fmt.Errorf("read runtime Lua cache identity: %w", err)
	}
	if seed.SchemaVersion != schema || seed.ToolchainIdentity != toolchain {
		return nil, fmt.Errorf("embedded cache identity mismatch (schema %q, toolchain %q)", seed.SchemaVersion, seed.ToolchainIdentity)
	}
	hash := sha256.Sum256(seed.Archive)
	if seed.Digest != "sha256:"+hex.EncodeToString(hash[:]) {
		return nil, fmt.Errorf("embedded cache digest mismatch")
	}
	store := embeddedLuaCache{files: make(map[string][]byte)}
	if err := readLuaCacheArchive(seed.Archive, func(name string, data []byte) error {
		if _, exists := store.files[name]; exists {
			return fmt.Errorf("duplicate embedded cache file %q", name)
		}
		store.files[name] = data
		return nil
	}); err != nil {
		return nil, err
	}
	return &store, nil
}

type embeddedLuaCache struct{ files map[string][]byte }

func (s *embeddedLuaCache) Get(key string) (*cache.Entry, bool, error) {
	if !isCacheKey(key) {
		return nil, false, nil
	}
	prefix := "v1/entries/" + key + "/"
	var meta cache.Meta
	if err := json.Unmarshal(s.files[prefix+"meta.json"], &meta); err != nil {
		return nil, false, nil
	}
	entry := &cache.Entry{Meta: meta}
	// The archive digest authenticates these immutable bytes, including metadata.
	// Unlike mutable disk files, they need no individual hash check on each read.
	if meta.ProtoHash != "" {
		entry.Proto = bytes.Clone(s.files[prefix+"proto.luac"])
	}
	if meta.ManifestHash != "" {
		entry.Manifest = bytes.Clone(s.files[prefix+"manifest.bin"])
	}
	if meta.DiagnosticsHash != "" {
		if err := json.Unmarshal(s.files[prefix+"diags.json"], &entry.Diagnostics); err != nil {
			return nil, false, nil
		}
	}
	if !validLuaCacheEntry(key, entry) {
		return nil, false, nil
	}
	return entry, true, nil
}

func mergeLuaCache(source, destination string) error {
	entriesDir := filepath.Join(source, "v1", "entries")
	entries, err := os.ReadDir(entriesDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	destinationEntries := filepath.Join(destination, "v1", "entries")
	if err := os.MkdirAll(destinationEntries, 0o700); err != nil {
		return err
	}
	sourceStore := cache.NewDiskStore(source)
	destinationStore := cache.NewDiskStore(destination)
	for _, entryDir := range entries {
		if !entryDir.IsDir() || entryDir.Type()&os.ModeSymlink != 0 || !isCacheKey(entryDir.Name()) {
			return fmt.Errorf("invalid Lua cache entry %q", entryDir.Name())
		}
		key := entryDir.Name()
		entry, ok, err := sourceStore.Get(key)
		if err != nil || !ok || !validLuaCacheEntry(key, entry) {
			return fmt.Errorf("invalid Lua cache entry %q", key)
		}
		if existing, ok, err := destinationStore.Get(key); err == nil && ok && validLuaCacheEntry(key, existing) {
			continue
		}
		// The archive already supplied the exact hashed files in private staging.
		// Adopt that directory after verification instead of rewriting each file
		// and pruning the growing store after every batch of individual puts.
		target := filepath.Join(destinationEntries, key)
		if err := os.RemoveAll(target); err != nil {
			return fmt.Errorf("remove invalid derived cache entry %q: %w", key, err)
		}
		if err := os.Rename(filepath.Join(entriesDir, key), target); err != nil {
			return fmt.Errorf("merge Lua cache entry %q: %w", key, err)
		}
	}
	return destinationStore.Prune()
}

func validLuaCacheEntry(key string, entry *cache.Entry) bool {
	if entry == nil || entry.Meta.SchemaVersion != cache.SchemaVersion {
		return false
	}
	compile := entry.Meta.CompileFingerprint != "" && key == cache.CompileKey(entry.Meta.CompileFingerprint) && len(entry.Proto) > 0
	typecheck := entry.Meta.TypecheckFingerprint != "" && key == cache.TypecheckKey(entry.Meta.TypecheckFingerprint) && len(entry.Manifest) > 0
	return compile != typecheck
}

func isCacheKey(key string) bool {
	if len(key) != 64 {
		return false
	}
	for _, c := range key {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func readLuaCacheArchive(archive []byte, consume func(string, []byte) error) error {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return fmt.Errorf("open embedded cache archive: %w", err)
	}
	defer func() { _ = gz.Close() }()
	reader := tar.NewReader(gz)
	var total int64
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read embedded cache archive: %w", err)
		}
		name := path.Clean(header.Name)
		if name != header.Name || strings.HasPrefix(name, "/") || name == "." {
			return fmt.Errorf("invalid embedded cache path %q", header.Name)
		}
		parts := strings.Split(name, "/")
		if len(parts) < 1 || parts[0] != "v1" || (len(parts) > 1 && parts[1] != "entries") || len(parts) > 4 {
			return fmt.Errorf("invalid embedded cache path %q", header.Name)
		}
		if len(parts) >= 3 && !isCacheKey(parts[2]) {
			return fmt.Errorf("invalid embedded cache key in %q", header.Name)
		}
		if header.Typeflag == tar.TypeDir {
			if len(parts) > 3 || header.Size != 0 {
				return fmt.Errorf("invalid embedded cache directory %q", header.Name)
			}
			continue
		}
		if header.Typeflag != tar.TypeReg || len(parts) != 4 || !allowedCacheFile(parts[3]) || header.Size < 0 {
			return fmt.Errorf("invalid embedded cache file %q", header.Name)
		}
		if header.Size > maxEmbeddedLuaCacheBytes-total {
			return fmt.Errorf("embedded cache files exceed %d bytes", maxEmbeddedLuaCacheBytes)
		}
		total += header.Size

		data, err := io.ReadAll(io.LimitReader(reader, header.Size))
		if err != nil {
			return err
		}
		if int64(len(data)) != header.Size {
			return io.ErrUnexpectedEOF
		}
		if err := consume(name, data); err != nil {
			return err
		}
	}
}

func allowedCacheFile(name string) bool {
	switch name {
	case "meta.json", "manifest.bin", "diags.json", "proto.luac":
		return true
	default:
		return false
	}
}
