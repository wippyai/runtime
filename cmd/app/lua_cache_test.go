// SPDX-License-Identifier: MPL-2.0

package app

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"

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
	files, err := indexLuaCacheArchive(archive)
	if err != nil {
		return err
	}
	for name, file := range files {
		data, err := readLuaCacheMember(file)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func TestIndexLuaCacheArchiveRejectsUnsafeEntries(t *testing.T) {
	key := cache.CompileKey("fingerprint")
	for _, tc := range []struct {
		name string
		mode os.FileMode
	}{
		{name: "../escape", mode: 0o600},
		{name: "/escape", mode: 0o600},
		{name: "v1/entries/invalid/meta.json", mode: 0o600},
		{name: "v1/entries/" + key + "/unexpected", mode: 0o600},
		{name: "v1/entries/" + key + "/proto.luac", mode: os.ModeSymlink | 0o777},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var archive bytes.Buffer
			writer := zip.NewWriter(&archive)
			header := &zip.FileHeader{Name: tc.name, Method: zip.Deflate}
			header.SetMode(tc.mode)
			member, err := writer.CreateHeader(header)
			require.NoError(t, err)
			_, err = member.Write([]byte("../escape"))
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			_, err = indexLuaCacheArchive(archive.Bytes())
			require.Error(t, err)
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
	writer := zip.NewWriter(&output)
	require.NoError(t, filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		member, err := writer.CreateHeader(&zip.FileHeader{Name: filepath.ToSlash(name), Method: zip.Deflate})
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = member.Write(data)
		return err
	}))
	require.NoError(t, writer.Close())
	return output.Bytes()
}

func sha256Bytes(data []byte) []byte {
	hash := sha256.Sum256(data)
	return hash[:]
}

func TestSeedLuaCacheRejectsDuplicateAndTruncatedArchives(t *testing.T) {
	seed, key, _ := testLuaCacheSeed(t)
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	name := "v1/entries/" + key + "/meta.json"
	for range 2 {
		_, err := writer.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
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

func TestIndexLuaCacheArchiveRejectsOversizedContents(t *testing.T) {
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	member, err := writer.CreateRaw(&zip.FileHeader{
		Name:               "v1/entries/" + cache.CompileKey("oversized") + "/proto.luac",
		Method:             zip.Store,
		UncompressedSize64: maxEmbeddedLuaCacheBytes + 1,
	})
	require.NoError(t, err)
	// The declared size alone must reject the archive; the body stays empty.
	_, err = member.Write(nil)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	_, err = indexLuaCacheArchive(archive.Bytes())
	require.ErrorContains(t, err, "exceed")
}

// TestSeedLuaCacheKeepsEntriesCompressedUntilRead verifies that adopting the
// embedded seed indexes it without decompressing its entries: a large entry
// costs nothing until the runtime asks for it.
func TestSeedLuaCacheKeepsEntriesCompressedUntilRead(t *testing.T) {
	seed, compileKey, _ := testLuaCacheSeed(t)
	staging := t.TempDir()
	require.NoError(t, unpackLuaCacheSeed(seed.Archive, staging))
	large := bytes.Repeat([]byte("bee "), 4<<20)
	disk := cache.NewDiskStore(staging)
	existing, ok, err := disk.Get(compileKey)
	require.NoError(t, err)
	require.True(t, ok)
	existing.Proto = large
	require.NoError(t, disk.Put(compileKey, existing))
	seed.Archive = makeLuaCacheArchive(t, staging)
	seed.Digest = "sha256:" + hex.EncodeToString(sha256Bytes(seed.Archive))

	var before, after goruntime.MemStats
	goruntime.GC()
	goruntime.ReadMemStats(&before)
	store, err := seedLuaCache(&seed)
	goruntime.ReadMemStats(&after)
	require.NoError(t, err)
	require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(len(large)/4), "adopting the seed decompressed its entries")

	entry, ok, err := store.Get(compileKey)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, large, entry.Proto)
}
