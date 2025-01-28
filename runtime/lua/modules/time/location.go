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
	l.SetMetatable(ud, l.GetTypeMetatable("Location"))
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
	l.SetMetatable(ud, l.GetTypeMetatable("Location"))
	l.Push(ud)
	return 1
}

// Register the Location methods and create UTC/Local constants
func registerLocation(l *lua.LState, mod *lua.LTable) {
	locationMt := l.NewTypeMetatable("Location")
	l.SetField(locationMt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"string": locationString,
	}))
	l.SetField(locationMt, "__tostring", l.NewFunction(locationString))

	// Register location-related functions
	l.SetField(mod, "load_location", l.NewFunction(loadLocation))
	l.SetField(mod, "fixed_zone", l.NewFunction(fixedZone))

	// Create and set UTC constant
	utcUD := l.NewUserData()
	utcUD.Value = &Location{location: time.UTC}
	l.SetMetatable(utcUD, locationMt)
	l.SetField(mod, "utc", utcUD)

	// Create and set Local constant
	localUD := l.NewUserData()
	localUD.Value = &Location{location: time.Local}
	l.SetMetatable(localUD, locationMt)
	l.SetField(mod, "localtz", localUD)
}
