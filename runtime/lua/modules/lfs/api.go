package lfs

import (
	fsapi "github.com/ponyruntime/pony/api/fs"
	"github.com/ponyruntime/pony/runtime/uow"
	"log"
	"os"
	"path"
	"path/filepath"
	"time"

	apic "github.com/ponyruntime/pony/api/context"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func apiAttributes(l *lua.LState) int {
	return attributes(l, os.Stat)
}

func getCtxLogger(l *lua.LState) *zap.Logger {
	ctx := l.Context()
	return ctx.Value(apic.LoggerCtx).(*zap.Logger)
}

func apiChdir(l *lua.LState) int {
	dir := l.CheckString(1)
	cwd := l.GetGlobal(globalFnName).String()

	if dir == cwd {
		l.Push(lua.LNil)
		l.Push(lua.LTrue)
		return 1
	}

	// if the path is relative, then concatenate it with the current directory
	if isRelative(dir) {
		dir = path.Join(cwd, dir)
	}

	// check if the directory exists
	f, err := os.Open(dir)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	_ = f.Close()

	// save the new cwd
	l.SetGlobal(globalFnName, lua.LString(dir))

	uow.FromContext(l.Context()).AddCleanup(func() error {
		// clear the global variable
		l.SetGlobal(globalFnName, lua.LString(""))
		return nil
	})

	l.Push(lua.LTrue)
	return 1
}

func apiLockDir(l *lua.LState) int {
	l.RaiseError("unimplemented function")
	return 0
}

func apiCurrentDir(l *lua.LState) int {
	uw := uow.FromContext(l.Context())
	if uw == nil {
		l.RaiseError("no uow found in context")
		return 0
	}

	fs := fsapi.FromContext(l.Context())
	log.Printf("fs: %v", fs)

	a, ok := uw.Get("currentdir")
	log.Printf("current dir: %v %v", a, ok)

	cwd := l.GetGlobal(globalFnName).String()

	l.Push(lua.LString(cwd))
	return 1
}

func apiDir(l *lua.LState) int {
	lp := l.CheckString(1)
	if lp == "" {
		l.RaiseError("%s", "path is empty")
		return 0
	}

	cwd := l.GetGlobal(globalFnName).String()

	// if the path is relative, then concatenate it with the current directory
	if isRelative(lp) {
		lp = path.Join(cwd, lp)
	}

	f, err := os.Open(lp)
	if err != nil {
		l.RaiseError("%s", err)
		return 0
	}
	stat, err := f.Stat()
	if err != nil {
		l.RaiseError("%s", err)
		return 0
	}
	if !stat.IsDir() {
		l.RaiseError("not a directory")
		return 0
	}

	l.Push(l.NewFunction(dirItr))
	ud := l.NewUserData()
	ud.Value = f
	l.Push(ud)

	// add the file to the cleanup
	uow.FromContext(l.Context()).AddCleanup(func() error {
		_ = f.Close()
		return nil
	})

	return 2
}

func apiLock(l *lua.LState) int {
	l.RaiseError("unimplemented function")
	return 0
}

func apiLink(l *lua.LState) int {
	old := l.CheckString(1)
	cwd := l.GetGlobal(globalFnName).String()
	log := getCtxLogger(l)
	if isRelative(old) {
		old = path.Join(cwd, old)
	}

	newn := l.CheckString(2)
	// if the path is relative, then concatenate it with the current directory
	if isRelative(newn) {
		newn = path.Join(cwd, newn)
	}

	log.Debug("link", zap.String("old", old), zap.String("new", newn))

	symlink := l.OptBool(3, false)

	var err error
	if symlink {
		err = os.Symlink(old, newn)
	} else {
		err = os.Link(old, newn)
	}

	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LTrue)
	return 1
}

func apiMkdir(l *lua.LState) int {
	dir := l.CheckString(1)
	cwd := l.GetGlobal(globalFnName).String()

	// if the path is relative, then concatenate it with the current directory
	if isRelative(dir) {
		dir = path.Join(cwd, dir)
	}

	// 0644 - user: read, write; group: read; others: read
	err := os.Mkdir(dir, 0644)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LTrue)
	return 1
}

func apiRmdir(l *lua.LState) int {
	dir := l.CheckString(1)
	cwd := l.GetGlobal(globalFnName).String()

	// if the path is relative, then concatenate it with the current directory
	if isRelative(dir) {
		dir = path.Join(cwd, dir)
	}

	stat, err := os.Stat(dir)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	if !stat.IsDir() {
		l.Push(lua.LNil)
		l.Push(lua.LString("not a directory"))
		return 2
	}
	err = os.Remove(dir)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	l.Push(lua.LTrue)
	return 1
}

func apiSetMode(l *lua.LState) int {
	l.RaiseError("unimplemented function")
	return 0
}

func apiSymlinkAttributes(l *lua.LState) int {
	return attributes(l, os.Lstat)
}

func apiTouch(l *lua.LState) int {
	dir := l.CheckString(1)
	log := getCtxLogger(l)
	cwd := l.GetGlobal(globalFnName).String()

	if isRelative(dir) {
		dir = path.Join(cwd, dir)
	}

	log.Debug("touch", zap.String("file", dir))

	atime := l.OptInt64(2, time.Now().Unix())
	mtime := l.OptInt64(3, atime)

	// Check if the file exists. If not, create it.
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		file, err := os.Create(dir)
		if err != nil {
			l.Push(lua.LNil)
			l.Push(lua.LString(err.Error()))
			return 2
		}
		file.Close() // Close the file immediately after creating it
	}

	// Now that the file exists, change its timestamps
	err := os.Chtimes(dir, time.Unix(atime, 0), time.Unix(mtime, 0))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	l.Push(lua.LTrue)
	return 1
}

func apiUnlock(l *lua.LState) int {
	l.RaiseError("unimplemented function")
	return 0
}

// isRelative returns true if the path is relative.
func isRelative(path string) bool {
	return !filepath.IsAbs(path)
}
