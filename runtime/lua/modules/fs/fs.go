package fs

import (
	"bytes"
	"fmt"
	ctxapi "github.com/ponyruntime/pony/api/context"
	fsapi "github.com/ponyruntime/pony/api/fs"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/ponyruntime/pony/runtime/uow"
	lua "github.com/yuin/gopher-lua"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var cwdKey = &ctxapi.Key{Name: "fs.cwd"}

// FS represents a filesystem instance wrapper with its own current working directory.
type FS struct {
	fs  fsapi.FS
	cwd string // current working directory relative to the FS mount point; "." represents root.
}

// registerFS registers the FS type and its methods, including helper functions.
func registerFS(l *lua.LState, mod *lua.LTable) {
	mt := l.NewTypeMetatable("fs.FS")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		// Core operations
		"chdir":   fsChdir,
		"pwd":     fsPwd,
		"open":    fsOpen,
		"stat":    fsStat,
		"mkdir":   fsMkdir,
		"remove":  fsRemove,
		"readdir": fsReadDir,

		// File operations
		"readfile":  fsReadFile,  // instead of read_all
		"writefile": fsWriteFile, // instead of write_all

		// Checks
		"isdir":  fsIsDir,
		"exists": fsExists,
	}))

	l.SetFuncs(mod, map[string]lua.LGFunction{
		"get": apiGet,
	})
}

// resolvePath resolves the provided path relative to the FS instance's cwd.
// If the path is absolute (starts with '/'), the leading slash is stripped.
// If the path is relative, it is joined with the current cwd.
func (f *FS) resolvePath(p string) string {
	var res string
	if p == "" {
		res = f.cwd
	} else if p[0] == '/' {
		// Absolute path: remove the leading slash.
		res = p[1:]
	} else {
		res = filepath.Join(f.cwd, p)
	}
	if res == "" {
		return "."
	}
	return res
}

// fsChdir changes the current directory stored in the FS wrapper.
func fsChdir(l *lua.LState) int {
	fsInst := CheckFS(l, 1)
	path := l.CheckString(2)
	if path == "" {
		l.RaiseError("path required")
		return 0
	}
	// Resolve the target path relative to the current cwd.
	target := fsInst.resolvePath(path)
	// Check that the target exists and is a directory.
	info, err := fsInst.fs.Stat(target)
	if err != nil {
		if os.IsNotExist(err) {
			l.RaiseError("directory does not exist: %s", path)
			return 0
		}
		if os.IsPermission(err) {
			l.RaiseError("permission denied: %s", path)
			return 0
		}
		wrappedErr := fmt.Errorf("failed to stat directory %s: %w", path, err)
		l.RaiseError("%s", wrappedErr.Error())
		return 0
	}
	if !info.IsDir() {
		l.RaiseError("not a directory: %s", path)
		return 0
	}
	// Update the current directory in the FS wrapper.
	fsInst.cwd = target
	l.Push(lua.LBool(true))
	return 1
}

// fsPwd returns the current working directory from the FS wrapper.
func fsPwd(l *lua.LState) int {
	fsInst := CheckFS(l, 1)
	// Return "/" if cwd is "." (root) or empty.
	if fsInst.cwd == "" || fsInst.cwd == "." {
		l.Push(lua.LString("/"))
	} else {
		l.Push(lua.LString("/" + fsInst.cwd))
	}
	return 1
}

// fsOpen opens a file relative to the current working directory.
func fsOpen(l *lua.LState) int {
	fsInst := CheckFS(l, 1)
	path := l.CheckString(2)
	if path == "" {
		l.RaiseError("path required")
		return 0
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
		l.RaiseError("invalid mode: must be 'r', 'w', 'wx' or 'a'")
		return 0
	}
	// Resolve the file path.
	resolved := fsInst.resolvePath(path)
	file, err := fsInst.fs.OpenFile(resolved, flag, 0644)
	if err != nil {
		if os.IsNotExist(err) {
			l.RaiseError("file not found: %s", path)
			return 0
		}
		if os.IsPermission(err) {
			l.RaiseError("permission denied: %s", path)
			return 0
		}
		l.RaiseError("failed to open file: %s", err)
		return 0
	}
	// Use uow to register cleanup for open files.
	uw := uow.FromContext(l.Context())
	if uw != nil {
		uw.AddCleanup(func() error {
			return file.Close()
		})
	}
	l.Push(WrapFile(l, file))
	return 1
}

