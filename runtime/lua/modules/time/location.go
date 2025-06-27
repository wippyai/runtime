package time

import (
	"time"

	lua "github.com/yuin/gopher-lua"
)

// Location represents a Lua userdata object wrapping time.Location
type Location struct {
	location *time.Location
}

// locationString returns the name of the location
func locationString(l *lua.LState) int {
	ud := l.CheckUserData(1)
	if loc, ok := ud.Value.(*Location); ok {
		l.Push(lua.LString(loc.location.String()))
		return 1
	}
	l.ArgError(1, "location expected")
	return 0
}

// loadLocation implements time.LoadLocation for Lua
func loadLocation(l *lua.LState) int {
	name := l.CheckString(1)

	if name == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("empty location name"))
		return 2
	}

	loc, err := time.LoadLocation(name)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	ud := l.NewUserData()
	ud.Value = &Location{location: loc}
	l.SetMetatable(ud, l.GetTypeMetatable("time.Location"))
	l.Push(ud)
	return 1
}

// fixedZone implements time.FixedZone for Lua
func fixedZone(l *lua.LState) int {
	name := l.CheckString(1)
	offset := l.CheckInt(2)

	loc := time.FixedZone(name, offset)

	ud := l.NewUserData()
	ud.Value = &Location{location: loc}
	l.SetMetatable(ud, l.GetTypeMetatable("time.Location"))
	l.Push(ud)
	return 1
}
