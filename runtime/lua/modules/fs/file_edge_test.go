// SPDX-License-Identifier: MPL-2.0

package fs

import (
	"errors"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/runtime/resource"
	"github.com/wippyai/runtime/service/fs/directory"
)

// --- File struct methods ---

func openTestFile(t *testing.T, content string) (*File, func()) {
	t.Helper()
	dir := t.TempDir()
	fsys, err := directory.NewFS(dir, 0755, false)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(dir+"/test.txt", []byte(content), 0644))

	file, err := fsys.OpenFile("test.txt", os.O_RDWR, 0644)
	require.NoError(t, err)

	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	store := resource.NewStore()
	require.NoError(t, resource.SetStore(ctx, store))

	f := NewFileWithCleanup(ctx, file)
	return f, func() {
		_ = f.Close()
		_ = store.Close()
		_ = fsys.Close()
	}
}

func TestFile_Read_Success(t *testing.T) {
	f, cleanup := openTestFile(t, "hello world")
	defer cleanup()

	buf := make([]byte, 5)
	n, err := f.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, "hello", string(buf[:n]))
}

func TestFile_Read_EOF(t *testing.T) {
	f, cleanup := openTestFile(t, "hi")
	defer cleanup()

	buf := make([]byte, 100)
	n, err := f.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	_, err = f.Read(buf)
	assert.ErrorIs(t, err, io.EOF)
}

func TestFile_Read_Closed(t *testing.T) {
	f, cleanup := openTestFile(t, "data")
	defer cleanup()

	require.NoError(t, f.Close())
	_, err := f.Read(make([]byte, 10))
	assert.Equal(t, ErrFileAlreadyClosed, err)
}

func TestFile_Write_Success(t *testing.T) {
	dir := t.TempDir()
	fsys, err := directory.NewFS(dir, 0755, false)
	require.NoError(t, err)
	defer func() { _ = fsys.Close() }()

	file, err := fsys.OpenFile("out.txt", os.O_WRONLY|os.O_CREATE, 0644)
	require.NoError(t, err)

	f := &File{file: file}
	n, err := f.Write([]byte("written"))
	require.NoError(t, err)
	assert.Equal(t, 7, n)
	require.NoError(t, f.Close())

	data, err := os.ReadFile(dir + "/out.txt")
	require.NoError(t, err)
	assert.Equal(t, "written", string(data))
}

func TestFile_Write_Closed(t *testing.T) {
	f, cleanup := openTestFile(t, "")
	defer cleanup()

	require.NoError(t, f.Close())
	_, err := f.Write([]byte("data"))
	assert.Equal(t, ErrFileAlreadyClosed, err)
}

func TestFile_Seek_Success(t *testing.T) {
	f, cleanup := openTestFile(t, "abcdefghij")
	defer cleanup()

	pos, err := f.Seek(5, io.SeekStart)
	require.NoError(t, err)
	assert.Equal(t, int64(5), pos)

	buf := make([]byte, 5)
	n, err := f.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "fghij", string(buf[:n]))
}

func TestFile_Seek_Closed(t *testing.T) {
	f, cleanup := openTestFile(t, "data")
	defer cleanup()

	require.NoError(t, f.Close())
	_, err := f.Seek(0, io.SeekStart)
	assert.Equal(t, ErrFileAlreadyClosed, err)
}

func TestFile_Stat_Success(t *testing.T) {
	f, cleanup := openTestFile(t, "twelve chars")
	defer cleanup()

	info, err := f.Stat()
	require.NoError(t, err)
	assert.Equal(t, "test.txt", info.Name())
	assert.Equal(t, int64(12), info.Size())
}

func TestFile_Stat_Closed(t *testing.T) {
	f, cleanup := openTestFile(t, "data")
	defer cleanup()

	require.NoError(t, f.Close())
	_, err := f.Stat()
	assert.Equal(t, ErrFileAlreadyClosed, err)
}

func TestFile_Sync_Success(t *testing.T) {
	f, cleanup := openTestFile(t, "data")
	defer cleanup()

	err := f.Sync()
	assert.NoError(t, err)
}

