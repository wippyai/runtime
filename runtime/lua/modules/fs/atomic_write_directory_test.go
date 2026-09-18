//go:build linux || darwin || windows

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

func TestFSWritefileAtomicWithDirectoryBackend(t *testing.T) {
	root := t.TempDir()
	backend, err := directory.NewFS(root, 0700, false)
	require.NoError(t, err)
	defer backend.Close()
	require.NoError(t, os.Mkdir(filepath.Join(root, "state"), 0700))

	f := NewFS(backend, "")
	l, nret := callAtomicWrite(t, f, "state/config", "published")
	defer l.Close()
	require.Equal(t, 2, nret)
	require.Equal(t, lua.LTrue, l.Get(-2))
	require.Equal(t, lua.LNil, l.Get(-1))

	data, err := os.ReadFile(filepath.Join(root, "state", "config"))
	require.NoError(t, err)
	require.Equal(t, "published", string(data))
	// One verb, one mode: an atomic publication creates the file exactly as an
	// ordinary writefile does on the same mount, including the mount's cap.
	plain, err := backend.OpenFile("state/plain", os.O_WRONLY|os.O_CREATE, 0644)
	require.NoError(t, err)
	require.NoError(t, plain.Close())
	plainInfo, err := os.Stat(filepath.Join(root, "state", "plain"))
	require.NoError(t, err)
	info, err := os.Stat(filepath.Join(root, "state", "config"))
	require.NoError(t, err)
	require.Equal(t, plainInfo.Mode().Perm(), info.Mode().Perm())
}
