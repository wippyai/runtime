// SPDX-License-Identifier: MPL-2.0

package fs

import (
	"errors"
	iofs "io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/service/fs/directory"
)

type atomicTestFS struct {
	fsapi.FS
	err    error
	root   string
	name   string
	data   []byte
	perm   iofs.FileMode
	called bool
}

func (f *atomicTestFS) WriteFileAtomic(name string, data []byte, perm iofs.FileMode) error {
	f.called = true
	f.name = name
	f.data = append([]byte(nil), data...)
	f.perm = perm
	if f.err != nil {
		return f.err
	}
	return os.WriteFile(filepath.Join(f.root, name), data, perm)
}

func newAtomicTestFS(t *testing.T, backendErr error) (*FS, *atomicTestFS, func()) {
	t.Helper()
	root := t.TempDir()
	base, err := directory.NewFS(root, 0755, false)
	require.NoError(t, err)
	wrapped := &atomicTestFS{FS: base, root: root, err: backendErr}
	return NewFS(wrapped, ""), wrapped, func() { _ = base.Close() }
}

func callAtomicWrite(t *testing.T, filesystem *FS, path, content string) (*lua.LState, int) {
	t.Helper()
	l := lua.NewState()
	ud := l.NewUserData()
	ud.Value = filesystem
	l.Push(ud)
	l.Push(lua.LString(path))
	l.Push(lua.LString(content))
	return l, fsWritefileAtomic(l)
}

func TestFSWritefileAtomicSuccessUsesBoundedAtomicCapability(t *testing.T) {
	f, backend, cleanup := newAtomicTestFS(t, nil)
	defer cleanup()
	require.NoError(t, os.WriteFile(filepath.Join(backend.root, "config"), []byte("old"), 0644))

	l, nret := callAtomicWrite(t, f, "config", "new")
	defer l.Close()
	require.Equal(t, 2, nret)
	assert.Equal(t, lua.LTrue, l.Get(-2))
	assert.Equal(t, lua.LNil, l.Get(-1))
	assert.True(t, backend.called)
	assert.Equal(t, "config", backend.name)
	assert.Equal(t, []byte("new"), backend.data)
	assert.Equal(t, iofs.FileMode(0600), backend.perm)
	data, err := os.ReadFile(filepath.Join(backend.root, "config"))
	require.NoError(t, err)
	assert.Equal(t, "new", string(data))
}

func TestFSWritefileAtomicRefusesUnsupportedAndDeniedBackends(t *testing.T) {
	t.Run("unsupported", func(t *testing.T) {
		root := t.TempDir()
		base, err := directory.NewFS(root, 0755, false)
		require.NoError(t, err)
		defer base.Close()
		f := NewFS(fsapi.NewReadOnlyFS(base), "")
		l, nret := callAtomicWrite(t, f, "config", "secret-free content")
		defer l.Close()
		require.Equal(t, 2, nret)
		assert.Equal(t, lua.LFalse, l.Get(-2))
		luaErr := requireLuaError(t, l.Get(-1))
		assert.Equal(t, lua.Unavailable, luaErr.Kind())
		assert.Contains(t, luaErr.Error(), "atomic write unsupported")
		assert.NotContains(t, luaErr.Error(), "secret-free content")
	})

	t.Run("permission denied", func(t *testing.T) {
		f, backend, cleanup := newAtomicTestFS(t, fsapi.ErrPermissionDenied)
		defer cleanup()
		l, nret := callAtomicWrite(t, f, "config", "private content")
		defer l.Close()
		require.Equal(t, 2, nret)
		assert.Equal(t, lua.LFalse, l.Get(-2))
		luaErr := requireLuaError(t, l.Get(-1))
		assert.Equal(t, lua.PermissionDenied, luaErr.Kind())
		assert.True(t, backend.called)
		assert.NotContains(t, luaErr.Error(), "private content")
	})
}

func TestFSWritefileAtomicRejectsInvalidPathsAndOversizedInputBeforeBackend(t *testing.T) {
	paths := []string{"", "../escape", "bad\x00path"}
	for _, path := range paths {
		t.Run("path "+path, func(t *testing.T) {
			f, backend, cleanup := newAtomicTestFS(t, nil)
			defer cleanup()
			l, nret := callAtomicWrite(t, f, path, "content")
			defer l.Close()
			require.Equal(t, 2, nret)
			assert.Equal(t, lua.LFalse, l.Get(-2))
			assert.Equal(t, lua.Invalid, requireLuaError(t, l.Get(-1)).Kind())
			assert.False(t, backend.called)
		})
	}

	f, backend, cleanup := newAtomicTestFS(t, nil)
	defer cleanup()
	l, nret := callAtomicWrite(t, f, "config", strings.Repeat("x", (8<<20)+1))
	defer l.Close()
	require.Equal(t, 2, nret)
	assert.Equal(t, lua.LFalse, l.Get(-2))
	luaErr := requireLuaError(t, l.Get(-1))
	assert.Equal(t, lua.Invalid, luaErr.Kind())
	assert.Contains(t, luaErr.Error(), "8 MiB")
	assert.False(t, backend.called)
}

func TestFSWritefileAtomicBackendFailureIsSafe(t *testing.T) {
	secret := "backend failure with secret content"
	f, backend, cleanup := newAtomicTestFS(t, errors.New(secret))
	defer cleanup()
	l, nret := callAtomicWrite(t, f, "config", "private content")
	defer l.Close()
	require.Equal(t, 2, nret)
	assert.Equal(t, lua.LFalse, l.Get(-2))
	luaErr := requireLuaError(t, l.Get(-1))
	assert.Equal(t, lua.Internal, luaErr.Kind())
	assert.Equal(t, "atomic write failed", luaErr.Error())
	assert.NotContains(t, luaErr.Error(), secret)
	assert.True(t, backend.called)
}

func TestFSWritefileAtomicPublishedSyncFailureIsUncertain(t *testing.T) {
	f, backend, cleanup := newAtomicTestFS(t, fsapi.ErrPublishedSyncFailed)
	defer cleanup()
	l, nret := callAtomicWrite(t, f, "config", "new")
	defer l.Close()
	require.Equal(t, 2, nret)
	assert.Equal(t, lua.LFalse, l.Get(-2))
	luaErr := requireLuaError(t, l.Get(-1))
	assert.Equal(t, lua.Unavailable, luaErr.Kind())
	assert.Contains(t, luaErr.Error(), "atomic write published; sync status uncertain")
	assert.True(t, backend.called)
}
