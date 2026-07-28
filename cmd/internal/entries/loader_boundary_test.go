// SPDX-License-Identifier: MPL-2.0

package entries

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
	contextapi "github.com/wippyai/runtime/api/context"
	embedapi "github.com/wippyai/runtime/api/service/fs/embed"
	"github.com/wippyai/runtime/boot/deps/lock"
	embedpkg "github.com/wippyai/runtime/service/fs/embed"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

func writeBoundaryPack(t *testing.T, path string, entries []wapp.Entry, resources []wapp.ResourceSpec) {
	t.Helper()
	file, err := os.Create(path)
	require.NoError(t, err)
	writer := wapp.NewWriter()
	if len(resources) == 0 {
		err = writer.PackEntries(wapp.Metadata{}, entries, file)
	} else {
		err = writer.PackWithResources(wapp.Metadata{}, entries, resources, file)
	}
	require.NoError(t, err)
	require.NoError(t, file.Close())
}

func TestB09EntryLoaderAcceptsUppercaseWAPP(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MODULE.WAPP")
	writeBoundaryPack(t, path, []wapp.Entry{{ID: wapp.NewID("app", "value"), Kind: "test.kind", Data: map[string]any{"ok": true}}}, nil)

	loaded, err := LoadEntriesFromPaths(setupTestContext(t), []string{path}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	require.Equal(t, "app:value", loaded[0].ID.String())
}

func TestB10CorruptLatePackRollsBack(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "valid.wapp")
	corrupt := filepath.Join(dir, "corrupt.wapp")
	writeBoundaryPack(t, valid, nil, []wapp.ResourceSpec{{ID: wapp.NewID("app", "assets"), FS: fstest.MapFS{"a.txt": &fstest.MapFile{Data: []byte("a")}}}})
	require.NoError(t, os.WriteFile(corrupt, []byte("not a pack"), 0o644))
	reg := embedpkg.NewRegistry()
	ctx := contextapi.WithAppContext(context.Background(), contextapi.NewAppContext())
	ctx = embedapi.WithRegistry(ctx, reg)

	err := registerWappWithEmbedRegistry(ctx, []lock.ModuleLoadPath{{Path: valid, Module: "valid", Version: "1"}, {Path: corrupt, Module: "corrupt", Version: "1"}}, zap.NewNop())
	require.Error(t, err)
	require.False(t, reg.HasModulePack("valid", "1"))
	require.False(t, hasOpenFDForPath(valid), "valid pack handle survived rollback")
}

func TestB11DuplicateLateEmbedRollsBack(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.wapp")
	second := filepath.Join(dir, "second.wapp")
	resource := func(data string) []wapp.ResourceSpec {
		return []wapp.ResourceSpec{{ID: wapp.NewID("app", "assets"), FS: fstest.MapFS{"data.txt": &fstest.MapFile{Data: []byte(data)}}}}
	}
	writeBoundaryPack(t, first, nil, resource("first"))
	writeBoundaryPack(t, second, nil, resource("second"))
	reg := embedpkg.NewRegistry()
	ctx := contextapi.WithAppContext(context.Background(), contextapi.NewAppContext())
	ctx = embedapi.WithRegistry(ctx, reg)

	err := registerWappWithEmbedRegistry(ctx, []lock.ModuleLoadPath{{Path: first, Module: "first", Version: "1"}, {Path: second, Module: "second", Version: "1"}}, zap.NewNop())
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate embedded resource")
	require.False(t, reg.HasModulePack("first", "1"))
	require.False(t, reg.HasModulePack("second", "1"))
	require.False(t, hasOpenFDForPath(first), "first staged handle survived rollback")
	require.False(t, hasOpenFDForPath(second), "second staged handle survived rollback")
}

func hasOpenFDForPath(path string) bool {
	fds, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return false
	}
	want, _ := filepath.EvalSymlinks(path)
	for _, fd := range fds {
		target, err := os.Readlink(filepath.Join("/proc/self/fd", fd.Name()))
		if err != nil {
			continue
		}
		target, _ = filepath.EvalSymlinks(target)
		if bytes.Equal([]byte(target), []byte(want)) {
			return true
		}
	}
	return false
}
