package fs

import (
	"errors"
	"os"
	"strings"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/runtime/resource"
	"github.com/wippyai/runtime/runtime/lua/modules/stream"
	"github.com/wippyai/runtime/service/fs/directory"
	streamsys "github.com/wippyai/runtime/system/stream"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
)

func requireLuaError(t *testing.T, val lua.LValue) *lua.Error {
	t.Helper()
	if err, isErr := val.(error); isErr {
		var luaErr *lua.Error
		if errors.As(err, &luaErr) {
			return luaErr
		}
	}
	require.Fail(t, "expected *lua.Error, got %T", val)
	return nil
}

func createTestFS(t *testing.T) (*FS, func()) {
	tmpDir := t.TempDir()
	fsys, err := directory.NewFS(tmpDir, 0755, false)
	require.NoError(t, err)
	return NewFS(fsys, ""), func() { _ = fsys.Close() }
}

func TestFSStructuredErrors(t *testing.T) {
	f, cleanup := createTestFS(t)
	defer cleanup()

	tests := []struct {
		name        string
		fn          func(*lua.LState) int
		setup       func(*lua.LState)
		expectedErr bool
		checkKind   string
	}{
		{
			name: "chdir empty path",
			fn:   fsChdir,
			setup: func(l *lua.LState) {
				ud := l.NewUserData()
				ud.Value = f
				l.Push(ud)
				l.Push(lua.LString(""))
			},
			expectedErr: true,
			checkKind:   string(lua.Invalid),
		},
		{
			name: "chdir non-existent",
			fn:   fsChdir,
			setup: func(l *lua.LState) {
				ud := l.NewUserData()
				ud.Value = f
				l.Push(ud)
				l.Push(lua.LString("/nonexistent"))
			},
			expectedErr: true,
			checkKind:   string(lua.NotFound),
		},
		{
			name: "open empty path",
			fn:   fsOpen,
			setup: func(l *lua.LState) {
				l.SetContext(t.Context())
				ud := l.NewUserData()
				ud.Value = f
				l.Push(ud)
				l.Push(lua.LString(""))
				l.Push(lua.LString("r"))
			},
			expectedErr: true,
			checkKind:   string(lua.Invalid),
		},
		{
			name: "open invalid mode",
			fn:   fsOpen,
			setup: func(l *lua.LState) {
				l.SetContext(t.Context())
				ud := l.NewUserData()
				ud.Value = f
				l.Push(ud)
				l.Push(lua.LString("/test.txt"))
				l.Push(lua.LString("xyz"))
			},
			expectedErr: true,
			checkKind:   string(lua.Invalid),
		},
		{
			name: "open non-existent file",
			fn:   fsOpen,
			setup: func(l *lua.LState) {
				l.SetContext(t.Context())
				ud := l.NewUserData()
				ud.Value = f
				l.Push(ud)
				l.Push(lua.LString("/nonexistent.txt"))
				l.Push(lua.LString("r"))
			},
			expectedErr: true,
			checkKind:   string(lua.NotFound),
		},
		{
			name: "stat empty path",
			fn:   fsStat,
			setup: func(l *lua.LState) {
				ud := l.NewUserData()
				ud.Value = f
				l.Push(ud)
				l.Push(lua.LString(""))
			},
			expectedErr: true,
			checkKind:   string(lua.Invalid),
		},
		{
			name: "stat non-existent",
			fn:   fsStat,
			setup: func(l *lua.LState) {
				ud := l.NewUserData()
				ud.Value = f
				l.Push(ud)
				l.Push(lua.LString("/nonexistent"))
			},
			expectedErr: true,
			checkKind:   string(lua.NotFound),
		},
		{
			name: "mkdir empty path",
			fn:   fsMkdir,
			setup: func(l *lua.LState) {
				ud := l.NewUserData()
				ud.Value = f
				l.Push(ud)
				l.Push(lua.LString(""))
			},
			expectedErr: true,
			checkKind:   string(lua.Invalid),
		},
		{
			name: "remove empty path",
			fn:   fsRemove,
			setup: func(l *lua.LState) {
				ud := l.NewUserData()
				ud.Value = f
				l.Push(ud)
				l.Push(lua.LString(""))
			},
			expectedErr: true,
			checkKind:   string(lua.Invalid),
		},
		{
			name: "readdir empty path",
			fn:   fsReaddir,
			setup: func(l *lua.LState) {
				ud := l.NewUserData()
				ud.Value = f
				l.Push(ud)
				l.Push(lua.LString(""))
			},
			expectedErr: true,
			checkKind:   string(lua.Invalid),
		},
		{
			name: "readfile empty path",
			fn:   fsReadfile,
			setup: func(l *lua.LState) {
				ud := l.NewUserData()
				ud.Value = f
				l.Push(ud)
				l.Push(lua.LString(""))
			},
			expectedErr: true,
			checkKind:   string(lua.Invalid),
		},
		{
			name: "readfile non-existent",
			fn:   fsReadfile,
			setup: func(l *lua.LState) {
				ud := l.NewUserData()
				ud.Value = f
				l.Push(ud)
				l.Push(lua.LString("/nonexistent.txt"))
			},
			expectedErr: true,
			checkKind:   string(lua.NotFound),
		},
		{
			name: "writefile empty path",
			fn:   fsWritefile,
			setup: func(l *lua.LState) {
				ud := l.NewUserData()
				ud.Value = f
				l.Push(ud)
				l.Push(lua.LString(""))
				l.Push(lua.LString("data"))
			},
			expectedErr: true,
			checkKind:   string(lua.Invalid),
		},
		{
			name: "writefile no data",
			fn:   fsWritefile,
			setup: func(l *lua.LState) {
				ud := l.NewUserData()
				ud.Value = f
				l.Push(ud)
				l.Push(lua.LString("/test.txt"))
				l.Push(lua.LNil)
			},
			expectedErr: true,
			checkKind:   string(lua.Invalid),
		},
		{
			name: "writefile invalid mode",
			fn:   fsWritefile,
			setup: func(l *lua.LState) {
				ud := l.NewUserData()
				ud.Value = f
				l.Push(ud)
				l.Push(lua.LString("/test.txt"))
				l.Push(lua.LString("data"))
				l.Push(lua.LString("xyz"))
			},
			expectedErr: true,
			checkKind:   string(lua.Invalid),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			tt.setup(l)
			nret := tt.fn(l)

			require.Equal(t, 2, nret, "should return 2 values")

			if tt.expectedErr {
				luaErr := requireLuaError(t, l.Get(-1))
				assert.Equal(t, tt.checkKind, string(luaErr.Kind()), "error kind should match")
			}
		})
	}
}

