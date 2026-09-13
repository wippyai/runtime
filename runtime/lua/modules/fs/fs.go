// SPDX-License-Identifier: MPL-2.0

package fs

import (
	"errors"
	"fmt"
	"io"
	iofs "io/fs"
	"os"
	"path"
	"strings"

	lua "github.com/wippyai/go-lua"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/runtime/resource"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

type FS struct {
	fs  fsapi.FS
	cwd string
}

const maxAtomicWriteBytes = 8 << 20

func wrapFilesystemError(l *lua.LState, err error, message string, fallback lua.Kind) *lua.Error {
	kind := fallback
	if errors.Is(err, fsapi.ErrReadOnly) ||
		errors.Is(err, fsapi.ErrPermissionDenied) ||
		errors.Is(err, iofs.ErrPermission) {
		kind = lua.PermissionDenied
	}
	return lua.WrapErrorWithLua(l, err, message).WithKind(kind)
}

// dirIterator is a userdata-based iterator for directory entries
type dirIterator struct {
	entries []os.DirEntry
	index   int
}

func NewFS(fs fsapi.FS, cwd string) *FS {
	if cwd == "" {
		cwd = "."
	}
	return &FS{fs: fs, cwd: cwd}
}

func (f *FS) resolvePath(p string) (string, error) {
	if strings.ContainsRune(p, 0) {
		return "", ErrNullBytePath
	}
	var res string
	switch {
	case p == "":
		res = f.cwd
	case p[0] == '/':
		res = strings.TrimLeft(p, "/")
	default:
		res = path.Join(f.cwd, p)
	}
	if res == "" {
		return ".", nil
	}

	// The underlying fsapi.FS follows the io/fs contract, where paths are always
	// forward-slash regardless of OS, so clean with path (not filepath).
	res = path.Clean(res)
	if res == ".." || strings.HasPrefix(res, "../") {
		return "", ErrPathTraversal
	}

	return res, nil
}

var fsMethods = map[string]lua.LGoFunc{
	"chdir":            fsChdir,
	"pwd":              fsPwd,
	"open":             fsOpen,
	"stat":             fsStat,
	"mkdir":            fsMkdir,
	"remove":           fsRemove,
	"readdir":          fsReaddir,
	"exists":           fsExists,
	"isdir":            fsIsdir,
	"readfile":         fsReadfile,
	"read_file":        fsReadfile,
	"writefile":        fsWritefile,
	"write_file":       fsWritefile,
	"writefile_atomic": fsWritefileAtomic,
}

func fsChdir(l *lua.LState) int {
	fs := checkFS(l, 1)
	if fs == nil {
		return 0
	}
	path := l.CheckString(2)
	if path == "" {
		l.Push(lua.LFalse)
		l.Push(lua.NewLuaError(l, "path required").WithKind(lua.Invalid))
		return 2
	}
	target, err := fs.resolvePath(path)
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.WrapErrorWithLua(l, err, "invalid path").WithKind(lua.Invalid))
		return 2
	}
	info, err := fs.fs.Stat(target)
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(wrapFilesystemError(l, err, "failed to stat directory", lua.NotFound))
		return 2
	}
	if !info.IsDir() {
		l.Push(lua.LFalse)
		l.Push(lua.NewLuaError(l, "not a directory: "+path).WithKind(lua.Invalid))
		return 2
	}
	fs.cwd = target
	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func fsPwd(l *lua.LState) int {
	fs := checkFS(l, 1)
	if fs == nil {
		return 0
	}
	if fs.cwd == "" || fs.cwd == "." {
		l.Push(lua.LString("/"))
	} else {
		l.Push(lua.LString("/" + fs.cwd))
	}
	l.Push(lua.LNil)
	return 2
}

