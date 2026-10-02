// SPDX-License-Identifier: MPL-2.0

package app

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	luaiio "github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/code/cache"
)

func TestSeedLuaCacheUsesVerifiedEntriesWithoutDiskInstallation(t *testing.T) {
	seed, compileKey, typecheckKey := testLuaCacheSeed(t)
	store, err := seedLuaCache(&seed)
	require.NoError(t, err)
	compileEntry, ok, err := store.Get(compileKey)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("proto"), compileEntry.Proto)
	compileEntry.Proto[0] = 0
	compileEntry.Meta.Deps = append(compileEntry.Meta.Deps, cache.DepMeta{ID: "changed"})
	again, ok, err := store.Get(compileKey)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("proto"), again.Proto)
	require.Empty(t, again.Meta.Deps)
	typecheckEntry, ok, err := store.Get(typecheckKey)
	require.NoError(t, err)
	require.True(t, ok)
	require.NotEmpty(t, typecheckEntry.Manifest)
	_, ok, err = store.Get(cache.CompileKey("absent"))
	require.NoError(t, err)
	require.False(t, ok)
}

func TestMergeLuaCacheMovesVerifiedEntriesAndReplacesCorruptDerivedData(t *testing.T) {
	seed, compileKey, _ := testLuaCacheSeed(t)
	staging, destination := t.TempDir(), t.TempDir()
	require.NoError(t, unpackLuaCacheSeed(seed.Archive, staging))
	sourcePath := filepath.Join(staging, "v1", "entries", compileKey)
	// Windows path-based Stat loads file identity lazily in SameFile. Capture
	// it through a handle while the source exists, then close before renaming.
	source, err := os.Open(sourcePath)
	require.NoError(t, err)
	before, statErr := source.Stat()
	closeErr := source.Close()
	require.NoError(t, statErr)
	require.NoError(t, closeErr)
	targetPath := filepath.Join(destination, "v1", "entries", compileKey)
	require.NoError(t, os.MkdirAll(targetPath, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(targetPath, "meta.json"), []byte("corrupt"), 0o600))
	require.NoError(t, mergeLuaCache(staging, destination))
	after, err := os.Stat(targetPath)
	require.NoError(t, err)
	require.True(t, os.SameFile(before, after), "verified entry must be adopted without rewriting every cache file")
	require.NoDirExists(t, sourcePath)
	entry, found, err := cache.NewDiskStore(destination).Get(compileKey)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("proto"), entry.Proto)
}

func TestSeedLuaCacheRejectsDigestAndIdentityMismatch(t *testing.T) {
	for _, mismatch := range []string{"digest", "toolchain", "schema"} {
		t.Run(mismatch, func(t *testing.T) {
			seed, _, _ := testLuaCacheSeed(t)
			switch mismatch {
			case "digest":
				seed.Archive = append(seed.Archive, 0)
			case "toolchain":
				seed.ToolchainIdentity = "different"
			case "schema":
				seed.SchemaVersion = "different"
			}
			store, err := seedLuaCache(&seed)
			require.Error(t, err)
			require.Nil(t, store)
		})
	}
}

func TestSeedLuaCacheSkipsInvalidMetadataLazily(t *testing.T) {
	seed, compileKey, _ := testLuaCacheSeed(t)
	staging := t.TempDir()
	require.NoError(t, unpackLuaCacheSeed(seed.Archive, staging))
	require.NoError(t, os.WriteFile(filepath.Join(staging, "v1", "entries", compileKey, "meta.json"), []byte("corrupt"), 0o600))
	seed.Archive = makeLuaCacheArchive(t, staging)
	seed.Digest = "sha256:" + hex.EncodeToString(sha256Bytes(seed.Archive))
	store, err := seedLuaCache(&seed)
	require.NoError(t, err, "adoption must not decode every entry")
	_, found, err := store.Get(compileKey)
	require.NoError(t, err)
	require.False(t, found, "invalid metadata must be a cache miss")
}

// Only update-cache merge tests need a materialized archive.
func unpackLuaCacheSeed(archive []byte, destination string) error {
	return readLuaCacheArchive(archive, func(name string, data []byte) error {
		target := filepath.Join(destination, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	})
}

func TestUnpackLuaCacheSeedRejectsUnsafeEntries(t *testing.T) {
	key := cache.CompileKey("fingerprint")
	for _, tc := range []struct {
		name string
		kind byte
	}{
		{name: "../escape", kind: tar.TypeReg},
		{name: "/escape", kind: tar.TypeReg},
		{name: "v1/entries/invalid/meta.json", kind: tar.TypeReg},
		{name: "v1/entries/" + key + "/unexpected", kind: tar.TypeReg},
		{name: "v1/entries/" + key + "/proto.luac", kind: tar.TypeSymlink},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var archive bytes.Buffer
			gz := gzip.NewWriter(&archive)
			writer := tar.NewWriter(gz)
			require.NoError(t, writer.WriteHeader(&tar.Header{Name: tc.name, Typeflag: tc.kind, Linkname: "../escape"}))
			require.NoError(t, writer.Close())
			require.NoError(t, gz.Close())
			require.Error(t, unpackLuaCacheSeed(archive.Bytes(), t.TempDir()))
		})
	}
}

