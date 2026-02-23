// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"sync"

	lua "github.com/wippyai/go-lua"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
)

// moduleCache holds built module data.
type moduleCache struct {
	value  lua.LValue
	table  *lua.LTable
	yields []luaapi.YieldType
}

var (
	moduleCacheMu sync.RWMutex
	moduleBuilt   = make(map[*luaapi.ModuleDef]*moduleCache)
)

// buildModule builds a module once and caches the result.
func buildModule(m *luaapi.ModuleDef) *moduleCache {
	moduleCacheMu.RLock()
	if cached, ok := moduleBuilt[m]; ok {
		moduleCacheMu.RUnlock()
		return cached
	}
	moduleCacheMu.RUnlock()

	moduleCacheMu.Lock()
	defer moduleCacheMu.Unlock()

	// Double check after acquiring write lock
	if cached, ok := moduleBuilt[m]; ok {
		return cached
	}

	cache := &moduleCache{}

	if m.BuildValue != nil {
		cache.value, cache.yields = m.BuildValue()
		if tbl, ok := cache.value.(*lua.LTable); ok {
			cache.table = tbl
		}
	} else if m.Build != nil {
		cache.table, cache.yields = m.Build()
		cache.value = cache.table
	}

	moduleBuilt[m] = cache
	return cache
}

// LoadModuleDef loads a module into LState's _G.
func LoadModuleDef(l *lua.LState, m *luaapi.ModuleDef) []luaapi.YieldType {
	cache := buildModule(m)
	l.SetGlobal(m.Name, cache.value)
	return cache.yields
}

// ModuleLoader returns a loader function for the module.
func ModuleLoader(m *luaapi.ModuleDef) func(l *lua.LState) int {
	return func(l *lua.LState) int {
		cache := buildModule(m)
		l.Push(cache.value)
		return 1
	}
}

// ModuleValue returns the cached value for a module.
func ModuleValue(m *luaapi.ModuleDef) lua.LValue {
	cache := buildModule(m)
	return cache.value
}

// ModuleTable returns the cached table for a module.
func ModuleTable(m *luaapi.ModuleDef) *lua.LTable {
	cache := buildModule(m)
	return cache.table
}

// ModuleYields returns the cached yields for a module.
func ModuleYields(m *luaapi.ModuleDef) []luaapi.YieldType {
	cache := buildModule(m)
	return cache.yields
}

// ModuleInfo returns module metadata.
func ModuleInfo(m *luaapi.ModuleDef) luaapi.ModuleInfo {
	return luaapi.ModuleInfo{Name: m.Name, Description: m.Description, Class: m.Class}
}

// ModuleWrapper wraps ModuleDef to implement luaapi.Module interface.
type ModuleWrapper struct {
	Def *luaapi.ModuleDef
}

// WrapModule wraps a ModuleDef to implement Module interface.
func WrapModule(def *luaapi.ModuleDef) luaapi.Module {
	return &ModuleWrapper{Def: def}
}

// Info implements luaapi.Module.
func (w *ModuleWrapper) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{Name: w.Def.Name, Description: w.Def.Description, Class: w.Def.Class}
}

// Value implements luaapi.Module.
func (w *ModuleWrapper) Value() lua.LValue {
	return buildModule(w.Def).value
}

// Yields implements luaapi.Module.
func (w *ModuleWrapper) Yields() []luaapi.YieldType {
	return buildModule(w.Def).yields
}
