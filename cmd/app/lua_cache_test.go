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

func TestSeedLuaCacheInstallsVerifiedCompileAndTypecheckEntries(t *testing.T) {
	state := t.TempDir()
	seed, compileKey, typecheckKey := testLuaCacheSeed(t)
	require.NoError(t, seedLuaCache(state, &seed))

	store := cache.NewDiskStore(luaCachePath(state))
	compileEntry, ok, err := store.Get(compileKey)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("proto"), compileEntry.Proto)
	typecheckEntry, ok, err := store.Get(typecheckKey)
	require.NoError(t, err)
	require.True(t, ok)
	require.NotEmpty(t, typecheckEntry.Manifest)

	marker := filepath.Join(luaCachePath(state), ".seed-"+hex.EncodeToString(sha256Bytes(seed.Archive)))
	require.FileExists(t, marker)
}

func TestSeedLuaCacheRejectsDigestAndIdentityMismatch(t *testing.T) {
	seed, _, _ := testLuaCacheSeed(t)
	state := t.TempDir()
	seed.Archive = append(seed.Archive, 0)
	require.ErrorContains(t, seedLuaCache(state, &seed), "digest mismatch")
	require.NoDirExists(t, luaCachePath(state))

	seed, _, _ = testLuaCacheSeed(t)
	seed.ToolchainIdentity = "different-toolchain"
	require.ErrorContains(t, seedLuaCache(state, &seed), "identity mismatch")
	require.NoDirExists(t, luaCachePath(state))
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

func makeLuaCacheArchive(t *testing.T, root string) []byte {
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