func fsOpen(l *lua.LState) int {
	fs := checkFS(l, 1)
	if fs == nil {
		return 0
	}
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "no context").WithKind(lua.Internal))
		return 2
	}

	path := l.CheckString(2)
	if path == "" {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "path required").WithKind(lua.Invalid))
		return 2
	}
	mode := l.CheckString(3)
	var flag int
	switch mode {
	case "r":
		flag = os.O_RDONLY
	case "w":
		flag = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	case "wx":
		flag = os.O_WRONLY | os.O_CREATE | os.O_TRUNC | os.O_EXCL
	case "a":
		flag = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	default:
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "invalid mode: must be 'r', 'w', 'wx' or 'a'").WithKind(lua.Invalid))
		return 2
	}

	resolved, err := fs.resolvePath(path)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "invalid path").WithKind(lua.Invalid))
		return 2
	}
	file, err := fs.fs.OpenFile(resolved, flag, 0644)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(wrapFilesystemError(l, err, "failed to open file", lua.NotFound))
		return 2
	}

	value.PushUserData(l, NewFileWithCleanup(ctx, file), fileMetatable)
	l.Push(lua.LNil)
	return 2
}

func fsStat(l *lua.LState) int {
	fs := checkFS(l, 1)
	if fs == nil {
		return 0
	}
	path := l.CheckString(2)
	if path == "" {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "path required").WithKind(lua.Invalid))
		return 2
	}
	resolved, err := fs.resolvePath(path)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "invalid path").WithKind(lua.Invalid))
		return 2
	}
	info, err := fs.fs.Stat(resolved)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(wrapFilesystemError(l, err, "stat failed", lua.NotFound))
		return 2
	}
	l.Push(pushFileInfo(l, info))
	l.Push(lua.LNil)
	return 2
}

func fsMkdir(l *lua.LState) int {
	fs := checkFS(l, 1)
	if fs == nil {
		return 0
	}
	path := l.CheckString(2)
	if path == "" {
		l.Push(lua.LFalse)
		l.Push(lua.NewLuaError(l, "path required").WithKind(lua.Invalid))
		return 2
	}
	resolved, err := fs.resolvePath(path)
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.WrapErrorWithLua(l, err, "invalid path").WithKind(lua.Invalid))
		return 2
	}
	_, err = fs.fs.Stat(resolved)
	if err == nil {
		l.Push(lua.LFalse)
		l.Push(lua.NewLuaError(l, "path already exists: "+path).WithKind(lua.AlreadyExists))
		return 2
	}
	if err := fs.fs.Mkdir(resolved, 0755); err != nil {
		l.Push(lua.LFalse)
		l.Push(wrapFilesystemError(l, err, "mkdir failed", lua.Internal))
		return 2
	}
	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func fsRemove(l *lua.LState) int {
	fs := checkFS(l, 1)
	if fs == nil {
		return 0
	}
	path := l.CheckString(2)
	if path == "" {
		l.Push(lua.LFalse)
		l.Push(lua.NewLuaError(l, "path required").WithKind(lua.Invalid))
		return 2
	}
	resolved, err := fs.resolvePath(path)
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.WrapErrorWithLua(l, err, "invalid path").WithKind(lua.Invalid))
		return 2
	}
	info, err := fs.fs.Stat(resolved)
	if err == nil && info.IsDir() {
		entries, err := fs.fs.ReadDir(resolved)
		if err == nil && len(entries) > 0 {
			l.Push(lua.LFalse)
			l.Push(lua.NewLuaError(l, "directory not empty: "+path).WithKind(lua.Invalid))
			return 2
		}
	}
	if err := fs.fs.Remove(resolved); err != nil {
		l.Push(lua.LFalse)
		l.Push(wrapFilesystemError(l, err, "remove failed", lua.Internal))
		return 2
	}
	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func fsReaddir(l *lua.LState) int {
	fs := checkFS(l, 1)
	if fs == nil {
		return 0
	}
	path := l.CheckString(2)
	if path == "" {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "path required").WithKind(lua.Invalid))
		return 2
	}
	resolved, err := fs.resolvePath(path)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "invalid path").WithKind(lua.Invalid))
		return 2
	}
	info, err := fs.fs.Stat(resolved)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(wrapFilesystemError(l, err, "failed to stat directory", lua.NotFound))
		return 2
	}
	if !info.IsDir() {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "not a directory: "+path).WithKind(lua.Invalid))
		return 2
	}
	entries, err := fs.fs.ReadDir(resolved)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(wrapFilesystemError(l, err, "readdir failed", lua.Internal))
		return 2
	}

	// Create iterator userdata
	it := &dirIterator{entries: entries, index: 0}
	ud := l.NewUserData()
	ud.Value = it
	ud.Metatable = value.GetTypeMetatable(nil, "fs.DirIterator")

	l.Push(lua.LGoFunc(dirIteratorNext))
	l.Push(ud)
	return 2
}