func TestFile_Sync_Closed(t *testing.T) {
	f, cleanup := openTestFile(t, "data")
	defer cleanup()

	require.NoError(t, f.Close())
	err := f.Sync()
	assert.Equal(t, ErrFileAlreadyClosed, err)
}

func TestFile_Close_Idempotent(t *testing.T) {
	f, cleanup := openTestFile(t, "data")
	defer cleanup()

	require.NoError(t, f.Close())
	err := f.Close()
	assert.NoError(t, err)
}

func TestFile_Close_CancelsCleanup(t *testing.T) {
	dir := t.TempDir()
	fsys, err := directory.NewFS(dir, 0755, false)
	require.NoError(t, err)
	defer func() { _ = fsys.Close() }()

	require.NoError(t, os.WriteFile(dir+"/test.txt", []byte("data"), 0644))
	file, err := fsys.OpenFile("test.txt", os.O_RDONLY, 0)
	require.NoError(t, err)

	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	store := resource.NewStore()
	require.NoError(t, resource.SetStore(ctx, store))

	f := NewFileWithCleanup(ctx, file)
	assert.NotNil(t, f.cancelCleanup)

	require.NoError(t, f.Close())
	assert.Nil(t, f.cancelCleanup)

	// store cleanup should not error (cleanup was cancelled)
	assert.NoError(t, store.Close())
}

func TestFile_Read_FsClosed(t *testing.T) {
	dir := t.TempDir()
	fsys, err := directory.NewFS(dir, 0755, false)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(dir+"/test.txt", []byte("data"), 0644))
	file, err := fsys.OpenFile("test.txt", os.O_RDONLY, 0)
	require.NoError(t, err)

	f := &File{file: file}
	_ = file.Close()

	_, err = f.Read(make([]byte, 10))
	assert.ErrorIs(t, err, ErrFileAlreadyClosed)
	_ = fsys.Close()
}

// --- Lua file bindings ---

func pushTestFile(t *testing.T, l *lua.LState, content string) (*File, func()) {
	t.Helper()
	dir := t.TempDir()
	fsys, err := directory.NewFS(dir, 0755, false)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(dir+"/test.txt", []byte(content), 0644))

	file, err := fsys.OpenFile("test.txt", os.O_RDWR, 0644)
	require.NoError(t, err)

	f := &File{file: file}
	ud := l.NewUserData()
	ud.Value = f
	ud.Metatable = fileMetatable
	l.Push(ud)

	return f, func() {
		_ = f.Close()
		_ = fsys.Close()
	}
}

func TestFileRead_Lua_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	_, cleanup := pushTestFile(t, l, "hello world")
	defer cleanup()

	l.Push(lua.LNumber(5))
	nret := fileRead(l)
	require.Equal(t, 2, nret)

	assert.Equal(t, lua.LString("hello"), l.Get(-2))
	assert.Equal(t, lua.LNil, l.Get(-1))
}

func TestFileRead_Lua_EOF(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	_, cleanup := pushTestFile(t, l, "")
	defer cleanup()

	nret := fileRead(l)
	require.Equal(t, 2, nret)

	assert.Equal(t, lua.LNil, l.Get(-2))
	luaErr := requireLuaError(t, l.Get(-1))
	assert.Equal(t, string(lua.NotFound), string(luaErr.Kind()))
}

func TestFileRead_Lua_NegativeSize(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	_, cleanup := pushTestFile(t, l, "data")
	defer cleanup()

	l.Push(lua.LNumber(-1))
	nret := fileRead(l)
	require.Equal(t, 2, nret)

	assert.Equal(t, lua.LNil, l.Get(-2))
	luaErr := requireLuaError(t, l.Get(-1))
	assert.Equal(t, string(lua.Invalid), string(luaErr.Kind()))
}

