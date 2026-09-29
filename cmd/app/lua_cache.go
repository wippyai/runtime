// SPDX-License-Identifier: MPL-2.0

package app

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
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

func seedLuaCache(state string, seed *LuaCacheSeed) error {
	if seed == nil || len(seed.Archive) == 0 {
		return nil
	}
	if len(seed.Archive) > maxEmbeddedLuaCacheBytes {
		return fmt.Errorf("embedded archive exceeds %d bytes", maxEmbeddedLuaCacheBytes)
	}
	schema, toolchain, err := LuaCacheIdentity()
	if err != nil {
		return fmt.Errorf("read runtime Lua cache identity: %w", err)
	}
	if seed.SchemaVersion != schema || seed.ToolchainIdentity != toolchain {
		return fmt.Errorf("embedded cache identity mismatch (schema %q, toolchain %q)", seed.SchemaVersion, seed.ToolchainIdentity)
	}
	hash := sha256.Sum256(seed.Archive)
	digest := "sha256:" + hex.EncodeToString(hash[:])
	if seed.Digest != digest {
		return fmt.Errorf("embedded cache digest mismatch")
	}

	directory := luaCachePath(state)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	marker := filepath.Join(directory, ".seed-"+hex.EncodeToString(hash[:]))
	if info, err := os.Lstat(marker); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("cache seed marker is not a regular file")
		}
		data, err := os.ReadFile(marker)
		if err != nil {
			return err
		}
		if string(data) == digest {
			return nil
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	staging, err := os.MkdirTemp(directory, ".seed-stage-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(staging) }()
	if err := unpackLuaCacheSeed(seed.Archive, staging); err != nil {
		return err
	}
	if err := mergeLuaCache(staging, directory); err != nil {
		return fmt.Errorf("install embedded Lua cache: %w", err)
	}
	if err := writeLuaCacheSeedMarker(marker, digest); err != nil {
		return err
	}
	return nil
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
	if err := os.MkdirAll(destination, 0o700); err != nil {
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
		if err := destinationStore.Put(key, entry); err != nil {
			return fmt.Errorf("merge Lua cache entry %q: %w", key, err)
		}
	}
	return nil
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

func unpackLuaCacheSeed(archive []byte, destination string) error {
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
			if err := os.MkdirAll(filepath.Join(destination, filepath.FromSlash(name)), 0o700); err != nil {
				return err
			}
			continue
		}
		if header.Typeflag != tar.TypeReg || len(parts) != 4 || !allowedCacheFile(parts[3]) || header.Size < 0 {
			return fmt.Errorf("invalid embedded cache file %q", header.Name)
		}
		if total+header.Size > maxEmbeddedLuaCacheBytes {
			return fmt.Errorf("embedded cache files exceed %d bytes", maxEmbeddedLuaCacheBytes)
		}
		total += header.Size
		filePath := filepath.Join(destination, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filePath), 0o700); err != nil {
			return err
		}
		file, err := os.OpenFile(filePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		_, copyErr := io.CopyN(file, reader, header.Size)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
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

func writeLuaCacheSeedMarker(marker, digest string) error {
	tmp, err := os.CreateTemp(filepath.Dir(marker), ".seed-marker-")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := io.WriteString(tmp, digest); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, marker)
}
