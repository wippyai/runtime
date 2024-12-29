package lfs

import (
	"github.com/ponyruntime/go-lua"
	"os"
	"time"
)

func apiAttributes(l *lua.LState) int {
	return attributes(l, os.Stat)
}

func apiChdir(l *lua.LState) int {
	dir := l.CheckString(1)

	err := os.Chdir(dir)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	l.Push(lua.LTrue)
	return 1
}

func apiLockdir(l *lua.LState) int {
	l.RaiseError("unimplemented function")
	return 0
}

func apiCurrentdir(l *lua.LState) int {
	dir, err := os.Getwd()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	l.Push(lua.LString(dir))
	return 1
}

func apiDir(l *lua.LState) int {
	path := l.CheckString(1)

	f, err := os.Open(path)
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

func apiLock(l *lua.LState) int {
	l.RaiseError("unimplemented function")
	return 0
}

func apiLink(l *lua.LState) int {
	old := l.CheckString(1)
	_new := l.CheckString(2)
	symlink := l.OptBool(3, false)

	var err error
	if symlink {
		err = os.Symlink(old, _new)
	} else {
		err = os.Link(old, _new)
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

	err := os.Mkdir(dir, 0755)
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

func apiSetmode(l *lua.LState) int {
	l.RaiseError("unimplemented function")
	return 0
}

func apiSymlinkattributes(l *lua.LState) int {
	return attributes(l, os.Lstat)
}

func apiTouch(l *lua.LState) int {
	filepath := l.CheckString(1)
	atime := l.OptInt64(2, time.Now().Unix())
	mtime := l.OptInt64(3, atime)

	err := os.Chtimes(filepath, time.Unix(atime, 0), time.Unix(mtime, 0))
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