func TestFileWrite_Lua_Success(t *testing.T) {
	dir := t.TempDir()
	fsys, err := directory.NewFS(dir, 0755, false)
	require.NoError(t, err)
	defer func() { _ = fsys.Close() }()

	file, err := fsys.OpenFile("out.txt", os.O_WRONLY|os.O_CREATE, 0644)
	require.NoError(t, err)

	l := lua.NewState()
	defer l.Close()

	f := &File{file: file}
	ud := l.NewUserData()
	ud.Value = f
	ud.Metatable = fileMetatable
	l.Push(ud)
	l.Push(lua.LString("written via lua"))

	nret := fileWrite(l)
	require.Equal(t, 2, nret)

	assert.Equal(t, lua.LTrue, l.Get(-2))
	assert.Equal(t, lua.LNil, l.Get(-1))

	_ = f.Close()

	data, err := os.ReadFile(dir + "/out.txt")
	require.NoError(t, err)
	assert.Equal(t, "written via lua", string(data))
}

func TestFileWrite_Lua_EmptyData(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	_, cleanup := pushTestFile(t, l, "")
	defer cleanup()

	l.Push(lua.LString(""))
	nret := fileWrite(l)
	require.Equal(t, 2, nret)

	assert.Equal(t, lua.LFalse, l.Get(-2))
	luaErr := requireLuaError(t, l.Get(-1))
	assert.Equal(t, string(lua.Invalid), string(luaErr.Kind()))
}

func TestFileSeek_Lua_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	_, cleanup := pushTestFile(t, l, "0123456789")
	defer cleanup()

	l.Push(lua.LString("set"))
	l.Push(lua.LNumber(5))
	nret := fileSeek(l)
	require.Equal(t, 2, nret)

	assert.Equal(t, lua.LNumber(5), l.Get(-2))
	assert.Equal(t, lua.LNil, l.Get(-1))
}

func TestFileSeek_Lua_InvalidWhence(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	_, cleanup := pushTestFile(t, l, "data")
	defer cleanup()

	l.Push(lua.LString("invalid"))
	l.Push(lua.LNumber(0))
	nret := fileSeek(l)
	require.Equal(t, 2, nret)

	assert.Equal(t, lua.LNil, l.Get(-2))
	luaErr := requireLuaError(t, l.Get(-1))
	assert.Equal(t, string(lua.Invalid), string(luaErr.Kind()))
}

func TestFileSeek_Lua_AllWhenceTypes(t *testing.T) {
	for _, whence := range []string{"set", "cur", "end"} {
		t.Run(whence, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			_, cleanup := pushTestFile(t, l, "0123456789")
			defer cleanup()

			l.Push(lua.LString(whence))
			l.Push(lua.LNumber(0))
			nret := fileSeek(l)
			require.Equal(t, 2, nret)
			assert.Equal(t, lua.LNil, l.Get(-1))
		})
	}
}

func TestFileClose_Lua_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	_, cleanup := pushTestFile(t, l, "data")
	defer cleanup()

	nret := fileClose(l)
	require.Equal(t, 2, nret)

	assert.Equal(t, lua.LTrue, l.Get(-2))
	assert.Equal(t, lua.LNil, l.Get(-1))
}

func TestFileStat_Lua_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	_, cleanup := pushTestFile(t, l, "twelve chars")
	defer cleanup()

	nret := fileStat(l)
	require.Equal(t, 2, nret)

	tbl, ok := l.Get(-2).(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, lua.LString("test.txt"), tbl.RawGetString("name"))
	assert.Equal(t, lua.LNumber(12), tbl.RawGetString("size"))
	assert.Equal(t, lua.LFalse, tbl.RawGetString("is_dir"))
	assert.Equal(t, lua.LString("file"), tbl.RawGetString("type"))
	assert.Equal(t, lua.LNil, l.Get(-1))
}

func TestFileSync_Lua_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	_, cleanup := pushTestFile(t, l, "data")
	defer cleanup()

	nret := fileSync(l)
	require.Equal(t, 2, nret)

	assert.Equal(t, lua.LTrue, l.Get(-2))
	assert.Equal(t, lua.LNil, l.Get(-1))
}

