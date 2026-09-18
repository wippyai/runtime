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

const (
	maxAtomicWriteBytes = 8 << 20
	// Atomic publication creates files with the same mode as an ordinary
	// writefile, so one verb has one default.
	atomicWriteFileMode = 0644
)

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
	"chdir":      fsChdir,
	"pwd":        fsPwd,
	"open":       fsOpen,
	"stat":       fsStat,
	"mkdir":      fsMkdir,
	"remove":     fsRemove,
	"readdir":    fsReaddir,
	"exists":     fsExists,
	"isdir":      fsIsdir,
	"readfile":   fsReadfile,
	"read_file":  fsReadfile,
	"writefile":  fsWritefile,
	"write_file": fsWritefile,
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
	mode, atomic, optErr := writefileOptions(l, 4)
	if optErr != nil {
		l.Push(lua.LFalse)
		l.Push(optErr)
		return 2
	}
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
	if atomic && mode != "w" {
		l.Push(lua.LFalse)
		l.Push(lua.NewLuaError(l, "atomic write requires mode 'w'").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	resolved, err := fs.resolvePath(path)
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.WrapErrorWithLua(l, err, "invalid path").WithKind(lua.Invalid))
		return 2
	}

	if atomic {
		return writefileAtomic(l, fs, resolved, v)
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

// writefileOptions reads the fourth writefile argument, which is either the
// write mode as a string or an option table carrying that mode plus the atomic
// publication request. A table is validated in full: a field the call does not
// know, or a value of the wrong type, is an error, so a request for atomic
// publication is never dropped in favor of an ordinary write.
func writefileOptions(l *lua.LState, index int) (mode string, atomic bool, optErr *lua.Error) {
	mode = "w"
	arg := l.Get(index)
	if arg == lua.LNil {
		return mode, false, nil
	}
	table, ok := arg.(*lua.LTable)
	if !ok {
		return l.CheckString(index), false, nil
	}
	invalid := func(message string) *lua.Error {
		return lua.NewLuaError(l, message).WithKind(lua.Invalid).WithRetryable(false)
	}
	table.ForEach(func(key, value lua.LValue) {
		if optErr != nil {
			return
		}
		name, isString := key.(lua.LString)
		switch {
		case !isString:
			optErr = invalid("writefile options must be a table of named fields")
		case name == "mode":
			text, isText := value.(lua.LString)
			if !isText {
				optErr = invalid("writefile option 'mode' must be a string")
				return
			}
			mode = string(text)
		case name == "atomic":
			flag, isFlag := value.(lua.LBool)
			if !isFlag {
				optErr = invalid("writefile option 'atomic' must be a boolean")
				return
			}
			atomic = bool(flag)
		default:
			optErr = invalid("unknown writefile option: " + string(name))
		}
	})
	if optErr != nil {
		return "", false, optErr
	}
	return mode, atomic, nil
}

// writefileAtomic delegates the complete replacement to the filesystem's
// optional atomic capability. The Lua layer buffers and bounds the input so a
// backend is never asked to publish an oversized document or a partial stream.
func writefileAtomic(l *lua.LState, fs *FS, resolved string, content lua.LValue) int {
	atomicFS, ok := fs.fs.(fsapi.AtomicWriteFS)
	if !ok {
		l.Push(lua.LFalse)
		l.Push(lua.WrapErrorWithLua(l, fsapi.ErrAtomicWriteUnsupported, "atomic write unsupported").WithKind(lua.Unavailable).WithRetryable(false))
		return 2
	}

	value, ok := content.(lua.LString)
	if !ok {
		l.Push(lua.LFalse)
		l.Push(lua.NewLuaError(l, "atomic write requires string content").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	if len(value) > maxAtomicWriteBytes {
		l.Push(lua.LFalse)
		l.Push(lua.NewLuaError(l, "atomic write input exceeds 8 MiB").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	if err := atomicFS.WriteFileAtomic(resolved, []byte(string(value)), atomicWriteFileMode); err != nil {
		l.Push(lua.LFalse)
		switch {
		case errors.Is(err, fsapi.ErrPublishedSyncFailed):
			l.Push(lua.WrapErrorWithLua(l, err, "atomic write published; sync status uncertain").WithKind(lua.Unavailable).WithRetryable(false).WithDetails(map[string]any{"published": true}))
		case errors.Is(err, fsapi.ErrAtomicWriteUnsupported):
			l.Push(lua.WrapErrorWithLua(l, err, "atomic write unsupported").WithKind(lua.Unavailable).WithRetryable(false))
		default:
			l.Push(wrapFilesystemError(l, err, "atomic write failed", lua.Internal))
		}
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
