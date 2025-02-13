package loadlib

import (
	lua "github.com/yuin/gopher-lua"
)

// OpenRestrictedPackage is our replacement for lua.OpenPackage
func OpenRestrictedPackage(L *lua.LState) int {
	// Spawn the package table
	packagemod := L.RegisterModule(lua.LoadLibName, packageFuncs)

	// Set up the preload table
	L.SetField(packagemod, "preload", L.NewTable())

	// Set up the single preload loader
	loaders := L.CreateTable(1, 0)
	L.RawSetInt(loaders, 1, L.NewFunction(preloadLoader))
	L.SetField(packagemod, "loaders", loaders)
	L.SetField(L.Get(lua.RegistryIndex), "_LOADERS", loaders)

	// Set up the loaded table
	loaded := L.NewTable()
	L.SetField(packagemod, "loaded", loaded)
	L.SetField(L.Get(lua.RegistryIndex), "_LOADED", loaded)

	// Empty paths
	L.SetField(packagemod, "path", lua.LString(""))
	L.SetField(packagemod, "cpath", lua.LString(""))

	L.Push(packagemod)
	return 1
}

// Package functions map
var packageFuncs = map[string]lua.LGFunction{
	"loadlib": restrictedLoadLib,
	"seeall":  seeAll,
}

// restrictedLoadLib is our restricted version of loadlib
func restrictedLoadLib(L *lua.LState) int {
	name := L.CheckString(1)
	L.Push(lua.LString("cannot load module '" + name + "': loadlib disabled"))
	return 1
}

// preloadLoader only checks the preload table
func preloadLoader(L *lua.LState) int {
	name := L.CheckString(1)
	preload := L.GetField(L.GetField(L.Get(lua.EnvironIndex), "package"), "preload")
	if _, ok := preload.(*lua.LTable); !ok {
		L.RaiseError("package.preload must be a table")
	}
	lv := L.GetField(preload, name)
	if lv == lua.LNil {
		L.Push(lua.LString("module '" + name + "' not found in package.preload"))
		return 1
	}
	L.Push(lv)
	return 1
}

// seeall implements package.seeall
func seeAll(L *lua.LState) int {
	mod := L.CheckTable(1)
	mt := L.GetMetatable(mod)
	if mt == lua.LNil {
		mt = L.CreateTable(0, 1)
		L.SetMetatable(mod, mt)
	}
	L.SetField(mt, "__index", L.Get(lua.GlobalsIndex))
	return 0
}
