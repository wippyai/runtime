///
/// Reference: https://lunarmodules.github.io/luafilesystem/manual.html#introduction

package lfs

import (
	"os"

	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Module represents a lfs Lua module.
type Module struct {
	currDir string
	log     *zap.Logger
}

// NewLFSModule creates and returns a new instance of the lfs Module.
func NewLFSModule(log *zap.Logger) *Module {
	// TODO: is it safe to omit error handling here?
	// case1: cwd does not exist
	dir, _ := os.Getwd()
	return &Module{
		currDir: dir,
		log:     log,
	}
}

// Name returns the module's name.
func (m *Module) Name() string {
	return "lfs"
}

func (m *Module) Loader(l *lua.LState) int {
	t := l.NewTable()

	api := map[string]lua.LGFunction{
		"attributes":        m.apiAttributes,
		"chdir":             m.apiChdir,
		"lock_dir":          m.apiLockdir,
		"currentdir":        m.apiCurrentdir,
		"dir":               m.apiDir,
		"lock":              m.apiLock,
		"link":              m.apiLink,
		"mkdir":             m.apiMkdir,
		"rmdir":             m.apiRmdir,
		"setmode":           m.apiSetmode,
		"symlinkattributes": m.apiSymlinkattributes,
		"touch":             m.apiTouch,
		"unlock":            m.apiUnlock,
		// to close a file in the userdata
		"close": m.close,
	}

	l.SetFuncs(t, api)
	l.Push(t)
	return 1
}
