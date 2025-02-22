// fs.go
package fs

import (
	fsapi "github.com/ponyruntime/pony/api/fs"
	lua "github.com/yuin/gopher-lua"
)

type FS struct {
	fs fsapi.FS
}

func CheckFS(l *lua.LState, n int) *FS {
	ud := l.CheckUserData(n)
	if v, ok := ud.Value.(*FS); ok {
		return v
	}
	l.ArgError(n, "filesystem expected")
	return nil
}

func fsChdir(l *lua.LState) int   { return 0 }
func fsPwd(l *lua.LState) int     { return 0 }
func fsOpen(l *lua.LState) int    { return 0 }
func fsStat(l *lua.LState) int    { return 0 }
func fsReadDir(l *lua.LState) int { return 0 }
func fsMkdir(l *lua.LState) int   { return 0 }
func fsRemove(l *lua.LState) int  { return 0 }

func registerFS(l *lua.LState, mod *lua.LTable) {
	mt := l.NewTypeMetatable("FS")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"chdir":   fsChdir,
		"pwd":     fsPwd,
		"open":    fsOpen,
		"stat":    fsStat,
		"readdir": fsReadDir,
		"mkdir":   fsMkdir,
		"remove":  fsRemove,
	}))

	l.SetFuncs(mod, map[string]lua.LGFunction{
		"default": apiDefault,
		"get":     apiGet,
	})
}
