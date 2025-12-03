package loadlib

import (
	lua "github.com/yuin/gopher-lua"
)

// OpenRestrictedPackage is our replacement for lua.OpenPackage.
// Optimized for wippy: require() uses registry for fast table access.
func OpenRestrictedPackage(l *lua.LState) int {
	packagemod := l.RegisterModule(lua.LoadLibName, packageFuncs)

	// Preload table for modules (function or table values)
	preload := l.CreateTable(0, 16)
	l.SetField(packagemod, "preload", preload)

	// Loaded table for caching
	loaded := l.CreateTable(0, 32)
	l.SetField(packagemod, "loaded", loaded)

	// Store in registry for fast access (RawGetString)
	reg := l.Get(lua.RegistryIndex).(*lua.LTable)
	reg.RawSetString("_PRELOAD", preload)
	reg.RawSetString("_LOADED", loaded)

	// Empty paths (no file system access)
	l.SetField(packagemod, "path", lua.LString(""))
	l.SetField(packagemod, "cpath", lua.LString(""))

	// require function using LGoFunc (zero allocation)
	l.SetGlobal("require", lua.LGoFunc(loRequire))

	l.Push(packagemod)
	return 1
}

// loRequire implements the require function.
// Uses registry for fast table access via RawGetString.
func loRequire(l *lua.LState) int {
	name := l.CheckString(1)

	// Fast registry access
	reg := l.Get(lua.RegistryIndex).(*lua.LTable)
	loaded := reg.RawGetString("_LOADED").(*lua.LTable)

	// Check cache first
	if lv := loaded.RawGetString(name); lv != lua.LNil && lv != lua.LFalse {
		l.Push(lv)
		return 1
	}

	// Get from preload
	preload := reg.RawGetString("_PRELOAD").(*lua.LTable)
	value := preload.RawGetString(name)
	if value == lua.LNil {
		l.RaiseError("module '%s' not found", name)
	}

	var result lua.LValue

	switch v := value.(type) {
	case *lua.LTable:
		// Direct table module (optimized wippy path)
		result = v
	case *lua.LFunction:
		// Loader function - call it
		l.Push(v)
		l.Push(lua.LString(name))
		l.Call(1, 1)
		result = l.Get(-1)
		l.Pop(1)
	case lua.LGoFunc:
		// Stateless Go function loader
		l.Push(v)
		l.Push(lua.LString(name))
		l.Call(1, 1)
		result = l.Get(-1)
		l.Pop(1)
	default:
		l.RaiseError("invalid preload value for '%s': expected table or function", name)
	}

	// Cache result (use true if nil)
	if result == lua.LNil {
		result = lua.LTrue
	}
	loaded.RawSetString(name, result)
	l.Push(result)
	return 1
}

// Package functions map
//
// ok for now
var packageFuncs = map[string]lua.LGFunction{
	"loadlib": restrictedLoadLib,
	"seeall":  seeAll,
}

// restrictedLoadLib returns an error for loadlib calls
func restrictedLoadLib(l *lua.LState) int {
	name := l.CheckString(1)
	l.Push(lua.LString("cannot load module '" + name + "': loadlib disabled"))
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
