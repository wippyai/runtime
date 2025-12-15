// Package sandbox provides the eval.sandbox module for deterministic simulation.
// It enables manual process stepping with controllable time for testing and replay.
package sandbox

import (
	"sync"

	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/evalhost"
	"github.com/wippyai/runtime/runtime/lua/security"
	lua "github.com/yuin/gopher-lua"
)

var (
	moduleTable *lua.LTable
	initOnce    sync.Once
)

const (
	clockTypeName   = "eval.sandbox.Clock"
	processTypeName = "eval.sandbox.Process"
	programTypeName = "eval.sandbox.Program"
)

// Module is the eval_sandbox module definition.
var Module = &luaapi.ModuleDef{
	Name:        "eval_sandbox",
	Description: "Deterministic simulation with controllable time",
	Class:       []string{luaapi.ClassProcess, luaapi.ClassDeterministic},
	Build:       buildModule,
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
	initOnce.Do(func() {
		moduleTable = createModuleTable()
		value.RegisterTypeMethods(nil, clockTypeName, nil, clockMethods)
		value.RegisterTypeMethods(nil, processTypeName, nil, processMethods)
		value.RegisterTypeMethods(nil, programTypeName, nil, programMethods)
	})

	return moduleTable, []luaapi.YieldType{
		{Sample: &CompileYield{}, CmdID: evalhost.Compile},
		{Sample: &CreateProcessYield{}, CmdID: evalhost.CreateProcess},
	}
}

func createModuleTable() *lua.LTable {
	mod := lua.CreateTable(0, 3)
	mod.RawSetString("clock", lua.LGoFunc(newClock))
	mod.RawSetString("compile", lua.LGoFunc(compileFunc))
	mod.RawSetString("create_process", lua.LGoFunc(createProcessFunc))
	mod.Immutable = true
	return mod
}

// newClock creates a new mock clock: sandbox.clock(start_time?) -> Clock
func newClock(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "no context").WithKind(lua.Internal))
		return 2
	}

	if !security.IsAllowed(ctx, "eval.sandbox", "", nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: eval.sandbox").
			WithKind(lua.PermissionDenied).WithRetryable(false))
		return 2
	}

	var startNano int64
	if l.GetTop() >= 1 {
		startNano = int64(l.CheckNumber(1))
	}

	clock := NewMockClock(startNano)
	value.PushTypedUserData(l, clock, clockTypeName)
	return 1
}

// compileFunc compiles source for sandbox use: sandbox.compile(source, method, options?) -> Program, err
func compileFunc(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "no context").WithKind(lua.Internal))
		return 2
	}

	if !security.IsAllowed(ctx, "eval.compile", "", nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: eval.compile").
			WithKind(lua.PermissionDenied).WithRetryable(false))
		return 2
	}

	source := l.CheckString(1)
	method := l.OptString(2, "")

	var modules []string
	if l.GetTop() >= 3 && l.Get(3).Type() == lua.LTTable {
		opts := l.CheckTable(3)
		if modulesVal := opts.RawGetString("modules"); modulesVal.Type() == lua.LTTable {
			modulesTable := modulesVal.(*lua.LTable)
			modulesTable.ForEach(func(_, v lua.LValue) {
				if s, ok := v.(lua.LString); ok {
					modules = append(modules, string(s))
				}
			})
		}
	}

	yield := AcquireCompileYield()
	yield.Source = source
	yield.Method = method
	yield.Modules = modules

	l.Push(yield)
	return -1
}

// createProcessFunc creates a steppable process: sandbox.create_process(program, clock?) -> Process, err
func createProcessFunc(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "no context").WithKind(lua.Internal))
		return 2
	}

	if !security.IsAllowed(ctx, "eval.sandbox", "", nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: eval.sandbox").
			WithKind(lua.PermissionDenied).WithRetryable(false))
		return 2
	}

	// Get program from first argument
	programUD := l.CheckUserData(1)
	var program *evalhost.Program

	// Try ProgramWrapper first
	if wrapper, ok := programUD.Value.(*ProgramWrapper); ok {
		program = wrapper.Program()
	} else if prog, ok := programUD.Value.(*evalhost.Program); ok {
		program = prog
	}

	if program == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "invalid program").
			WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	yield := AcquireCreateProcessYield()
	yield.Program = program
	yield.Ctx = ctx

	// Optional clock from second argument
	if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTUserData {
		clockUD := l.CheckUserData(2)
		if clock, ok := clockUD.Value.(*MockClock); ok {
			yield.Clock = clock
		}
	}

	l.Push(yield)
	return -1
}

// Program methods
var programMethods = map[string]lua.LGoFunc{
	"method":  programMethod,
	"modules": programModules,
}

func checkProgram(l *lua.LState, idx int) *ProgramWrapper {
	ud := l.CheckUserData(idx)
	if p, ok := ud.Value.(*ProgramWrapper); ok {
		return p
	}
	l.ArgError(idx, "eval.sandbox.Program expected")
	return nil
}

func programMethod(l *lua.LState) int {
	p := checkProgram(l, 1)
	if p == nil {
		return 0
	}
	l.Push(lua.LString(p.program.Method()))
	return 1
}

func programModules(l *lua.LState) int {
	p := checkProgram(l, 1)
	if p == nil {
		return 0
	}
	modules := p.program.Modules()
	tbl := l.CreateTable(len(modules), 0)
	for i, m := range modules {
		tbl.RawSetInt(i+1, lua.LString(m))
	}
	l.Push(tbl)
	return 1
}
