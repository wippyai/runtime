// SPDX-License-Identifier: MPL-2.0

// Package eval exposes the eval host to Lua: compile dynamic source under an
// eval policy and run it as a supervised process. There is no in-place run:
// callers monitor or link the spawned process for its result.
package eval

import (
	"context"
	"sync"

	lua "github.com/wippyai/go-lua"
	apihost "github.com/wippyai/runtime/api/host"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/evalhost"
	"github.com/wippyai/runtime/runtime/security"
)

// Name is the module name.
const Name = "eval"

var (
	moduleTable *lua.LTable
	initOnce    sync.Once
)

// Module is the eval module definition.
var Module = &luaapi.ModuleDef{
	Name:        Name,
	Description: "Compile dynamic Lua source and run it as supervised processes",
	Class:       []string{luaapi.ClassProcess, luaapi.ClassNondeterministic},
	Build:       buildModule,
	Types:       ModuleTypes,
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
	initOnce.Do(func() {
		mod := lua.CreateTable(0, 3)
		mod.RawSetString("compile", lua.LGoFunc(moduleCompile))
		mod.RawSetString("spawn", lua.LGoFunc(moduleSpawn))
		mod.RawSetString("evict", lua.LGoFunc(moduleEvict))
		mod.Immutable = true
		moduleTable = mod
		value.RegisterTypeMethods(nil, programTypeName, programMetamethods, programMethods)
	})
	return moduleTable, nil
}

// moduleCompile is eval.compile(source, options?) -> Program, err.
func moduleCompile(l *lua.LState) int {
	source, ok := l.Get(1).(lua.LString)
	if !ok {
		return pushError(l, lua.Invalid, "eval.compile requires source string")
	}
	opts, err := parseCompileOptions(l, 2)
	if err != nil {
		return pushError(l, lua.Invalid, err.Error())
	}
	host, ctx, n := evalHost(l)
	if host == nil {
		return n
	}
	if n := checkCompilePermissions(ctx, l, opts.Policy); n != 0 {
		return n
	}
	program, err := host.Compile(ctx, apihost.EvalCompileSpec{
		SourceCode: string(source),
		Method:     opts.Method,
		Policy:     opts.Policy,
	})
	if err != nil {
		return pushWrapped(l, err)
	}
	l.Push(newProgram(l, program))
	l.Push(lua.LNil)
	return 2
}

// moduleSpawn is eval.spawn(source_or_program, options?) -> pid, err.
func moduleSpawn(l *lua.LState) int {
	switch arg := l.Get(1).(type) {
	case lua.LString:
		return spawnProgram(l, string(arg), apihost.EvalProgram{})
	case *lua.LUserData:
		if program, ok := arg.Value.(*Program); ok {
			return spawnProgram(l, "", program.handle)
		}
	}
	return pushError(l, lua.Invalid, "eval.spawn requires source string or eval.Program")
}

// moduleEvict is eval.evict(program) -> true, err.
func moduleEvict(l *lua.LState) int {
	program, ok := programArg(l, 1)
	if !ok {
		return pushError(l, lua.Invalid, "eval.Program expected")
	}
	return evict(l, program)
}

func spawnProgram(l *lua.LState, source string, program apihost.EvalProgram) int {
	opts, err := parseSpawnOptions(l, 2)
	if err != nil {
		return pushError(l, lua.Invalid, err.Error())
	}
	host, ctx, n := evalHost(l)
	if host == nil {
		return n
	}
	parent, ok := runtime.GetFramePID(ctx)
	if !ok {
		return pushError(l, lua.Internal, "eval.spawn requires a process")
	}
	if program.IsZero() {
		if n := checkCompilePermissions(ctx, l, opts.Policy); n != 0 {
			return n
		}
	}
	if !security.IsAllowed(ctx, "eval.spawn", "", nil) {
		return pushError(l, lua.PermissionDenied, "permission denied: eval.spawn")
	}
	child, err := host.Spawn(ctx, apihost.EvalSpawnSpec{
		Program:    program,
		SourceCode: source,
		Method:     opts.Method,
		Name:       opts.Name,
		Network:    opts.Network,
		Input:      opts.Input,
		Policy:     opts.Policy,
		Parent:     parent,
		LinkMode:   opts.LinkMode,
	})
	if err != nil {
		return pushWrapped(l, err)
	}
	l.Push(lua.LString(child.String()))
	l.Push(lua.LNil)
	return 2
}

func evict(l *lua.LState, program apihost.EvalProgram) int {
	host, ctx, n := evalHost(l)
	if host == nil {
		return n
	}
	if _, err := host.Evict(ctx, program); err != nil {
		return pushWrapped(l, err)
	}
	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

// checkCompilePermissions applies the caller's eval permissions to what the
// policy admits: compiling, and each module, import and class. It returns 0
// when allowed, otherwise the pushed error pair.
func checkCompilePermissions(ctx context.Context, l *lua.LState, policy apihost.EvalPolicy) int {
	if !security.IsAllowed(ctx, "eval.compile", "", nil) {
		return pushError(l, lua.PermissionDenied, "permission denied: eval.compile")
	}
	if denied, ok := evalhost.CheckModulePermissions(ctx, policy.Modules); !ok {
		return pushError(l, lua.PermissionDenied, "permission denied: eval.module "+denied)
	}
	if len(policy.Imports) > 0 {
		imports := make(map[string]registry.ID, len(policy.Imports))
		for _, imp := range policy.Imports {
			imports[imp.Alias] = imp.Source
		}
		if denied, ok := evalhost.CheckImportPermissions(ctx, imports); !ok {
			return pushError(l, lua.PermissionDenied, "permission denied: eval.import "+denied)
		}
	}
	if denied, ok := evalhost.CheckClassPermissions(ctx, policy.AllowClasses); !ok {
		return pushError(l, lua.PermissionDenied, "permission denied: eval.class "+denied)
	}
	return 0
}

// evalHost returns the node's eval host and the caller context, or nil with
// the pushed error pair.
func evalHost(l *lua.LState) (apihost.EvalHost, context.Context, int) {
	ctx := l.Context()
	if ctx == nil {
		return nil, nil, pushError(l, lua.Internal, "no context")
	}
	host := apihost.GetEvalHost(ctx)
	if host == nil {
		return nil, nil, pushError(l, lua.Unavailable, "eval host not available")
	}
	return host, ctx, 0
}

func pushError(l *lua.LState, kind lua.Kind, msg string) int {
	l.Push(lua.LNil)
	l.Push(lua.NewLuaError(l, msg).WithKind(kind).WithRetryable(false))
	return 2
}

func pushWrapped(l *lua.LState, err error) int {
	l.Push(lua.LNil)
	l.Push(lua.WrapErrorWithLua(l, err, ""))
	return 2
}
