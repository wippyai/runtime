///
/// Reference: https://lunarmodules.github.io/luafilesystem/manual.html#introduction

package lfs

import (
	"github.com/ponyruntime/go-lua"
)

// Module represents a lfs Lua module.
type Module struct{}

// NewLFSModule creates and returns a new instance of the lfs Module.
func NewLFSModule() *Module {
	return &Module{}
}

// Name returns the module's name.
func (m *Module) Name() string {
	return "lfs"
}

func (m *Module) Loader(l *lua.LState) int {
	t := l.NewTable()

	api := map[string]lua.LGFunction{
		"attributes":        apiAttributes,
		"chdir":             apiChdir,
		"lock_dir":          apiLockdir,
		"currentdir":        apiCurrentdir,
		"dir":               apiDir,
		"lock":              apiLock,
		"link":              apiLink,
		"mkdir":             apiMkdir,
		"rmdir":             apiRmdir,
		"setmode":           apiSetmode,
		"symlinkattributes": apiSymlinkattributes,
		"touch":             apiTouch,
		"unlock":            apiUnlock,
	}

	l.SetFuncs(t, api)
	l.Push(t)
	return 1
}
