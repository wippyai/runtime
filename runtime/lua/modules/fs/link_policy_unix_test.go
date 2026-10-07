//go:build unix

// SPDX-License-Identifier: MPL-2.0

package fs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/service/fs/directory"
)

func TestOwnerSafeLuaReads(t *testing.T) {
	userHome, err := os.UserHomeDir()
	require.NoError(t, err)
	base, err := os.MkdirTemp(userHome, ".wippy-lua-link-test-")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(base) })
	home := filepath.Join(base, "home")
	require.NoError(t, os.Mkdir(home, 0700))
	target := filepath.Join(base, "fixture")
	require.NoError(t, os.WriteFile(target, []byte("fixture"), 0600))
	require.NoError(t, os.Symlink(target, filepath.Join(home, "login")))
	for _, policy := range []string{"", "owner_safe"} {
		backend, err := directory.NewFactory().CreateFS(directory.CreateFSConfig{DirPath: home, Mode: 0700, LinkPolicy: policy})
		require.NoError(t, err)
		defer backend.(interface{ Close() error }).Close()
		for _, writable := range []bool{false, true} {
			mode := os.FileMode(0600)
			if writable {
				mode = 0620
			}
			require.NoError(t, os.Chmod(target, mode))
			for _, fn := range []func(*lua.LState) int{fsStat, fsReadfile, fsExists} {
				l := lua.NewState()
				l.SetContext(t.Context())
				ud := l.NewUserData()
				ud.Value = NewFS(backend, "")
				l.Push(ud)
				l.Push(lua.LString("login"))
				n := fn(l)
				require.Equal(t, 2, n)
				if policy == "owner_safe" && writable {
					e := requireLuaError(t, l.Get(-1))
					require.Equal(t, lua.PermissionDenied, e.Kind())
					require.Contains(t, e.Error(), target)
					require.Contains(t, e.Error(), "group/other-writable")
				} else if policy == "owner_safe" {
					require.Equal(t, lua.LNil, l.Get(-1))
				}
				l.Close()
			}
		}
	}
}

func TestFSRemoveUnlinksDirectoryLink(t *testing.T) {
	tmpDir := t.TempDir()
	fsys, err := directory.NewFS(tmpDir, 0755, false)
	require.NoError(t, err)
	defer func() { _ = fsys.Close() }()
	target := filepath.Join(tmpDir, "target")
	require.NoError(t, os.Mkdir(target, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(target, "file.txt"), []byte("kept"), 0600))
	require.NoError(t, os.Symlink("target", filepath.Join(tmpDir, "link")))

	l := lua.NewState()
	defer l.Close()
	ud := l.NewUserData()
	ud.Value = NewFS(fsys, "")
	l.Push(ud)
	l.Push(lua.LString("link"))

	require.Equal(t, 2, fsRemove(l))
	require.Equal(t, lua.LNil, l.Get(-1))
	require.Equal(t, lua.LTrue, l.Get(-2))
	_, err = os.Lstat(filepath.Join(tmpDir, "link"))
	require.True(t, os.IsNotExist(err))
	content, err := os.ReadFile(filepath.Join(target, "file.txt"))
	require.NoError(t, err)
	require.Equal(t, "kept", string(content))
}
