package lfs

import (
	"os"
	"path/filepath"

	lua "github.com/yuin/gopher-lua"
)

func attributes(l *lua.LState, statFunc func(string) (os.FileInfo, error)) int {
	fp := l.CheckString(1)

	stat, err := statFunc(fp)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	table := l.NewTable()
	if err := attributesFill(table, stat); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	if table.RawGetString("mode").String() == "link" {
		if path, err := filepath.EvalSymlinks(fp); err == nil {
			table.RawSetString("target", lua.LString(path))
		}
	}

	if l.GetTop() > 1 {
		requestName := l.CheckString(2)
		l.Push(table.RawGetString(requestName))
		return 1
	}
	l.Push(table)
	return 1
}

func dirItr(l *lua.LState) int {
	ud := l.CheckUserData(1)
	if ud == nil {
		return 0
	}

	f, ok := ud.Value.(*os.File)
	if !ok {
		return 0
	}

	if f == nil {
		return 0
	}

	names, err := f.Readdirnames(1)
	if err != nil {
		return 0
	}

	l.Push(lua.LString(names[0]))
	return 1
}