func testLuaCacheSeed(t *testing.T) (LuaCacheSeed, string, string) {
	t.Helper()
	identity, err := code.ToolchainIdentity()
	require.NoError(t, err)
	root := t.TempDir()
	store := cache.NewDiskStore(root)
	id := registry.NewID("app", "main")
	sourceHash := cache.SourceHash("return 1", "main")
	compileFP := code.CompileFingerprint(identity, id.String(), lua.Function, sourceHash, "main", nil)
	typeCfg := code.TypeCheckConfig{Enabled: true, Strict: true}
	typecheckFP := code.TypecheckFingerprint(identity, id.String(), lua.Function, sourceHash, "main", code.TypecheckConfigHash(typeCfg), code.BuiltinManifestHash(nil), nil)
	manifest, err := luaiio.NewManifest("app.main").Encode()
	require.NoError(t, err)
	require.NoError(t, store.Put(cache.CompileKey(compileFP), &cache.Entry{
		Meta:  cache.Meta{SchemaVersion: cache.SchemaVersion, CompileFingerprint: compileFP, EntryID: id.String(), Kind: lua.Function, Method: "main", SourceHash: sourceHash},
		Proto: []byte("proto"),
	}))
	require.NoError(t, store.Put(cache.TypecheckKey(typecheckFP), &cache.Entry{
		Meta:     cache.Meta{SchemaVersion: cache.SchemaVersion, TypecheckFingerprint: typecheckFP, EntryID: id.String(), Kind: lua.Function, Method: "main", SourceHash: sourceHash},
		Manifest: manifest,
	}))
	archive := makeLuaCacheArchive(t, root)
	hash := sha256.Sum256(archive)
	schema, toolchain, err := LuaCacheIdentity()
	require.NoError(t, err)
	return LuaCacheSeed{Archive: archive, Digest: "sha256:" + hex.EncodeToString(hash[:]), SchemaVersion: schema, ToolchainIdentity: toolchain}, cache.CompileKey(compileFP), cache.TypecheckKey(typecheckFP)
}

func makeLuaCacheArchive(t testing.TB, root string) []byte {
	t.Helper()
	var output bytes.Buffer
	gz := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(gz)
	require.NoError(t, filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name, err = filepath.Rel(root, path)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(header.Name)
		header.ModTime = time.Time{}
		header.AccessTime = time.Time{}
		header.ChangeTime = time.Time{}
		header.Typeflag = tar.TypeReg
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tarWriter, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}))
	require.NoError(t, tarWriter.Close())
	require.NoError(t, gz.Close())
	return output.Bytes()
}

func sha256Bytes(data []byte) []byte {
	hash := sha256.Sum256(data)
	return hash[:]
}

func TestSeedLuaCacheRejectsDuplicateAndTruncatedArchives(t *testing.T) {
	seed, key, _ := testLuaCacheSeed(t)
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	writer := tar.NewWriter(gz)
	name := "v1/entries/" + key + "/meta.json"
	for range 2 {
		require.NoError(t, writer.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg}))
	}
	require.NoError(t, writer.Close())
	require.NoError(t, gz.Close())
	seed.Archive = archive.Bytes()
	seed.Digest = "sha256:" + hex.EncodeToString(sha256Bytes(seed.Archive))
	store, err := seedLuaCache(&seed)
	require.ErrorContains(t, err, "duplicate")
	require.Nil(t, store)
	seed.Archive = seed.Archive[:len(seed.Archive)/2]
	seed.Digest = "sha256:" + hex.EncodeToString(sha256Bytes(seed.Archive))
	store, err = seedLuaCache(&seed)
	require.Error(t, err)
	require.Nil(t, store)
}

func TestReadLuaCacheArchiveRejectsOversizedContentsBeforeReading(t *testing.T) {
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	writer := tar.NewWriter(gz)
	require.NoError(t, writer.WriteHeader(&tar.Header{
		Name:     "v1/entries/" + cache.CompileKey("oversized") + "/proto.luac",
		Typeflag: tar.TypeReg, Size: maxEmbeddedLuaCacheBytes + 1,
	}))
	// Deliberately omit the body: rejection must happen before allocating it.
	require.NoError(t, gz.Close())
	err := readLuaCacheArchive(archive.Bytes(), func(string, []byte) error { t.Fatal("oversized contents reached the cache"); return nil })
	require.ErrorContains(t, err, "exceed")
}
