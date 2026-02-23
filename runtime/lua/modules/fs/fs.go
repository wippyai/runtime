// SPDX-License-Identifier: MPL-2.0

package fs

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
		res = filepath.Join(f.cwd, p)
	}
	if res == "" {
		return ".", nil
	}

	// Clean and validate path doesn't escape root
	res = filepath.Clean(res)
	if res == ".." || strings.HasPrefix(res, "../") || strings.HasPrefix(res, "..\\") {
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
		l.Push(lua.WrapErrorWithLua(l, err, "failed to stat directory").WithKind(lua.NotFound))
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
		l.Push(lua.WrapErrorWithLua(l, err, "failed to open file").WithKind(lua.NotFound))
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
		l.Push(lua.WrapErrorWithLua(l, err, "stat failed").WithKind(lua.NotFound))
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
		l.Push(lua.WrapErrorWithLua(l, err, "mkdir failed").WithKind(lua.Internal))
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
		l.Push(lua.WrapErrorWithLua(l, err, "remove failed").WithKind(lua.Internal))
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
		l.Push(lua.WrapErrorWithLua(l, err, "failed to stat directory").WithKind(lua.NotFound))
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
		l.Push(lua.WrapErrorWithLua(l, err, "readdir failed").WithKind(lua.Internal))
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
		l.Push(lua.WrapErrorWithLua(l, err, "stat failed").WithKind(lua.NotFound))
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
		l.Push(lua.WrapErrorWithLua(l, err, "failed to open file").WithKind(lua.NotFound))
		return 2
	}
	defer func() { _ = file.Close() }()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, file); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "failed to read file").WithKind(lua.Internal))
		return 2
	}

	l.Push(lua.LString(buf.String()))
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
		l.Push(lua.WrapErrorWithLua(l, err, "failed to open destination").WithKind(lua.NotFound))
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
		l.Push(lua.WrapErrorWithLua(l, err, "copy failed").WithKind(lua.Internal))
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