func dirIteratorNext(l *lua.LState) int {
	ud := l.CheckUserData(1)
	it, ok := ud.Value.(*dirIterator)
	if !ok {
		l.Push(lua.LNil)
		return 1
	}

	if it.index >= len(it.entries) {
		l.Push(lua.LNil)
		return 1
	}

	entry := it.entries[it.index]
	it.index++

	entryTbl := l.CreateTable(0, 2)
	entryTbl.RawSetString("name", lua.LString(entry.Name()))
	if entry.IsDir() {
		entryTbl.RawSetString("type", lua.LString(typeDir))
	} else {
		entryTbl.RawSetString("type", lua.LString(typeFile))
	}
	l.Push(entryTbl)
	return 1
}

func fsExists(l *lua.LState) int {
	fs := checkFS(l, 1)
	if fs == nil {
		return 0
	}
	path := l.CheckString(2)
	resolved, err := fs.resolvePath(path)
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.WrapErrorWithLua(l, err, "invalid path").WithKind(lua.Invalid))
		return 2
	}
	_, err = fs.fs.Stat(resolved)
	l.Push(lua.LBool(err == nil))
	l.Push(lua.LNil)
	return 2
}

func fsIsdir(l *lua.LState) int {
	fs := checkFS(l, 1)
	if fs == nil {
		return 0
	}
	path := l.CheckString(2)
	resolved, err := fs.resolvePath(path)
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.WrapErrorWithLua(l, err, "invalid path").WithKind(lua.Invalid))
		return 2
	}
	info, err := fs.fs.Stat(resolved)
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(wrapFilesystemError(l, err, "stat failed", lua.NotFound))
		return 2
	}
	l.Push(lua.LBool(info.IsDir()))
	l.Push(lua.LNil)
	return 2
}

func fsReadfile(l *lua.LState) int {
	fs := checkFS(l, 1)
	if fs == nil {
		return 0
	}
	path := l.CheckString(2)
	if path == "" {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "path required").WithKind(lua.Invalid))
		return 2
	}
	resolved, err := fs.resolvePath(path)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "invalid path").WithKind(lua.Invalid))
		return 2
	}
	file, err := fs.fs.OpenFile(resolved, os.O_RDONLY, 0)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(wrapFilesystemError(l, err, "failed to open file", lua.NotFound))
		return 2
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(file)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(wrapFilesystemError(l, err, "failed to read file", lua.Internal))
		return 2
	}

	l.Push(lua.LString(data))
	l.Push(lua.LNil)
	return 2
}