func TestFileToString_Open(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	_, cleanup := pushTestFile(t, l, "data")
	defer cleanup()

	nret := fileToString(l)
	require.Equal(t, 1, nret)
	assert.Equal(t, lua.LString("fs.File{}"), l.Get(-1))
}

func TestFileToString_Closed(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	f, cleanup := pushTestFile(t, l, "data")
	defer cleanup()

	_ = f.Close()

	nret := fileToString(l)
	require.Equal(t, 1, nret)
	assert.Equal(t, lua.LString("fs.File{closed}"), l.Get(-1))
}

// --- FileScanner ---

func TestFileScanner_Lines(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	_, cleanup := pushTestFile(t, l, "line1\nline2\nline3")
	defer cleanup()

	nret := fileScanner(l)
	require.Equal(t, 2, nret)
	assert.Equal(t, lua.LNil, l.Get(-1))

	scannerUD := l.Get(-2).(*lua.LUserData)
	scanner := scannerUD.Value.(*FileScanner)

	var lines []string
	for scanner.scanner.Scan() {
		lines = append(lines, scanner.scanner.Text())
	}
	assert.Equal(t, []string{"line1", "line2", "line3"}, lines)
}

func TestFileScanner_Words(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	_, cleanup := pushTestFile(t, l, "hello world foo")
	defer cleanup()

	l.Push(lua.LString("words"))
	nret := fileScanner(l)
	require.Equal(t, 2, nret)
	assert.Equal(t, lua.LNil, l.Get(-1))

	scannerUD := l.Get(-2).(*lua.LUserData)
	scanner := scannerUD.Value.(*FileScanner)

	var words []string
	for scanner.scanner.Scan() {
		words = append(words, scanner.scanner.Text())
	}
	assert.Equal(t, []string{"hello", "world", "foo"}, words)
}

func TestFileScanner_InvalidSplitType(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	_, cleanup := pushTestFile(t, l, "data")
	defer cleanup()

	l.Push(lua.LString("invalid"))
	nret := fileScanner(l)
	require.Equal(t, 2, nret)

	assert.Equal(t, lua.LNil, l.Get(-2))
	luaErr := requireLuaError(t, l.Get(-1))
	assert.Equal(t, string(lua.Invalid), string(luaErr.Kind()))
}

func TestFileScanner_ClosedFile(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	f, cleanup := pushTestFile(t, l, "data")
	defer cleanup()

	_ = f.Close()

	nret := fileScanner(l)
	require.Equal(t, 2, nret)

	assert.Equal(t, lua.LNil, l.Get(-2))
	luaErr := requireLuaError(t, l.Get(-1))
	assert.Equal(t, string(lua.Invalid), string(luaErr.Kind()))
}

func TestFileScanner_Lua_ScanTextErr(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	_, cleanup := pushTestFile(t, l, "line1\nline2")
	defer cleanup()

	nret := fileScanner(l)
	require.Equal(t, 2, nret)
	assert.Equal(t, lua.LNil, l.Get(-1))

	scannerUD := l.Get(-2).(*lua.LUserData)

	// test scan via Lua binding
	l.SetTop(0)
	l.Push(scannerUD)
	nret = fileScannerScan(l)
	require.Equal(t, 1, nret)
	assert.Equal(t, lua.LTrue, l.Get(-1))

	// test text
	l.SetTop(0)
	l.Push(scannerUD)
	nret = fileScannerText(l)
	require.Equal(t, 1, nret)
	assert.Equal(t, lua.LString("line1"), l.Get(-1))

	// test err (should be nil)
	l.SetTop(0)
	l.Push(scannerUD)
	nret = fileScannerErr(l)
	require.Equal(t, 1, nret)
	assert.Equal(t, lua.LNil, l.Get(-1))
}

// --- Error constructors ---

func TestErrorConstructors(t *testing.T) {
	cause := errors.New("underlying error")

	tests := []struct {
		name    string
		errFn   func(error) error
		msgPart string
	}{
		{"read", func(e error) error { return NewReadError(e) }, "read failed"},
		{"write", func(e error) error { return NewWriteError(e) }, "write failed"},
		{"seek", func(e error) error { return NewSeekError(e) }, "seek failed"},
		{"stat", func(e error) error { return NewStatError(e) }, "stat failed"},
		{"sync", func(e error) error { return NewSyncError(e) }, "sync failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.errFn(cause)
			assert.Contains(t, err.Error(), tt.msgPart)
		})
	}
}

