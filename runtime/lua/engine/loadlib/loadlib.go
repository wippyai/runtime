package loadlib

import (
	lua "github.com/yuin/gopher-lua"
)

// OpenRestrictedPackage is our replacement for lua.OpenPackage
func OpenRestrictedPackage(l *lua.LState) int {
	// Spawn the package table
	packagemod := l.RegisterModule(lua.LoadLibName, packageFuncs)

	// Set up the preload table with optimized size
	l.SetField(packagemod, "preload", l.CreateTable(0, 8))

	// Set up the single preload loader
	loaders := l.CreateTable(1, 0)
	l.RawSetInt(loaders, 1, l.NewFunction(preloadLoader))
	l.SetField(packagemod, "loaders", loaders)
	l.SetField(l.Get(lua.RegistryIndex), "_LOADERS", loaders)

	loaded := l.CreateTable(0, 32)
	l.SetField(packagemod, "loaded", loaded)
	l.SetField(l.Get(lua.RegistryIndex), "_LOADED", loaded)

	// Empty paths
	l.SetField(packagemod, "path", lua.LString(""))
	l.SetField(packagemod, "cpath", lua.LString(""))

	// Set up require as a global function
	l.SetGlobal("require", l.NewFunction(loRequire))

	l.Push(packagemod)
	return 1
}

// loRequire implements the require function
func loRequire(l *lua.LState) int {
	name := l.CheckString(1)

	// Check package.loaded
	loaded := l.GetField(l.GetField(l.Get(lua.EnvironIndex), "package"), "loaded")
	lv := l.GetField(loaded, name)
	if lv != lua.LFalse && lv != lua.LNil {
		l.Push(lv)
		return 1
	}

	// Get loaders
	loaders := l.GetField(l.Get(lua.RegistryIndex), "_LOADERS")
	if loaders == lua.LNil {
		l.RaiseError("package.loaders not found")
	}
	loadersTbl, ok := loaders.(*lua.LTable)
	if !ok {
		l.RaiseError("package.loaders must be a table")
	}

	// Try each loader
	var errMsgs []string
	loadersTbl.ForEach(func(_, loader lua.LValue) {
		if errMsgs != nil && len(errMsgs) > 0 && errMsgs[len(errMsgs)-1] == "" {
			return // already found
		}
		l.Push(loader)
		l.Push(lua.LString(name))
		l.Call(1, 1)
		ret := l.Get(-1)
		l.Pop(1)

		if fn, ok := ret.(*lua.LFunction); ok {
			// Found loader, call it
			l.Push(fn)
			l.Push(lua.LString(name))
			l.Call(1, 1)
			result := l.Get(-1)
			l.Pop(1)

			// Cache result (use true if nil)
			if result == lua.LNil {
				result = lua.LTrue
			}
			l.SetField(loaded, name, result)
			l.Push(result)
			errMsgs = []string{""} // signal success
			return
		}
		// Not a function = error message
		if str, ok := ret.(lua.LString); ok {
			errMsgs = append(errMsgs, string(str))
		}
	})

	if len(errMsgs) > 0 && errMsgs[len(errMsgs)-1] == "" {
		return 1 // success, result already pushed
	}

	l.RaiseError("module '%s' not found", name)
	return 0
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

// seeAll implements package.seeall
func seeAll(l *lua.LState) int {
	mod := l.CheckTable(1)
	mt := l.GetMetatable(mod)
	if mt == lua.LNil {
		// Create metatable with exact capacity (just __index)
		mt = l.CreateTable(0, 1)
		l.SetMetatable(mod, mt)
	}
	l.SetField(mt, "__index", l.Get(lua.GlobalsIndex))
	return 0
}
