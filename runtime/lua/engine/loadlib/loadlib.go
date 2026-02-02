package loadlib

import (
	"sync"

	lua "github.com/wippyai/go-lua"
)

var (
	packageModule *lua.LTable
	packageOnce   sync.Once
)

func initPackageModule() {
	packageModule = lua.CreateTable(0, 4)
	packageModule.RawSetString("loadlib", lua.LGoFunc(restrictedLoadLib))
	packageModule.RawSetString("seeall", lua.LGoFunc(seeAll))
	packageModule.RawSetString("path", lua.LString(""))
	packageModule.RawSetString("cpath", lua.LString(""))
	packageModule.Immutable = true
}

// OpenRestrictedPackage is wippy's minimal package implementation.
// Modules are in _G, so require() just looks them up there.
// Zero per-state allocation after warmup.
func OpenRestrictedPackage(l *lua.LState) int {
	packageOnce.Do(initPackageModule)

	l.SetGlobal(lua.LoadLibName, packageModule)
	l.SetGlobal("require", lua.LGoFunc(loRequire))

	l.Push(packageModule)
	return 1
}

// loRequire implements require() by looking up _G directly.
// Modules are pre-loaded into _G by ModuleDef, no preload/loaded tables needed.
func loRequire(l *lua.LState) int {
	name := l.CheckString(1)

	// Direct _G lookup - modules are already there
	result := l.GetGlobal(name)
	if result == lua.LNil {
		l.RaiseError("module '%s' not found", name)
	}

	l.Push(result)
	return 1
}

// restrictedLoadLib returns an error for loadlib calls.
func restrictedLoadLib(l *lua.LState) int {
	name := l.CheckString(1)
	l.Push(lua.LString("cannot load module '" + name + "': loadlib disabled"))
	return 1
}

// seeAll implements package.seeall.
func seeAll(l *lua.LState) int {
	mod := l.CheckTable(1)
	mt := l.GetMetatable(mod)
	if mt == lua.LNil {
		mt = l.CreateTable(0, 1)
		l.SetMetatable(mod, mt)
	}
	l.SetField(mt, "__index", l.Get(lua.GlobalsIndex))
	return 0
}
