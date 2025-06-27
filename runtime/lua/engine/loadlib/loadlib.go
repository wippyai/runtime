package loadlib

import (
	lua "github.com/yuin/gopher-lua"
)

// OpenRestrictedPackage is our replacement for lua.OpenPackage
func OpenRestrictedPackage(l *lua.LState) int {
	// Spawn the package table
	packagemod := l.RegisterModule(lua.LoadLibName, packageFuncs)

	// Set up the preload table
	l.SetField(packagemod, "preload", l.NewTable())

	// Set up the single preload loader
	loaders := l.CreateTable(1, 0)
	l.RawSetInt(loaders, 1, l.NewFunction(preloadLoader))
	l.SetField(packagemod, "loaders", loaders)
	l.SetField(l.Get(lua.RegistryIndex), "_LOADERS", loaders)

	// Set up the loaded table
	loaded := l.NewTable()
	l.SetField(packagemod, "loaded", loaded)
	l.SetField(l.Get(lua.RegistryIndex), "_LOADED", loaded)

	// Empty paths
	l.SetField(packagemod, "path", lua.LString(""))
	l.SetField(packagemod, "cpath", lua.LString(""))

	l.Push(packagemod)
	return 1
}

// Package functions map
//
// ok for now
var packageFuncs = map[string]lua.LGFunction{
	"loadlib": restrictedLoadLib,
	"seeall":  seeAll,
}

// restrictedLoadLib is our restricted version of loadlib
func restrictedLoadLib(l *lua.LState) int {
	name := l.CheckString(1)
	l.Push(lua.LString("cannot load module '" + name + "': loadlib disabled"))
	return 1
}

// preloadLoader only checks the preload table
func preloadLoader(l *lua.LState) int {
	name := l.CheckString(1)
	preload := l.GetField(l.GetField(l.Get(lua.EnvironIndex), "package"), "preload")
	if _, ok := preload.(*lua.LTable); !ok {
		l.RaiseError("package.preload must be a table")
	}
	lv := l.GetField(preload, name)
	if lv == lua.LNil {
		l.Push(lua.LString("module '" + name + "' not found in package.preload"))
		return 1
	}
	l.Push(lv)
	return 1
}

// see all implements package.see all
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
