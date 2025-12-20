package registry

import (
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

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

	value.PushTypedUserData(l, prev, typeVersion)
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