func TestFSMkdirAlreadyExists(t *testing.T) {
	tmpDir := t.TempDir()
	fsys, err := directory.NewFS(tmpDir, 0755, false)
	require.NoError(t, err)
	defer func() { _ = fsys.Close() }()
	f := NewFS(fsys, "")

	err = os.Mkdir(tmpDir+"/existing", 0755)
	require.NoError(t, err)

	l := lua.NewState()
	defer l.Close()

	ud := l.NewUserData()
	ud.Value = f
	l.Push(ud)
	l.Push(lua.LString("existing"))

	nret := fsMkdir(l)
	require.Equal(t, 2, nret)

	result := l.Get(-2)
	assert.Equal(t, lua.LFalse, result)

	luaErr := requireLuaError(t, l.Get(-1))
	assert.Equal(t, string(lua.AlreadyExists), string(luaErr.Kind()))
}

func TestFSChdirNotDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	fsys, err := directory.NewFS(tmpDir, 0755, false)
	require.NoError(t, err)
	defer func() { _ = fsys.Close() }()
	f := NewFS(fsys, "")

	err = os.WriteFile(tmpDir+"/file.txt", []byte("test"), 0600)
	require.NoError(t, err)

	l := lua.NewState()
	defer l.Close()

	ud := l.NewUserData()
	ud.Value = f
	l.Push(ud)
	l.Push(lua.LString("file.txt"))

	nret := fsChdir(l)
	require.Equal(t, 2, nret)

	result := l.Get(-2)
	assert.Equal(t, lua.LFalse, result)

	luaErr := requireLuaError(t, l.Get(-1))
	assert.Equal(t, string(lua.Invalid), string(luaErr.Kind()))
}

func TestFSReaddirNotDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	fsys, err := directory.NewFS(tmpDir, 0755, false)
	require.NoError(t, err)
	defer func() { _ = fsys.Close() }()
	f := NewFS(fsys, "")

	err = os.WriteFile(tmpDir+"/file.txt", []byte("test"), 0600)
	require.NoError(t, err)

	l := lua.NewState()
	defer l.Close()

	ud := l.NewUserData()
	ud.Value = f
	l.Push(ud)
	l.Push(lua.LString("file.txt"))

	nret := fsReaddir(l)
	require.Equal(t, 2, nret)

	result := l.Get(-2)
	assert.Equal(t, lua.LNil, result)

	luaErr := requireLuaError(t, l.Get(-1))
	assert.Equal(t, string(lua.Invalid), string(luaErr.Kind()))
}

func TestFSRemoveNonEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	fsys, err := directory.NewFS(tmpDir, 0755, false)
	require.NoError(t, err)
	defer func() { _ = fsys.Close() }()
	f := NewFS(fsys, "")

	dir := tmpDir + "/nonempty"
	err = os.Mkdir(dir, 0755)
	require.NoError(t, err)
	err = os.WriteFile(dir+"/file.txt", []byte("test"), 0600)
	require.NoError(t, err)

	l := lua.NewState()
	defer l.Close()

	ud := l.NewUserData()
	ud.Value = f
	l.Push(ud)
	l.Push(lua.LString("nonempty"))

	nret := fsRemove(l)
	require.Equal(t, 2, nret)

	result := l.Get(-2)
	assert.Equal(t, lua.LFalse, result)

	luaErr := requireLuaError(t, l.Get(-1))
	assert.Equal(t, string(lua.Invalid), string(luaErr.Kind()))
}

type testReaderCloser struct {
	*strings.Reader
}

func (t *testReaderCloser) Close() error { return nil }

func TestFSWritefileWithStream(t *testing.T) {
	tmpDir := t.TempDir()
	fsys, err := directory.NewFS(tmpDir, 0755, false)
	require.NoError(t, err)
	defer func() { _ = fsys.Close() }()
	f := NewFS(fsys, "")

	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	store := resource.NewStore()
	defer func() { _ = store.Close() }()
	err = resource.SetStore(ctx, store)
	require.NoError(t, err)
	table := resource.GetTable(ctx)
	require.NotNil(t, table)

	fileContent := "uploaded file content from multipart form"
	reader := strings.NewReader(fileContent)
	rc := &testReaderCloser{reader}

	streamID := streamsys.Insert(table, rc)

	l := lua.NewState()
	defer l.Close()
	l.SetContext(ctx)

	ud := l.NewUserData()
	ud.Value = f
	ud.Metatable = fsMetatable
	l.Push(ud)
	l.Push(lua.LString("/uploaded.txt"))

	streamUD := stream.NewStream(l, streamID)
	l.Push(streamUD)

	nret := fsWritefile(l)
	require.Equal(t, 2, nret)

	result := l.Get(-2)
	errVal := l.Get(-1)
	assert.Equal(t, lua.LTrue, result, "should succeed, error: %v", errVal)
	assert.Equal(t, lua.LNil, errVal, "should not have error")

	written, err := os.ReadFile(tmpDir + "/uploaded.txt")
	require.NoError(t, err)
	assert.Equal(t, fileContent, string(written))
}

