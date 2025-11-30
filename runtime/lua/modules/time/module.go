package time

import (
	stdtime "time"

	lua "github.com/yuin/gopher-lua"
)

// Type definitions for time module

type Time struct {
	time stdtime.Time
}

type Duration struct {
	duration stdtime.Duration
}

type Location struct {
	location *stdtime.Location
}

// Pre-created location values (cached, immutable)
var (
	utcLocation   = &Location{location: stdtime.UTC}
	localLocation = &Location{location: stdtime.Local}
)

// Bind is deprecated - use Module.Loader instead.
// Kept for backward compatibility.
func Bind(l *lua.LState) {
	BindYields(l)
}
