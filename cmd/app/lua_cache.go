// SPDX-License-Identifier: MPL-2.0

package app

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

// LuaCacheSeed is a cache snapshot created while assembling an application: a
// zip archive with one compressed member per cache file. The archive digest
// covers every embedded cache file.
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

// seedLuaCache authenticates the complete embedded set once, then indexes the
// archive's directory. Members stay compressed in the embedded bytes and are
// decompressed only when the runtime asks for an entry, so adopting the seed
// keeps no decompressed copy. No entry is installed, rehashed, or pruned on
// disk. The runtime still checks fingerprints and decodes artifacts before use,
// falling back to compilation on a mismatch.
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
	files, err := indexLuaCacheArchive(seed.Archive)
	if err != nil {
		return nil, err
	}
	return &embeddedLuaCache{files: files}, nil
}

type embeddedLuaCache struct{ files map[string]*zip.File }

// read decompresses one member, or reports that the archive has none by name.
func (s *embeddedLuaCache) read(name string) ([]byte, bool) {
	file := s.files[name]
	if file == nil {
		return nil, false
	}
	data, err := readLuaCacheMember(file)
	if err != nil {
		return nil, false
	}
	return data, true
}

func (s *embeddedLuaCache) Get(key string) (*cache.Entry, bool, error) {
	if !isCacheKey(key) {
		return nil, false, nil
	}
	prefix := "v1/entries/" + key + "/"
	raw, ok := s.read(prefix + "meta.json")
	if !ok {
		return nil, false, nil
	}
	var meta cache.Meta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, false, nil
	}
	entry := &cache.Entry{Meta: meta}
	// The archive digest authenticates these immutable bytes, including metadata.
	// Unlike mutable disk files, they need no individual hash check on each read.
	if meta.ProtoHash != "" {
		entry.Proto, _ = s.read(prefix + "proto.luac")
	}
	if meta.ManifestHash != "" {
		entry.Manifest, _ = s.read(prefix + "manifest.bin")
	}
	if meta.DiagnosticsHash != "" {
		diagnostics, ok := s.read(prefix + "diags.json")
		if !ok || json.Unmarshal(diagnostics, &entry.Diagnostics) != nil {
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

// indexLuaCacheArchive validates every member of the archive's directory and
// maps each cache file to its member, without decompressing any of them.
func indexLuaCacheArchive(archive []byte) (map[string]*zip.File, error) {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("open embedded cache archive: %w", err)
	}
	files := make(map[string]*zip.File, len(reader.File))
	var total uint64
	for _, file := range reader.File {
		name := path.Clean(file.Name)
		if name != strings.TrimSuffix(file.Name, "/") || strings.HasPrefix(name, "/") || name == "." {
			return nil, fmt.Errorf("invalid embedded cache path %q", file.Name)
		}
		parts := strings.Split(name, "/")
		if parts[0] != "v1" || (len(parts) > 1 && parts[1] != "entries") || len(parts) > 4 {
			return nil, fmt.Errorf("invalid embedded cache path %q", file.Name)
		}
		if len(parts) >= 3 && !isCacheKey(parts[2]) {
			return nil, fmt.Errorf("invalid embedded cache key in %q", file.Name)
		}
		if file.FileInfo().IsDir() {
			if len(parts) > 3 || file.UncompressedSize64 != 0 {
				return nil, fmt.Errorf("invalid embedded cache directory %q", file.Name)
			}
			continue
		}
		if !file.Mode().IsRegular() || len(parts) != 4 || !allowedCacheFile(parts[3]) {
			return nil, fmt.Errorf("invalid embedded cache file %q", file.Name)
		}
		if file.Method != zip.Store && file.Method != zip.Deflate {
			return nil, fmt.Errorf("unsupported compression for embedded cache file %q", file.Name)
		}
		if file.UncompressedSize64 > uint64(maxEmbeddedLuaCacheBytes)-total {
			return nil, fmt.Errorf("embedded cache files exceed %d bytes", maxEmbeddedLuaCacheBytes)
		}
		total += file.UncompressedSize64
		if _, exists := files[name]; exists {
			return nil, fmt.Errorf("duplicate embedded cache file %q", name)
		}
		files[name] = file
	}
	return files, nil
}

// readLuaCacheMember decompresses one member; the zip reader checks its CRC.
func readLuaCacheMember(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(io.LimitReader(reader, int64(file.UncompressedSize64)+1))
	if err != nil {
		return nil, err
	}
	if uint64(len(data)) != file.UncompressedSize64 {
		return nil, fmt.Errorf("embedded cache file %q has the wrong size", file.Name)
	}
	return data, nil
}

func allowedCacheFile(name string) bool {
	switch name {
	case "meta.json", "manifest.bin", "diags.json", "proto.luac":
		return true
	default:
		return false
	}
}
