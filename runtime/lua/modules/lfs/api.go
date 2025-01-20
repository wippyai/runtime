package lfs

import (
	"os"
	"path"
	"path/filepath"
	"time"

	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func (m *Module) apiAttributes(l *lua.LState) int {
	return attributes(l, os.Stat)
}

func (m *Module) close(l *lua.LState) int {
	ud := l.CheckUserData(1)
	if ud == nil {
		return 0
	}

	// if not file, then there is something else in the userdata
	f, ok := ud.Value.(*os.File)
	if !ok {
		return 0
	}
	if f != nil {
		_ = f.Close()
	}
	return 0
}

func (m *Module) apiChdir(l *lua.LState) int {
	dir := l.CheckString(1)
	m.log.Debug("previous dir", zap.String("dir", m.currDir), zap.String("new", dir))
	if dir == m.currDir {
		l.Push(lua.LNil)
		l.Push(lua.LTrue)
		return 1
	}

	// if the path is relative, then concatenate it with the current directory
	if isRelative(dir) {
		dir = path.Join(m.currDir, dir)
	}

	// check if the directory exists
	f, err := os.Open(dir)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	_ = f.Close()

	// save the current directory
	m.currDir = dir
	m.log.Debug("current dir", zap.String("dir", m.currDir))

	l.Push(lua.LTrue)
	return 1
}

func (m *Module) apiLockdir(l *lua.LState) int {
	l.RaiseError("unimplemented function")
	return 0
}

func (m *Module) apiCurrentdir(l *lua.LState) int {
	m.log.Debug("current dir", zap.String("dir", m.currDir))

	l.Push(lua.LString(m.currDir))
	return 1
}

func (m *Module) apiDir(l *lua.LState) int {
	lp := l.CheckString(1)
	if lp == "" {
		l.RaiseError("%s", "path is empty")
		return 0
	}

	// if the path is relative, then concatenate it with the current directory
	if isRelative(lp) {
		lp = path.Join(m.currDir, lp)
	}

	m.log.Debug("dir", zap.String("path", lp))

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
	return 2
}

func (m *Module) apiLock(l *lua.LState) int {
	l.RaiseError("unimplemented function")
	return 0
}

func (m *Module) apiLink(l *lua.LState) int {
	old := l.CheckString(1)
	// if the path is relative, then concatenate it with the current directory
	if isRelative(old) {
		old = path.Join(m.currDir, old)
	}

	newn := l.CheckString(2)
	// if the path is relative, then concatenate it with the current directory
	if isRelative(newn) {
		newn = path.Join(m.currDir, newn)
	}

	m.log.Debug("link", zap.String("old", old), zap.String("new", newn))

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

func (m *Module) apiMkdir(l *lua.LState) int {
	dir := l.CheckString(1)

	// if the path is relative, then concatenate it with the current directory
	if isRelative(dir) {
		dir = path.Join(m.currDir, dir)
	}

	m.log.Debug("mkdir", zap.String("dir", dir))
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

func (m *Module) apiRmdir(l *lua.LState) int {
	dir := l.CheckString(1)

	// if the path is relative, then concatenate it with the current directory
	if isRelative(dir) {
		dir = path.Join(m.currDir, dir)
	}

	m.log.Debug("rmdir", zap.String("dir", dir))

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

func (m *Module) apiSetmode(l *lua.LState) int {
	l.RaiseError("unimplemented function")
	return 0
}

func (m *Module) apiSymlinkattributes(l *lua.LState) int {
	return attributes(l, os.Lstat)
}

func (m *Module) apiTouch(l *lua.LState) int {
	dir := l.CheckString(1)
	// if the path is relative, then concatenate it with the current directory
	if isRelative(dir) {
		dir = path.Join(m.currDir, dir)
	}

	m.log.Debug("touch", zap.String("file", dir))

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

func (m *Module) apiUnlock(l *lua.LState) int {
	l.RaiseError("unimplemented function")
	return 0
}

// isRelative returns true if the path is relative.
func isRelative(path string) bool {
	return !filepath.IsAbs(path)
}
