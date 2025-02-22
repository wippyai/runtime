package fs

import (
	fsapi "github.com/ponyruntime/pony/api/fs"
	lua "github.com/yuin/gopher-lua"
	"log"
)

// Module represents a fs Lua module
type Module struct{}

// NewFSModule creates and returns a new instance of the fs Module
func NewFSModule() *Module {
	return &Module{}
}

// Name returns the module's name
func (m *Module) Name() string {
	return "fs"
}

// Loader loads the module into the given Lua state
func (m *Module) Loader(l *lua.LState) int {
	t := l.NewTable()

	// Register core functions
	api := map[string]lua.LGFunction{
		"default": apiDefault,
		"get":     apiGet,
	}

	l.SetFuncs(t, api)
	l.Push(t)
	return 1
}

func apiDefault(l *lua.LState) int {
	reg := fsapi.FromContext(l.Context())
	if reg == nil {
		l.RaiseError("no filesystem registry in context")
		return 0
	}

	fs, ok := reg.GetDefaultFS()
	if !ok {
		l.RaiseError("no default filesystem available")
		return 0
	}

	log.Printf("Default filesystem: %s", fs)
	// Create and return default filesystem instance
	instance := l.NewTable()
	// TODO: Set methods on instance using fs
	l.Push(instance)
	return 1
}

func apiGet(l *lua.LState) int {
	name := l.CheckString(1)
	if name == "" {
		l.ArgError(1, "filesystem name required")
		return 0
	}

	reg := fsapi.FromContext(l.Context())
	if reg == nil {
		l.RaiseError("no filesystem registry in context")
		return 0
	}

	fs, ok := reg.GetFS(name)
	if !ok {
		l.RaiseError("filesystem not found: %s", name)
		return 0
	}

	log.Printf("Getting filesystem: %v", fs)

	// Create and return named filesystem instance
	instance := l.NewTable()
	// TODO: Set methods on instance using fs
	l.Push(instance)
	return 1
}
