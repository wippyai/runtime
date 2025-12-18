// Package eval provides the eval module for dynamic Lua code execution.
package eval

import (
	"github.com/wippyai/runtime/api/payload"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/evalhost"
	lua "github.com/yuin/gopher-lua"
)

const (
	programTypeName = "eval.Program"
	sandboxTypeName = "eval.Sandbox"
)

var sandboxMetatable *lua.LTable

func init() {
	value.RegisterTypeMethods(nil, programTypeName, nil, programMethods)
	sandboxMetatable = value.RegisterTypeMethods(nil, sandboxTypeName, nil, sandboxMethods)
}

// Module is the eval module definition.
var Module = &luaapi.ModuleDef{
	Name:        "eval",
	Description: "Dynamic Lua code compilation and execution",
	Class:       []string{luaapi.ClassProcess, luaapi.ClassNondeterministic},
	Types:       ModuleTypes,
	Build:       buildModule,
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
	mod := lua.CreateTable(0, 3)
	mod.RawSetString("compile", lua.LGoFunc(compileFunc))
	mod.RawSetString("run", lua.LGoFunc(runFunc))
	mod.RawSetString("sandbox", lua.LGoFunc(sandboxFunc))
	mod.Immutable = true

	yields := []luaapi.YieldType{
		{Sample: &CompileYield{}, CmdID: evalhost.Compile},
		{Sample: &RunYield{}, CmdID: evalhost.Run},
	}

	return mod, yields
}

// compileFunc is eval.compile(source, method, options?) -> Program
func compileFunc(l *lua.LState) int {
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

// runFunc is eval.run(config) -> result
func runFunc(l *lua.LState) int {
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

// sandboxFunc is eval.sandbox(source_or_id, options?) -> Sandbox
func sandboxFunc(l *lua.LState) int {
	// Check if first arg is string (source or ID)
	arg1 := l.Get(1)
	if arg1.Type() != lua.LTString {
		l.ArgError(1, "string expected (source code or prototype ID)")
		return 0
	}

	sourceOrID := string(arg1.(lua.LString))

	var modules []string
	if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTTable {
		opts := l.CheckTable(2)
		if modulesVal := opts.RawGetString("modules"); modulesVal.Type() == lua.LTTable {
			modulesTable := modulesVal.(*lua.LTable)
			modulesTable.ForEach(func(_, v lua.LValue) {
				if s, ok := v.(lua.LString); ok {
					modules = append(modules, string(s))
				}
			})
		}
	}

	// Create sandbox userdata
	sandbox := &Sandbox{
		sourceOrID: sourceOrID,
		modules:    modules,
	}

	ud := l.NewUserData()
	ud.Value = sandbox
	ud.Metatable = sandboxMetatable
	l.Push(ud)
	return 1
}