// fsStat returns file information for the given path relative to the current cwd.
func fsStat(l *lua.LState) int {
	fsInst := CheckFS(l, 1)
	path := l.CheckString(2)
	if path == "" {
		l.RaiseError("path required")
		return 0
	}
	resolved := fsInst.resolvePath(path)
	info, err := fsInst.fs.Stat(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			l.RaiseError("path does not exist: %s", path)
			return 0
		}
		if os.IsPermission(err) {
			l.RaiseError("permission denied: %s", path)
			return 0
		}
		wrappedErr := fmt.Errorf("stat failed for path %s: %w", path, err)
		l.RaiseError("%s", wrappedErr.Error())
		return 0
	}
	l.Push(pushFileInfo(l, info))
	return 1
}

// fsReadDir lists the directory entries for the given path relative to the current cwd.
func fsReadDir(l *lua.LState) int {
	fsInst := CheckFS(l, 1)
	path := l.CheckString(2)
	if path == "" {
		l.RaiseError("path required")
		return 0
	}
	resolved := fsInst.resolvePath(path)
	// Validate that the path exists and is a directory.
	info, err := fsInst.fs.Stat(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			l.RaiseError("directory does not exist: %s", path)
			return 0
		}
		if os.IsPermission(err) {
			l.RaiseError("permission denied: %s", path)
			return 0
		}
		wrappedErr := fmt.Errorf("failed to stat directory %s: %w", path, err)
		l.RaiseError("%s", wrappedErr.Error())
		return 0
	}
	if !info.IsDir() {
		l.RaiseError("not a directory: %s", path)
		return 0
	}
	entries, err := fsInst.fs.ReadDir(resolved)
	if err != nil {
		if os.IsPermission(err) {
			l.RaiseError("permission denied: %s", path)
			return 0
		}
		wrappedErr := fmt.Errorf("readdir failed for directory %s: %w", path, err)
		l.RaiseError("%s", wrappedErr.Error())
		return 0
	}
	index := 0
	iter := func(l *lua.LState) int {
		if index >= len(entries) {
			l.Push(lua.LNil)
			return 1
		}
		entry := entries[index]
		index++
		entryTbl := l.NewTable()
		entryTbl.RawSetString("name", lua.LString(entry.Name()))
		if entry.IsDir() {
			entryTbl.RawSetString("type", lua.LString(typeDir))
		} else {
			entryTbl.RawSetString("type", lua.LString(typeFile))
		}
		l.Push(entryTbl)
		return 1
	}
	l.Push(l.NewFunction(iter))
	return 1
}

// fsMkdir creates a directory at the given path relative to the current cwd.
func fsMkdir(l *lua.LState) int {
	fsInst := CheckFS(l, 1)
	path := l.CheckString(2)
	if path == "" {
		l.RaiseError("path required")
		return 0
	}
	resolved := fsInst.resolvePath(path)
	// Check if the path already exists.
	_, err := fsInst.fs.Stat(resolved)
	if err == nil {
		l.RaiseError("path already exists: %s", path)
		return 0
	}
	if err := fsInst.fs.Mkdir(resolved, 0755); err != nil {
		if os.IsPermission(err) {
			l.RaiseError("permission denied: %s", path)
			return 0
		}
		wrappedErr := fmt.Errorf("mkdir failed for path %s: %w", path, err)
		l.RaiseError("%s", wrappedErr.Error())
		return 0
	}
	l.Push(lua.LBool(true))
	return 1
}

// fsRemove deletes a file or directory at the given path relative to the current cwd.
func fsRemove(l *lua.LState) int {
	fsInst := CheckFS(l, 1)
	path := l.CheckString(2)
	if path == "" {
		l.RaiseError("path required")
		return 0
	}
	resolved := fsInst.resolvePath(path)
	// If it's a directory, check that it is empty.
	info, err := fsInst.fs.Stat(resolved)
	if err == nil && info.IsDir() {
		entries, err := fsInst.fs.ReadDir(resolved)
		if err == nil && len(entries) > 0 {
			l.RaiseError("directory not empty: %s", path)
			return 0
		}
	}
	if err := fsInst.fs.Remove(resolved); err != nil {
		if os.IsPermission(err) {
			l.RaiseError("permission denied: %s", path)
			return 0
		}
		wrappedErr := fmt.Errorf("remove failed for path %s: %w", path, err)
		l.RaiseError("%s", wrappedErr.Error())
		return 0
	}
	l.Push(lua.LBool(true))
	return 1
}

