package registry

import (
	regapi "github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// registerVersionType registers the Version type and methods
func (m *Module) registerVersionType(l *lua.LState) {
	value.RegisterMethods(l, versionMetatable, map[string]lua.LGFunction{
		"id":       versionID,
		"previous": versionPrevious,
		"string":   versionString,
	})
}

// versionID returns the ID of a version
func versionID(l *lua.LState) int {
	// Get version - parameter check, no coroutine needed
	ud := l.CheckUserData(1)
	version, ok := ud.Value.(regapi.Version)
	if !ok {
		l.ArgError(1, "version expected")
		return 0
	}

	// Simple accessor, no coroutine needed
	l.Push(lua.LNumber(version.ID()))
	return 1
}

// versionPrevious returns the previous version
func versionPrevious(l *lua.LState) int {
	// Get version - parameter check, no coroutine needed
	ud := l.CheckUserData(1)
	version, ok := ud.Value.(regapi.Version)
	if !ok {
		l.ArgError(1, "version expected")
		return 0
	}

	// Simple accessor, no coroutine needed
	prev := version.Previous()
	if prev == nil {
		l.Push(lua.LNil)
		return 1
	}

	// Create userdata for Version
	ud = wrapVersion(l, prev)
	l.Push(ud)
	return 1
}

// versionString returns a string representation of the version
func versionString(l *lua.LState) int {
	// Get version - parameter check, no coroutine needed
	ud := l.CheckUserData(1)
	version, ok := ud.Value.(regapi.Version)
	if !ok {
		l.ArgError(1, "version expected")
		return 0
	}

	// Simple accessor, no coroutine needed
	l.Push(lua.LString(version.String()))
	return 1
}