func TestSentinelErrors(t *testing.T) {
	assert.Contains(t, ErrFileAlreadyClosed.Error(), "file already closed")
	assert.Contains(t, ErrNullBytePath.Error(), "null byte")
	assert.Contains(t, ErrPathTraversal.Error(), "traversal")
}

// --- FS path resolution ---

func TestFS_ResolvePath_Traversal(t *testing.T) {
	dir := t.TempDir()
	fsys, err := directory.NewFS(dir, 0755, false)
	require.NoError(t, err)
	defer func() { _ = fsys.Close() }()
	f := NewFS(fsys, "subdir")

	tests := []struct {
		err  error
		name string
		path string
	}{
		{name: "parent escape", path: "../../etc/passwd", err: ErrPathTraversal},
		{name: "null byte", path: "test\x00file", err: ErrNullBytePath},
		{name: "absolute ok", path: "/test.txt", err: nil},
		{name: "relative ok", path: "test.txt", err: nil},
		{name: "empty uses cwd", path: "", err: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := f.resolvePath(tt.path)
			if tt.err != nil {
				assert.Equal(t, tt.err, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFS_ResolvePath_EmptyCwd(t *testing.T) {
	f := NewFS(nil, "")
	assert.Equal(t, ".", f.cwd)

	resolved, err := f.resolvePath("")
	require.NoError(t, err)
	assert.Equal(t, ".", resolved)
}

func TestFS_ResolvePath_AbsoluteRoot(t *testing.T) {
	f := NewFS(nil, ".")
	resolved, err := f.resolvePath("/")
	require.NoError(t, err)
	assert.Equal(t, ".", resolved)
}

// --- fsToString ---

func TestFsToString(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	f := NewFS(nil, "mydir")
	ud := l.NewUserData()
	ud.Value = f
	l.Push(ud)

	nret := fsToString(l)
	require.Equal(t, 1, nret)
	assert.Equal(t, lua.LString("fs.FS{cwd=mydir}"), l.Get(-1))
}

// --- fsGet edge cases ---

func TestFsGet_EmptyName(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	l.SetContext(ctxapi.NewRootContext())
	l.Push(lua.LString(""))

	nret := fsGet(l)
	require.Equal(t, 2, nret)

	assert.Equal(t, lua.LNil, l.Get(-2))
	luaErr := requireLuaError(t, l.Get(-1))
	assert.Equal(t, string(lua.Invalid), string(luaErr.Kind()))
}

// --- fsPwd ---

func TestFsPwd_Root(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	f := NewFS(nil, ".")
	ud := l.NewUserData()
	ud.Value = f
	l.Push(ud)

	nret := fsPwd(l)
	require.Equal(t, 2, nret)
	assert.Equal(t, lua.LString("/"), l.Get(-2))
	assert.Equal(t, lua.LNil, l.Get(-1))
}

func TestFsPwd_Subdir(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	f := NewFS(nil, "docs/api")
	ud := l.NewUserData()
	ud.Value = f
	l.Push(ud)

	nret := fsPwd(l)
	require.Equal(t, 2, nret)
	assert.Equal(t, lua.LString("/docs/api"), l.Get(-2))
}

// --- pushFileInfo ---

func TestPushFileInfo_Directory(t *testing.T) {
	dir := t.TempDir()
	info, err := os.Stat(dir)
	require.NoError(t, err)

	l := lua.NewState()
	defer l.Close()

	tbl := pushFileInfo(l, info)
	assert.Equal(t, lua.LTrue, tbl.RawGetString("is_dir"))
	assert.Equal(t, lua.LString("directory"), tbl.RawGetString("type"))
}

func TestPushFileInfo_File(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(dir+"/test.txt", []byte("data"), 0644))
	info, err := os.Stat(dir + "/test.txt")
	require.NoError(t, err)

	l := lua.NewState()
	defer l.Close()

	tbl := pushFileInfo(l, info)
	assert.Equal(t, lua.LFalse, tbl.RawGetString("is_dir"))
	assert.Equal(t, lua.LString("file"), tbl.RawGetString("type"))
	assert.Equal(t, lua.LNumber(4), tbl.RawGetString("size"))
	assert.Equal(t, lua.LString("test.txt"), tbl.RawGetString("name"))
}

// --- dirIteratorNext ---

func TestDirIteratorNext_Empty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	it := &dirIterator{entries: nil, index: 0}
	ud := l.NewUserData()
	ud.Value = it
	l.Push(ud)

	nret := dirIteratorNext(l)
	require.Equal(t, 1, nret)
	assert.Equal(t, lua.LNil, l.Get(-1))
}

func TestDirIteratorNext_WrongType(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ud := l.NewUserData()
	ud.Value = "not an iterator"
	l.Push(ud)

	nret := dirIteratorNext(l)
	require.Equal(t, 1, nret)
	assert.Equal(t, lua.LNil, l.Get(-1))
}

// --- buildModule ---

func TestBuildModule(t *testing.T) {
	mod, yields := buildModule()
	require.NotNil(t, mod)
	assert.Nil(t, yields)

	// type constants
	typeTbl := mod.RawGetString("type").(*lua.LTable)
	assert.Equal(t, lua.LString("file"), typeTbl.RawGetString("FILE"))
	assert.Equal(t, lua.LString("directory"), typeTbl.RawGetString("DIR"))

	// seek constants
	seekTbl := mod.RawGetString("seek").(*lua.LTable)
	assert.Equal(t, lua.LString("set"), seekTbl.RawGetString("SET"))
	assert.Equal(t, lua.LString("cur"), seekTbl.RawGetString("CUR"))
	assert.Equal(t, lua.LString("end"), seekTbl.RawGetString("END"))

	// get function
	assert.NotNil(t, mod.RawGetString("get"))
}

// --- File operations on closed file via Lua ---

func TestFileLuaOps_ClosedFile(t *testing.T) {
	tests := []struct {
		fn   func(l *lua.LState) int
		push func(l *lua.LState)
		name string
	}{
		{name: "read", fn: fileRead, push: nil},
		{name: "write", fn: fileWrite, push: func(l *lua.LState) { l.Push(lua.LString("data")) }},
		{name: "seek", fn: fileSeek, push: func(l *lua.LState) {
			l.Push(lua.LString("set"))
			l.Push(lua.LNumber(0))
		}},
		{name: "stat", fn: fileStat, push: nil},
		{name: "sync", fn: fileSync, push: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			f, cleanup := pushTestFile(t, l, "data")
			defer cleanup()
			_ = f.Close()

			if tt.push != nil {
				tt.push(l)
			}

			nret := tt.fn(l)
			require.Equal(t, 2, nret)

			errVal := l.Get(-1)
			luaErr := requireLuaError(t, errVal)
			assert.Equal(t, string(lua.Internal), string(luaErr.Kind()))
		})
	}
}

// --- fs.File wraps fs.ErrClosed ---

func TestFile_Read_FsErrClosed(t *testing.T) {
	dir := t.TempDir()
	fsys, err := directory.NewFS(dir, 0755, false)
	require.NoError(t, err)
	defer func() { _ = fsys.Close() }()

	require.NoError(t, os.WriteFile(dir+"/test.txt", []byte("data"), 0644))
	rawFile, err := fsys.OpenFile("test.txt", os.O_RDONLY, 0)
	require.NoError(t, err)

	f := &File{file: rawFile}
	// close underlying file but not our wrapper
	_ = rawFile.Close()

	_, err = f.Read(make([]byte, 10))
	assert.ErrorIs(t, err, ErrFileAlreadyClosed)
}

// --- FileScanner bytes and runes split modes ---

func TestFileScanner_Bytes(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	_, cleanup := pushTestFile(t, l, "abc")
	defer cleanup()

	l.Push(lua.LString("bytes"))
	nret := fileScanner(l)
	require.Equal(t, 2, nret)
	assert.Equal(t, lua.LNil, l.Get(-1))

	scannerUD := l.Get(-2).(*lua.LUserData)
	scanner := scannerUD.Value.(*FileScanner)

	var chars []string
	for scanner.scanner.Scan() {
		chars = append(chars, scanner.scanner.Text())
	}
	assert.Equal(t, []string{"a", "b", "c"}, chars)
}

func TestFileScanner_Runes(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	_, cleanup := pushTestFile(t, l, "AB")
	defer cleanup()

	l.Push(lua.LString("runes"))
	nret := fileScanner(l)
	require.Equal(t, 2, nret)
	assert.Equal(t, lua.LNil, l.Get(-1))

	scannerUD := l.Get(-2).(*lua.LUserData)
	scanner := scannerUD.Value.(*FileScanner)

	var runes []string
	for scanner.scanner.Scan() {
		runes = append(runes, scanner.scanner.Text())
	}
	assert.Equal(t, []string{"A", "B"}, runes)
}

// --- NewFileWithCleanup without store ---

func TestNewFileWithCleanup_NoStore(t *testing.T) {
	dir := t.TempDir()
	fsys, err := directory.NewFS(dir, 0755, false)
	require.NoError(t, err)
	defer func() { _ = fsys.Close() }()

	require.NoError(t, os.WriteFile(dir+"/test.txt", []byte("data"), 0644))
	rawFile, err := fsys.OpenFile("test.txt", os.O_RDONLY, 0)
	require.NoError(t, err)

	// context without resource store
	ctx := ctxapi.NewRootContext()
	f := NewFileWithCleanup(ctx, rawFile)

	assert.Nil(t, f.cancelCleanup)

	buf := make([]byte, 10)
	n, err := f.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "data", string(buf[:n]))

	require.NoError(t, f.Close())
}

// --- FS happy path operations via Lua ---

func TestFsOpen_WriteReadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	fsys, err := directory.NewFS(dir, 0755, false)
	require.NoError(t, err)
	defer func() { _ = fsys.Close() }()
	f := NewFS(fsys, "")

	l := lua.NewState()
	defer l.Close()
	l.SetContext(ctxapi.NewRootContext())

	// open for write
	ud := l.NewUserData()
	ud.Value = f
	l.Push(ud)
	l.Push(lua.LString("/roundtrip.txt"))
	l.Push(lua.LString("w"))
	nret := fsOpen(l)
	require.Equal(t, 2, nret)
	assert.Equal(t, lua.LNil, l.Get(-1))

	fileUD := l.Get(-2).(*lua.LUserData)
	file := fileUD.Value.(*File)

	// write data
	l.SetTop(0)
	l.Push(fileUD)
	l.Push(lua.LString("roundtrip data"))
	nret = fileWrite(l)
	require.Equal(t, 2, nret)
	assert.Equal(t, lua.LTrue, l.Get(-2))

	_ = file.Close()

	// read it back via readfile
	l.SetTop(0)
	ud2 := l.NewUserData()
	ud2.Value = f
	l.Push(ud2)
	l.Push(lua.LString("/roundtrip.txt"))
	nret = fsReadfile(l)
	require.Equal(t, 2, nret)
	assert.Equal(t, lua.LString("roundtrip data"), l.Get(-2))
	assert.Equal(t, lua.LNil, l.Get(-1))
}

// --- ModuleTypes ---

func TestModuleTypes(t *testing.T) {
	m := ModuleTypes()
	require.NotNil(t, m)

	_, ok := m.LookupType("FS")
	assert.True(t, ok)
	_, ok = m.LookupType("File")
	assert.True(t, ok)
	_, ok = m.LookupType("FileInfo")
	assert.True(t, ok)
}

// --- Module definition ---

func TestModuleDef(t *testing.T) {
	assert.Equal(t, "fs", Module.Name)
	assert.NotNil(t, Module.Build)
	assert.NotNil(t, Module.Types)
}
