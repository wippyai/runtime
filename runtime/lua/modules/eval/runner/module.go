// Package runner provides the eval.runner module for executing untrusted Lua code.
// Code execution is delegated to the dispatcher which handles yields internally.
package runner

import (
	"sync"

	"github.com/wippyai/runtime/api/payload"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/evalhost"
	"github.com/wippyai/runtime/runtime/lua/security"
	lua "github.com/yuin/gopher-lua"
)

var (
	moduleTable      *lua.LTable
	registration     *luaapi.Registration
	programMetatable *lua.LTable
	initOnce         sync.Once
)

const programTypeName = "eval.runner.Program"

// Module is the eval_runner module definition.
var Module = &luaapi.ModuleDef{
	Name:        "eval_runner",
	Description: "Execute untrusted Lua code via dispatcher",
	Class:       []string{luaapi.ClassProcess, luaapi.ClassNondeterministic},
	Build:       buildModule,
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
	initOnce.Do(func() {
		moduleTable = createModuleTable()
		programMetatable = value.RegisterTypeMethods(nil, programTypeName, nil, programMethods)
	})

	return moduleTable, []luaapi.YieldType{
		{Sample: &CompileYield{}, CmdID: evalhost.CmdCompile},
		{Sample: &RunYield{}, CmdID: evalhost.CmdRun},
	}
}

func createModuleTable() *lua.LTable {
	mod := lua.CreateTable(0, 2)
	mod.RawSetString("compile", lua.LGoFunc(compileFunc))
	mod.RawSetString("run", lua.LGoFunc(runFunc))
	mod.Immutable = true
	return mod
}

// compileFunc is runner.compile(source, method, options?) -> Program
func compileFunc(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "no context").WithKind(lua.KindInternal))
		return 2
	}

	if !security.IsAllowed(ctx, "eval.compile", "", nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: eval.compile").
			WithKind(lua.KindPermissionDenied).WithRetryable(false))
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

// runFunc is runner.run(config) -> result
func runFunc(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "no context").WithKind(lua.KindInternal))
		return 2
	}

	if !security.IsAllowed(ctx, "eval.run", "", nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: eval.run").
			WithKind(lua.KindPermissionDenied).WithRetryable(false))
		return 2
	}

	config := l.CheckTable(1)

	source := ""
	if v := config.RawGetString("source"); v.Type() == lua.LTString {
		source = string(v.(lua.LString))
	}

	method := ""
	if v := config.RawGetString("method"); v.Type() == lua.LTString {
		method = string(v.(lua.LString))
	}

	var modules []string
	if v := config.RawGetString("modules"); v.Type() == lua.LTTable {
		v.(*lua.LTable).ForEach(func(_, mv lua.LValue) {
			if s, ok := mv.(lua.LString); ok {
				modules = append(modules, string(s))
			}
		})
	}

	var args payload.Payloads
	if v := config.RawGetString("args"); v.Type() == lua.LTTable {
		v.(*lua.LTable).ForEach(func(_, av lua.LValue) {
			args = append(args, payload.NewPayload(av, payload.Lua))
		})
	}

	contextVals := make(map[string]any)
	if v := config.RawGetString("context"); v.Type() == lua.LTTable {
		v.(*lua.LTable).ForEach(func(k, cv lua.LValue) {
			if ks, ok := k.(lua.LString); ok {
				contextVals[string(ks)] = value.ToGoAny(cv)
			}
		})
	}

	yield := AcquireRunYield()
	yield.Source = source
	yield.Method = method
	yield.Args = args
	yield.Modules = modules
	yield.Context = contextVals

	l.Push(yield)
	return -1
}
