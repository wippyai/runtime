package fs

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/service/fs/directory"
	lua "github.com/yuin/gopher-lua"
)

func createTestFS(t *testing.T) (*FS, func()) {
	tmpDir := t.TempDir()
	fsys, err := directory.NewDirectoryFS(tmpDir, 0755, false)
	require.NoError(t, err)
	return NewFS(fsys, ""), func() { fsys.Close() }
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
			checkKind:   string(lua.KindInvalid),
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
			checkKind:   string(lua.KindNotFound),
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
			checkKind:   string(lua.KindInvalid),
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
			checkKind:   string(lua.KindInvalid),
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
			checkKind:   string(lua.KindNotFound),
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
			checkKind:   string(lua.KindInvalid),
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
			checkKind:   string(lua.KindNotFound),
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
			checkKind:   string(lua.KindInvalid),
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
			checkKind:   string(lua.KindInvalid),
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
			checkKind:   string(lua.KindInvalid),
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
			checkKind:   string(lua.KindInvalid),
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
			checkKind:   string(lua.KindNotFound),
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
			checkKind:   string(lua.KindInvalid),
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
			checkKind:   string(lua.KindInvalid),
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
			checkKind:   string(lua.KindInvalid),
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
				errVal := l.Get(-1)
				luaErr, ok := errVal.(*lua.Error)
				require.True(t, ok, "second return should be lua.Error, got %T", errVal)
				assert.Equal(t, tt.checkKind, string(luaErr.Kind()), "error kind should match")
			}
		})
	}
}

func TestFSMkdirAlreadyExists(t *testing.T) {
	tmpDir := t.TempDir()
	fsys, err := directory.NewDirectoryFS(tmpDir, 0755, false)
	require.NoError(t, err)
	defer fsys.Close()
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

	errVal := l.Get(-1)
	luaErr, ok := errVal.(*lua.Error)
	require.True(t, ok, "error should be lua.Error")
	assert.Equal(t, string(lua.KindAlreadyExists), string(luaErr.Kind()))
}

func TestFSChdirNotDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	fsys, err := directory.NewDirectoryFS(tmpDir, 0755, false)
	require.NoError(t, err)
	defer fsys.Close()
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

	errVal := l.Get(-1)
	luaErr, ok := errVal.(*lua.Error)
	require.True(t, ok, "error should be lua.Error")
	assert.Equal(t, string(lua.KindInvalid), string(luaErr.Kind()))
}

func TestFSReaddirNotDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	fsys, err := directory.NewDirectoryFS(tmpDir, 0755, false)
	require.NoError(t, err)
	defer fsys.Close()
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

	errVal := l.Get(-1)
	luaErr, ok := errVal.(*lua.Error)
	require.True(t, ok, "error should be lua.Error")
	assert.Equal(t, string(lua.KindInvalid), string(luaErr.Kind()))
}

func TestFSRemoveNonEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	fsys, err := directory.NewDirectoryFS(tmpDir, 0755, false)
	require.NoError(t, err)
	defer fsys.Close()
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

	errVal := l.Get(-1)
	luaErr, ok := errVal.(*lua.Error)
	require.True(t, ok, "error should be lua.Error")
	assert.Equal(t, string(lua.KindInvalid), string(luaErr.Kind()))
}
