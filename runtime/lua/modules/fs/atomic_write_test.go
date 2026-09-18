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

// callWrite invokes writefile with the supplied option table entries.
func callWrite(t *testing.T, filesystem *FS, path string, content lua.LValue, options map[string]lua.LValue) (*lua.LState, int) {
	t.Helper()
	l := lua.NewState()
	ud := l.NewUserData()
	ud.Value = filesystem
	l.Push(ud)
	l.Push(lua.LString(path))
	l.Push(content)
	if options != nil {
		table := l.NewTable()
		for key, value := range options {
			table.RawSetString(key, value)
		}
		l.Push(table)
	}
	return l, fsWritefile(l)
}

func callAtomicWrite(t *testing.T, filesystem *FS, path, content string) (*lua.LState, int) {
	t.Helper()
	return callWrite(t, filesystem, path, lua.LString(content), map[string]lua.LValue{"atomic": lua.LTrue})
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
	assert.Equal(t, iofs.FileMode(0644), backend.perm)
	data, err := os.ReadFile(filepath.Join(backend.root, "config"))
	require.NoError(t, err)
	assert.Equal(t, "new", string(data))
}

// Without the option, writefile keeps its ordinary truncating behavior and
// never reaches the atomic capability.
func TestFSWritefileWithoutAtomicOptionUsesOrdinaryWrite(t *testing.T) {
	for name, options := range map[string]map[string]lua.LValue{
		"absent":   nil,
		"empty":    {},
		"disabled": {"atomic": lua.LFalse},
	} {
		t.Run(name, func(t *testing.T) {
			f, backend, cleanup := newAtomicTestFS(t, nil)
			defer cleanup()
			l, nret := callWrite(t, f, "config", lua.LString("plain"), options)
			defer l.Close()
			require.Equal(t, 2, nret)
			require.Equal(t, lua.LTrue, l.Get(-2))
			assert.False(t, backend.called)
			data, err := os.ReadFile(filepath.Join(backend.root, "config"))
			require.NoError(t, err)
			assert.Equal(t, "plain", string(data))
		})
	}
}

// The option table carries the write mode that the string argument carries.
func TestFSWritefileOptionTableCarriesMode(t *testing.T) {
	f, _, cleanup := newAtomicTestFS(t, nil)
	defer cleanup()
	root := f.fs.(*atomicTestFS).root
	require.NoError(t, os.WriteFile(filepath.Join(root, "log"), []byte("first"), 0644))

	l, nret := callWrite(t, f, "log", lua.LString("-second"), map[string]lua.LValue{"mode": lua.LString("a")})
	defer l.Close()
	require.Equal(t, 2, nret)
	require.Equal(t, lua.LTrue, l.Get(-2))
	data, err := os.ReadFile(filepath.Join(root, "log"))
	require.NoError(t, err)
	assert.Equal(t, "first-second", string(data))
}

// Atomic publication replaces the whole file, so the appending and exclusive
// modes cannot be combined with it.
func TestFSWritefileRejectsAtomicWithIncompatibleMode(t *testing.T) {
	for _, mode := range []string{"a", "wx"} {
		t.Run(mode, func(t *testing.T) {
			f, backend, cleanup := newAtomicTestFS(t, nil)
			defer cleanup()
			l, nret := callWrite(t, f, "config", lua.LString("content"), map[string]lua.LValue{
				"atomic": lua.LTrue,
				"mode":   lua.LString(mode),
			})
			defer l.Close()
			require.Equal(t, 2, nret)
			assert.Equal(t, lua.LFalse, l.Get(-2))
			assert.Equal(t, lua.Invalid, requireLuaError(t, l.Get(-1)).Kind())
			assert.False(t, backend.called)
		})
	}
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
		assert.ErrorIs(t, luaErr, fsapi.ErrAtomicWriteUnsupported)
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
		assert.ErrorIs(t, luaErr, fsapi.ErrPermissionDenied)
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

// Atomic publication needs the whole document up front, so a stream is refused
// rather than silently buffered.
func TestFSWritefileAtomicRejectsReaderContent(t *testing.T) {
	f, backend, cleanup := newAtomicTestFS(t, nil)
	defer cleanup()
	l := lua.NewState()
	defer l.Close()
	fsUD := l.NewUserData()
	fsUD.Value = f
	reader := l.NewUserData()
	reader.Value = strings.NewReader("streamed")
	l.Push(fsUD)
	l.Push(lua.LString("config"))
	l.Push(reader)
	options := l.NewTable()
	options.RawSetString("atomic", lua.LTrue)
	l.Push(options)

	nret := fsWritefile(l)
	require.Equal(t, 2, nret)
	assert.Equal(t, lua.LFalse, l.Get(-2))
	assert.Equal(t, lua.Invalid, requireLuaError(t, l.Get(-1)).Kind())
	assert.False(t, backend.called)
}

// The backend cause reaches the caller, exactly as the ordinary write path
// reports its causes, while the document itself stays out of the message.
func TestFSWritefileAtomicBackendFailurePreservesCause(t *testing.T) {
	backendErr := errors.New("device reported a hardware fault")
	f, backend, cleanup := newAtomicTestFS(t, backendErr)
	defer cleanup()
	l, nret := callAtomicWrite(t, f, "config", "private content")
	defer l.Close()
	require.Equal(t, 2, nret)
	assert.Equal(t, lua.LFalse, l.Get(-2))
	luaErr := requireLuaError(t, l.Get(-1))
	assert.Equal(t, lua.Internal, luaErr.Kind())
	assert.ErrorIs(t, luaErr, backendErr)
	assert.Contains(t, luaErr.Error(), "atomic write failed")
	assert.Contains(t, luaErr.Error(), backendErr.Error())
	assert.NotContains(t, luaErr.Error(), "private content")
	assert.True(t, backend.called)
}

// A wrapped filesystem error keeps both its kind mapping and its cause.
func TestFSWritefileAtomicWrappedPermissionCausePreserved(t *testing.T) {
	backendErr := &iofs.PathError{Op: "writefile_atomic", Path: "config", Err: fsapi.ErrReadOnly}
	f, _, cleanup := newAtomicTestFS(t, backendErr)
	defer cleanup()
	l, nret := callAtomicWrite(t, f, "config", "private content")
	defer l.Close()
	require.Equal(t, 2, nret)
	luaErr := requireLuaError(t, l.Get(-1))
	assert.Equal(t, lua.PermissionDenied, luaErr.Kind())
	assert.ErrorIs(t, luaErr, fsapi.ErrReadOnly)
	var pathErr *iofs.PathError
	assert.ErrorAs(t, luaErr, &pathErr)
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
	assert.ErrorIs(t, luaErr, fsapi.ErrPublishedSyncFailed)
	assert.Equal(t, true, luaErr.Details()["published"])
	assert.True(t, backend.called)
}