func TestNullByteInjectionPrevention(t *testing.T) {
	f, cleanup := createTestFS(t)
	defer cleanup()

	pathsWithNullByte := []string{
		"/test\x00.txt",
		"test\x00file",
		"/dir\x00name/file",
		"normal/\x00hidden",
	}

	t.Run("resolvePath rejects null bytes", func(t *testing.T) {
		for _, p := range pathsWithNullByte {
			_, err := f.resolvePath(p)
			require.Error(t, err, "resolvePath should reject path with null byte: %q", p)
			assert.Equal(t, ErrNullBytePath, err)
		}
	})

	t.Run("stat rejects null bytes", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()

		for _, p := range pathsWithNullByte {
			l.SetTop(0)
			ud := l.NewUserData()
			ud.Value = f
			l.Push(ud)
			l.Push(lua.LString(p))

			nret := fsStat(l)
			require.Equal(t, 2, nret)
			assert.Equal(t, lua.LNil, l.Get(-2), "stat should return nil for null byte path")
			luaErr := requireLuaError(t, l.Get(-1))
			assert.Equal(t, string(lua.Invalid), string(luaErr.Kind()))
		}
	})

	t.Run("open rejects null bytes", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		l.SetContext(t.Context())

		for _, p := range pathsWithNullByte {
			l.SetTop(0)
			ud := l.NewUserData()
			ud.Value = f
			l.Push(ud)
			l.Push(lua.LString(p))
			l.Push(lua.LString("r"))

			nret := fsOpen(l)
			require.Equal(t, 2, nret)
			assert.Equal(t, lua.LNil, l.Get(-2), "open should return nil for null byte path")
			luaErr := requireLuaError(t, l.Get(-1))
			assert.Equal(t, string(lua.Invalid), string(luaErr.Kind()))
		}
	})

	t.Run("mkdir rejects null bytes", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()

		for _, p := range pathsWithNullByte {
			l.SetTop(0)
			ud := l.NewUserData()
			ud.Value = f
			l.Push(ud)
			l.Push(lua.LString(p))

			nret := fsMkdir(l)
			require.Equal(t, 2, nret)
			assert.Equal(t, lua.LFalse, l.Get(-2), "mkdir should return false for null byte path")
			luaErr := requireLuaError(t, l.Get(-1))
			assert.Equal(t, string(lua.Invalid), string(luaErr.Kind()))
		}
	})

	t.Run("remove rejects null bytes", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()

		for _, p := range pathsWithNullByte {
			l.SetTop(0)
			ud := l.NewUserData()
			ud.Value = f
			l.Push(ud)
			l.Push(lua.LString(p))

			nret := fsRemove(l)
			require.Equal(t, 2, nret)
			assert.Equal(t, lua.LFalse, l.Get(-2), "remove should return false for null byte path")
			luaErr := requireLuaError(t, l.Get(-1))
			assert.Equal(t, string(lua.Invalid), string(luaErr.Kind()))
		}
	})

	t.Run("readdir rejects null bytes", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()

		for _, p := range pathsWithNullByte {
			l.SetTop(0)
			ud := l.NewUserData()
			ud.Value = f
			l.Push(ud)
			l.Push(lua.LString(p))

			nret := fsReaddir(l)
			require.Equal(t, 2, nret)
			assert.Equal(t, lua.LNil, l.Get(-2), "readdir should return nil for null byte path")
			luaErr := requireLuaError(t, l.Get(-1))
			assert.Equal(t, string(lua.Invalid), string(luaErr.Kind()))
		}
	})

	t.Run("exists rejects null bytes", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()

		for _, p := range pathsWithNullByte {
			l.SetTop(0)
			ud := l.NewUserData()
			ud.Value = f
			l.Push(ud)
			l.Push(lua.LString(p))

			nret := fsExists(l)
			require.Equal(t, 2, nret)
			assert.Equal(t, lua.LFalse, l.Get(-2), "exists should return false for null byte path")
			luaErr := requireLuaError(t, l.Get(-1))
			assert.Equal(t, string(lua.Invalid), string(luaErr.Kind()))
		}
	})

	t.Run("isdir rejects null bytes", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()

		for _, p := range pathsWithNullByte {
			l.SetTop(0)
			ud := l.NewUserData()
			ud.Value = f
			l.Push(ud)
			l.Push(lua.LString(p))

			nret := fsIsdir(l)
			require.Equal(t, 2, nret)
			assert.Equal(t, lua.LFalse, l.Get(-2), "isdir should return false for null byte path")
			luaErr := requireLuaError(t, l.Get(-1))
			assert.Equal(t, string(lua.Invalid), string(luaErr.Kind()))
		}
	})

	t.Run("readfile rejects null bytes", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()

		for _, p := range pathsWithNullByte {
			l.SetTop(0)
			ud := l.NewUserData()
			ud.Value = f
			l.Push(ud)
			l.Push(lua.LString(p))

			nret := fsReadfile(l)
			require.Equal(t, 2, nret)
			assert.Equal(t, lua.LNil, l.Get(-2), "readfile should return nil for null byte path")
			luaErr := requireLuaError(t, l.Get(-1))
			assert.Equal(t, string(lua.Invalid), string(luaErr.Kind()))
		}
	})

	t.Run("writefile rejects null bytes", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		l.SetContext(t.Context())

		for _, p := range pathsWithNullByte {
			l.SetTop(0)
			ud := l.NewUserData()
			ud.Value = f
			l.Push(ud)
			l.Push(lua.LString(p))
			l.Push(lua.LString("content"))

			nret := fsWritefile(l)
			require.Equal(t, 2, nret)
			assert.Equal(t, lua.LFalse, l.Get(-2), "writefile should return false for null byte path")
			luaErr := requireLuaError(t, l.Get(-1))
			assert.Equal(t, string(lua.Invalid), string(luaErr.Kind()))
		}
	})

	t.Run("chdir rejects null bytes", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()

		for _, p := range pathsWithNullByte {
			l.SetTop(0)
			ud := l.NewUserData()
			ud.Value = f
			l.Push(ud)
			l.Push(lua.LString(p))

			nret := fsChdir(l)
			require.Equal(t, 2, nret)
			assert.Equal(t, lua.LFalse, l.Get(-2), "chdir should return false for null byte path")
			luaErr := requireLuaError(t, l.Get(-1))
			assert.Equal(t, string(lua.Invalid), string(luaErr.Kind()))
		}
	})
}
