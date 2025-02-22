// file.go
package fs

import (
	fsapi "github.com/ponyruntime/pony/api/fs"
	lua "github.com/yuin/gopher-lua"
)

type File struct {
	file fsapi.File
}

func CheckFile(l *lua.LState, n int) *File {
	ud := l.CheckUserData(n)
	if v, ok := ud.Value.(*File); ok {
		return v
	}
	l.ArgError(n, "file expected")
	return nil
}

func fileRead(l *lua.LState) int  { return 0 }
func fileWrite(l *lua.LState) int { return 0 }
func fileSeek(l *lua.LState) int  { return 0 }
func fileClose(l *lua.LState) int { return 0 }
func fileStat(l *lua.LState) int  { return 0 }

func registerFile(l *lua.LState) {
	mt := l.NewTypeMetatable("File")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"read":  fileRead,
		"write": fileWrite,
		"seek":  fileSeek,
		"close": fileClose,
		"stat":  fileStat,
	}))
}
