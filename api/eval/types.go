package eval

import (
	"context"

	"github.com/wippyai/runtime/api/process2"
	"github.com/wippyai/runtime/api/registry"
	lua "github.com/yuin/gopher-lua"
)

// Program represents a compiled Lua program that can be executed multiple times.
type Program interface {
	// Method returns the method name to execute.
	Method() string
	// Modules returns the allowed modules whitelist.
	Modules() []string
	// Proto returns the compiled Lua function prototype.
	Proto() *lua.FunctionProto
}

// Host provides eval compilation and execution services.
type Host interface {
	// Compile compiles Lua source into a reusable Program.
	Compile(ctx context.Context, cmd CompileCmd) (Program, error)

	// Run compiles and executes Lua code, returning the result.
	Run(ctx context.Context, cmd RunCmd) (any, error)

	// CreateProcess creates a process from a Program for sandbox use.
	CreateProcess(ctx context.Context, program Program) (process2.Process, error)

	// CreateProcessFromID creates a process from a prototype ID.
	CreateProcessFromID(ctx context.Context, id registry.ID) (process2.Process, error)
}
