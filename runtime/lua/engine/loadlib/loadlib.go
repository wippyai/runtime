package loadlib

import (
	lua "github.com/yuin/gopher-lua"
)

// packageFuncs contains stateless Go functions for package library.
var packageFuncs = map[string]lua.LGoFunc{
	"loadlib": restrictedLoadLib,
	"seeall":  seeAll,
}

// OpenRestrictedPackage is our replacement for lua.OpenPackage.
// Optimized for wippy: require() uses registry for fast table access.
func OpenRestrictedPackage(l *lua.LState) int {
	// Create package module table
	packagemod := l.CreateTable(0, 6)
	l.SetGlobal(lua.LoadLibName, packagemod)

	// Register functions as LGoFunc (zero allocation)
	for name, fn := range packageFuncs {
		packagemod.RawSetString(name, fn)
	}

	// Preload table for modules (function or table values)
	preload := l.CreateTable(0, 16)
	packagemod.RawSetString("preload", preload)

	// Loaded table for caching
	loaded := l.CreateTable(0, 32)
	packagemod.RawSetString("loaded", loaded)

	// Store in registry for fast access (RawGetString)
	reg := l.Get(lua.RegistryIndex).(*lua.LTable)
	reg.RawSetString("_PRELOAD", preload)
	reg.RawSetString("_LOADED", loaded)

	// Empty paths (no file system access)
	packagemod.RawSetString("path", lua.LString(""))
	packagemod.RawSetString("cpath", lua.LString(""))

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
		l.RaiseError("invalid preload value for '%s': expected table or function, got %T", name, value)
	}

	// Cache result (use true if nil)
	if result == lua.LNil {
		result = lua.LTrue
	}
	loaded.RawSetString(name, result)
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
