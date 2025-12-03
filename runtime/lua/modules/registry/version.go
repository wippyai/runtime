package registry

import (
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// registerVersionType registers the Version type and methods
func registerVersionType(l *lua.LState) {
	value.RegisterMethods(l, versionMetatable, map[string]lua.LGFunction{
		"id":       versionID,
		"previous": versionPrevious,
		"next":     versionNext,
		"string":   versionString,
	})
}

// versionID returns the ID of a version
func versionID(l *lua.LState) int {
	ud := l.CheckUserData(1)
	version, ok := ud.Value.(registry.Version)
	if !ok {
		l.ArgError(1, "version expected")
		return 0
	}

	l.Push(lua.LNumber(version.ID()))
	return 1
}

// versionPrevious returns the previous version
func versionPrevious(l *lua.LState) int {
	ud := l.CheckUserData(1)
	version, ok := ud.Value.(registry.Version)
	if !ok {
		l.ArgError(1, "version expected")
		return 0
	}

	prev := version.Previous()
	if prev == nil {
		l.Push(lua.LNil)
		return 1
	}

	ud = wrapVersion(l, prev)
	l.Push(ud)
	return 1
}

// versionNext returns the next version
func versionNext(l *lua.LState) int {
	ud := l.CheckUserData(1)
	version, ok := ud.Value.(registry.Version)
	if !ok {
		l.ArgError(1, "version expected")
		return 0
	}

	next, exists := version.Next()
	if !exists {
		l.Push(lua.LNil)
		return 1
	}

	ud = wrapVersion(l, next)
	l.Push(ud)
	return 1
}

// versionString returns a string representation of the version
func versionString(l *lua.LState) int {
	ud := l.CheckUserData(1)
	version, ok := ud.Value.(registry.Version)
	if !ok {
		l.ArgError(1, "version expected")
		return 0
	}

	l.Push(lua.LString(version.String()))
	return 1
}
