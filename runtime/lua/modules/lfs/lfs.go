///
/// Reference: https://lunarmodules.github.io/luafilesystem/manual.html#introduction

package lfs

import (
	"os"

	lua "github.com/yuin/gopher-lua"
)

const globalFnName = "___currdir"

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

// Loader loads the module into the given Lua state.
func (m *Module) Loader(l *lua.LState) int {
	t := l.NewTable()

	api := map[string]lua.LGFunction{
		"attributes":        apiAttributes,
		"chdir":             apiChdir,
		"lock_dir":          apiLockDir,
		"currentdir":        apiCurrentDir,
		"dir":               apiDir,
		"lock":              apiLock,
		"link":              apiLink,
		"mkdir":             apiMkdir,
		"rmdir":             apiRmdir,
		"setmode":           apiSetMode,
		"symlinkattributes": apiSymlinkAttributes,
		"touch":             apiTouch,
		"unlock":            apiUnlock,
	}

	// TODO: is it safe to omit error handling here?
	// case1: cwd does not exist
	dir, _ := os.Getwd() // generally speaking we can isolate whole concept of root in here and simply use our mocked fs
	l.SetGlobal(globalFnName, lua.LString(dir))

	l.SetFuncs(t, api)
	l.Push(t)

	return 1
}
