// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/wapp"
)

// Exercise the actual CLI in fresh processes: its command state is global.
// Explicit metadata clocks avoid sleeping and model independent CI jobs.
func TestModulePackReproducibleAcrossCheckoutTimes(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "src"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "assets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "wippy.yaml"), []byte("organization: acme\nmodule: app\nversion: 1.0.0\nembed:\n  - acme.app:assets\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "wippy.lock"), []byte("directories:\n  modules: .wippy\n  src: ./src\nmodules: []\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "src", "_index.yaml"), []byte("version: '1.0'\nnamespace: acme.app\nentries:\n  - name: assets\n    kind: fs.directory\n    directory: ./assets\n    auto_init: false\n"), 0o644))
	asset := filepath.Join(root, "assets", "hello.txt")
	require.NoError(t, os.WriteFile(asset, []byte("portable content"), 0o644))
	pack := func(name, clock string, mtime int64, module string) []byte {
		t.Helper()
		require.NoError(t, os.Chtimes(asset, time.Unix(mtime, 0), time.Unix(mtime, 0)))
		command := exec.Command(os.Args[0], "-test.run=^TestModulePackCLIProcess$")
		command.Dir = root
		command.Env = []string{"WIPPY_PACK_TEST_HELPER=1", "WIPPY_PACK_TEST_OUTPUT=" + name,
			"WIPPY_PACK_TEST_CLOCK=" + clock, "WIPPY_PACK_TEST_MODULE=" + module, "HOME=" + root, "TMPDIR=" + root, "PATH=" + os.Getenv("PATH")}
		output, err := command.CombinedOutput()
		require.NoError(t, err, "%s", output)
		data, err := os.ReadFile(filepath.Join(root, name))
		require.NoError(t, err)
		return data
	}
	baseline := pack("baseline.wapp", "2026-10-07T00:00:00Z", 100, "acme/app")
	t.Run("packing clock", func(t *testing.T) {
		require.True(t, bytes.Equal(baseline, pack("clock.wapp", "2026-10-08T00:00:00Z", 100, "acme/app")), "packing time changed module bytes")
	})
	t.Run("checkout mtime", func(t *testing.T) {
		require.True(t, bytes.Equal(baseline, pack("mtime.wapp", "2026-10-07T00:00:00Z", 200, "acme/app")), "checkout mtime changed module bytes")
	})
	t.Run("snapshot clocks", func(t *testing.T) {
		data := pack("snapshot.wapp", "2026-10-07T00:00:00Z", 100, "")
		reader, err := wapp.NewReader(bytes.NewReader(data))
		require.NoError(t, err)
		metadata, err := reader.GetMetadata()
		require.NoError(t, err)
		require.Equal(t, "2026-10-07T00:00:00Z", metadata["packed_at"])
		fsys, err := reader.GetFS(wapp.NewID("acme.app", "assets"))
		require.NoError(t, err)
		info, err := fs.Stat(fsys, "hello.txt")
		require.NoError(t, err)
		require.Equal(t, int64(100), info.ModTime().Unix())
	})
	reader, err := wapp.NewReader(bytes.NewReader(baseline))
	require.NoError(t, err)
	metadata, err := reader.GetMetadata()
	require.NoError(t, err)
	require.NotContains(t, metadata, "packed_at")
	fsys, err := reader.GetFS(wapp.NewID("acme.app", "assets"))
	require.NoError(t, err)
	file, err := fsys.Open("hello.txt")
	require.NoError(t, err)
	defer file.Close()
	info, err := file.Stat()
	require.NoError(t, err)
	require.Equal(t, int64(0), info.ModTime().Unix())
	content, err := fs.ReadFile(fsys, "hello.txt")
	require.NoError(t, err)
	require.Equal(t, "portable content", string(content))
	require.Equal(t, fs.FileMode(0o644), info.Mode().Perm())
	require.NoError(t, os.WriteFile(asset, []byte("changed content"), 0o644))
	require.False(t, bytes.Equal(baseline, pack("changed.wapp", "2026-10-07T00:00:00Z", 100, "acme/app")), "content change must change module bytes")
}

func TestModulePackCLIProcess(t *testing.T) {
	if os.Getenv("WIPPY_PACK_TEST_HELPER") != "1" {
		return
	}
	args := []string{"pack", os.Getenv("WIPPY_PACK_TEST_OUTPUT"),
		"--meta", "packed_at=" + os.Getenv("WIPPY_PACK_TEST_CLOCK"), "--silent"}
	if module := os.Getenv("WIPPY_PACK_TEST_MODULE"); module != "" {
		args = append(args, "--module", module)
	}
	err := ExecuteWithOptions(context.Background(), ExecuteOptions{LockFile: "wippy.lock", Args: args})
	require.NoError(t, err)
}