func fsWritefile(l *lua.LState) int {
	fs := checkFS(l, 1)
	if fs == nil {
		return 0
	}
	path := l.CheckString(2)
	if path == "" {
		l.Push(lua.LFalse)
		l.Push(lua.NewLuaError(l, "path required").WithKind(lua.Invalid))
		return 2
	}
	v := l.Get(3)
	if v == lua.LNil {
		l.Push(lua.LFalse)
		l.Push(lua.NewLuaError(l, "data argument required").WithKind(lua.Invalid))
		return 2
	}
	mode := l.OptString(4, "w")
	var flag int
	switch mode {
	case "w":
		flag = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	case "wx":
		flag = os.O_WRONLY | os.O_CREATE | os.O_TRUNC | os.O_EXCL
	case "a":
		flag = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	default:
		l.Push(lua.LFalse)
		l.Push(lua.NewLuaError(l, "invalid mode; must be 'w', 'wx' or 'a'").WithKind(lua.Invalid))
		return 2
	}

	resolved, err := fs.resolvePath(path)
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.WrapErrorWithLua(l, err, "invalid path").WithKind(lua.Invalid))
		return 2
	}
	dstFile, err := fs.fs.OpenFile(resolved, flag, 0644)
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(wrapFilesystemError(l, err, "failed to open destination", lua.NotFound))
		return 2
	}
	defer func() { _ = dstFile.Close() }()

	var reader io.Reader
	switch v := v.(type) {
	case lua.LString:
		reader = strings.NewReader(string(v))
	case *lua.LUserData:
		if r, ok := v.Value.(io.Reader); ok {
			reader = r
		} else if rp, ok := v.Value.(resource.ReaderProvider); ok {
			r, err := rp.GetReader(l.Context())
			if err != nil {
				l.Push(lua.LFalse)
				l.Push(lua.WrapErrorWithLua(l, err, "failed to get reader").WithKind(lua.Internal))
				return 2
			}
			reader = r
		} else {
			l.Push(lua.LFalse)
			l.Push(lua.NewLuaError(l, "input does not implement io.Reader").WithKind(lua.Invalid))
			return 2
		}
	default:
		l.Push(lua.LFalse)
		l.Push(lua.NewLuaError(l, "invalid input type, expected string or Reader").WithKind(lua.Invalid))
		return 2
	}

	if _, err := io.Copy(dstFile, reader); err != nil {
		l.Push(lua.LFalse)
		l.Push(wrapFilesystemError(l, err, "copy failed", lua.Internal))
		return 2
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

// fsWritefileAtomic delegates the complete replacement to the filesystem's
// optional atomic capability. The Lua layer buffers and bounds the input so a
// backend is never asked to publish an oversized document or a partial stream.
func fsWritefileAtomic(l *lua.LState) int {
	fs := checkFS(l, 1)
	if fs == nil {
		return 0
	}
	pathArg := l.CheckString(2)
	if pathArg == "" {
		l.Push(lua.LFalse)
		l.Push(lua.NewLuaError(l, "path required").WithKind(lua.Invalid))
		return 2
	}
	content := l.Get(3)
	if content == lua.LNil {
		l.Push(lua.LFalse)
		l.Push(lua.NewLuaError(l, "data argument required").WithKind(lua.Invalid))
		return 2
	}

	resolved, err := fs.resolvePath(pathArg)
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.WrapErrorWithLua(l, err, "invalid path").WithKind(lua.Invalid))
		return 2
	}

	atomicFS, ok := fs.fs.(fsapi.AtomicWriteFS)
	if !ok {
		l.Push(lua.LFalse)
		l.Push(lua.WrapErrorWithLua(l, fsapi.ErrAtomicWriteUnsupported, "atomic write unsupported").WithKind(lua.Unavailable).WithRetryable(false))
		return 2
	}

	value, ok := content.(lua.LString)
	if !ok {
		l.Push(lua.LFalse)
		l.Push(lua.NewLuaError(l, "content must be a string").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	if len(value) > maxAtomicWriteBytes {
		l.Push(lua.LFalse)
		l.Push(lua.NewLuaError(l, "atomic write input exceeds 8 MiB").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	data := []byte(string(value))

	if err := atomicFS.WriteFileAtomic(resolved, data, 0600); err != nil {
		if errors.Is(err, fsapi.ErrPublishedSyncFailed) {
			l.Push(lua.LFalse)
			l.Push(lua.WrapErrorWithLua(l, fsapi.ErrPublishedSyncFailed, "atomic write published; sync status uncertain").WithKind(lua.Unavailable).WithRetryable(false).WithDetails(map[string]any{"published": true}))
			return 2
		}
		if errors.Is(err, fsapi.ErrAtomicWriteUnsupported) {
			l.Push(lua.LFalse)
			l.Push(lua.WrapErrorWithLua(l, fsapi.ErrAtomicWriteUnsupported, "atomic write unsupported").WithKind(lua.Unavailable).WithRetryable(false))
			return 2
		}
		if errors.Is(err, fsapi.ErrReadOnly) || errors.Is(err, fsapi.ErrPermissionDenied) || errors.Is(err, iofs.ErrPermission) {
			l.Push(lua.LFalse)
			l.Push(lua.NewLuaError(l, "atomic write permission denied").WithKind(lua.PermissionDenied).WithRetryable(false))
			return 2
		}
		l.Push(lua.LFalse)
		l.Push(lua.NewLuaError(l, "atomic write failed").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func fsToString(l *lua.LState) int {
	fs := checkFS(l, 1)
	if fs == nil {
		return 0
	}
	l.Push(lua.LString(fmt.Sprintf("fs.FS{cwd=%s}", fs.cwd)))
	return 1
}