// fsExists returns true if the file or directory exists at the given path relative to the current cwd.
func fsExists(l *lua.LState) int {
	fsInst := CheckFS(l, 1)
	path := l.CheckString(2)
	resolved := fsInst.resolvePath(path)
	_, err := fsInst.fs.Stat(resolved)
	if err == nil {
		l.Push(lua.LBool(true))
		return 1
	}
	if os.IsNotExist(err) {
		l.Push(lua.LBool(false))
		return 1
	}
	l.RaiseError("fs.exists: %s", err.Error())
	return 0
}

// fsIsDir returns true if the given path (relative to cwd) refers to a directory.
func fsIsDir(l *lua.LState) int {
	fsInst := CheckFS(l, 1)
	path := l.CheckString(2)
	resolved := fsInst.resolvePath(path)
	info, err := fsInst.fs.Stat(resolved)
	if err != nil {
		l.RaiseError("fs.is_dir: %s", err.Error())
		return 0
	}
	l.Push(lua.LBool(info.IsDir()))
	return 1
}

// fsReadFile reads an entire file's contents using coroutine.Wrap
func fsReadFile(l *lua.LState) int {
	fsInst := CheckFS(l, 1)
	path := l.CheckString(2)
	if path == "" {
		l.RaiseError("path required")
		return 0
	}
	resolved := fsInst.resolvePath(path)

	coroutine.Wrap(l, func() *engine.Result {
		file, err := fsInst.fs.OpenFile(resolved, os.O_RDONLY, 0)
		if err != nil {
			return engine.NewResult(nil, nil, fmt.Errorf("fs.read_all: %s", err.Error()))
		}
		f := &File{file: file}
		defer func() {
			_ = f.Close() // todo: add logger from ctx
		}()

		var buf bytes.Buffer
		if _, err := io.Copy(&buf, f); err != nil {
			return engine.NewResult(nil, nil, fmt.Errorf("fs.read_all: %s", err.Error()))
		}

		return engine.NewResult(nil, []lua.LValue{lua.LString(buf.String())}, nil)
	})

	return -1 // Yield
}

// fsWriteall writes data to a file using coroutine.Wrap
func fsWriteFile(l *lua.LState) int {
	fsInst := CheckFS(l, 1)
	path := l.CheckString(2)

	// Validate second argument is present
	if l.Get(3) == lua.LNil {
		l.RaiseError("fs.writeall: data argument required")
		return 0
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
		l.RaiseError("fs.writeall: invalid mode; must be 'w', 'wx' or 'a'")
		return 0
	}

	resolved := fsInst.resolvePath(path)

	coroutine.Wrap(l, func() *engine.Result {
		// Open destination file
		dstFile, err := fsInst.fs.OpenFile(resolved, flag, 0644)
		if err != nil {
			return engine.NewResult(nil, nil, fmt.Errorf("fs.writeall: failed to open destination: %w", err))
		}
		dst := &File{file: dstFile}
		defer func() {
			_ = dst.Close() // todo: ctx logger
		}()

		// Determine the reader based on input type
		var reader io.Reader
		switch v := l.Get(3).(type) {
		case lua.LString:
			reader = strings.NewReader(string(v))

		case *lua.LUserData:
			// Check if the userdata implements io.Reader
			if r, ok := v.Value.(io.Reader); ok {
				reader = r
			} else {
				return engine.NewResult(nil, nil, fmt.Errorf("fs.writeall: input does not implement io.Reader"))
			}

		default:
			return engine.NewResult(nil, nil, fmt.Errorf("fs.writeall: invalid input type, expected string or Reader"))
		}

		// Copy the data
		if _, err := io.Copy(dst, reader); err != nil {
			return engine.NewResult(nil, nil, fmt.Errorf("fs.writeall: copy failed: %w", err))
		}

		return engine.NewResult(nil, []lua.LValue{lua.LBool(true)}, nil)
	})

	return -1 // Yield
}
