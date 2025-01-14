package time

import lua "github.com/yuin/gopher-lua"

// Duration constants in nanoseconds
const (
	Nanosecond  = 1
	Microsecond = 1000 * Nanosecond
	Millisecond = 1000 * Microsecond
	Second      = 1000 * Millisecond
	Minute      = 60 * Second
	Hour        = 60 * Minute
)

// NewTimeModule creates and returns a new instance of the time Module
func NewTimeModule() *Module {
	return &Module{}
}

// Name returns the module's name
func (m *Module) Name() string {
	return "time"
}

// Loader registers the module's functions and constants into Lua state
func (m *Module) Loader(l *lua.LState) int {
	mod := l.NewTable()

	registerDuration(l, mod)
	registerLocation(l, mod)
	registerTime(l, mod)

	l.Push(mod)
	return 1
}
