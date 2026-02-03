// Package runner provides the eval.runner module for executing untrusted Lua code.
// Code execution is delegated to the dispatcher which handles yields internally.
package runner

import (
	"context"
	"sync"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/evalhost"
	"github.com/wippyai/runtime/runtime/lua/security"
)

var (
	moduleTable *lua.LTable
	initOnce    sync.Once
)

const programTypeName = "eval_runner.Program"

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
		value.RegisterTypeMethods(nil, programTypeName, nil, programMethods)
	})

	return moduleTable, []luaapi.YieldType{
		{Sample: &CompileYield{}, CmdID: evalhost.Compile},
		{Sample: &RunYield{}, CmdID: evalhost.Run},
	}
}

func createModuleTable() *lua.LTable {
	mod := lua.CreateTable(0, 2)
	mod.RawSetString("compile", lua.LGoFunc(compileFunc))
	mod.RawSetString("run", lua.LGoFunc(runFunc))
	mod.Immutable = true
	return mod
}

// checkModulePermissions checks if each module is allowed to be loaded
func checkModulePermissions(ctx context.Context, modules []string) (string, bool) {
	if len(modules) == 0 {
		return "", true
	}

	meta := attrs.NewBag()
	if frameID, ok := runtime.GetFrameID(ctx); ok {
		meta.Set("entry_id", frameID.String())
	}

	for _, module := range modules {
		if !security.IsAllowed(ctx, "eval.module", module, meta) {
			return module, false
		}
	}
	return "", true
}

// checkImportPermissions checks if each import is allowed to be loaded
func checkImportPermissions(ctx context.Context, imports map[string]registry.ID) (string, bool) {
	if len(imports) == 0 {
		return "", true
	}

	meta := attrs.NewBag()
	if frameID, ok := runtime.GetFrameID(ctx); ok {
		meta.Set("entry_id", frameID.String())
	}

	for alias, id := range imports {
		meta.Set("alias", alias)
		if !security.IsAllowed(ctx, "eval.import", id.String(), meta) {
			return id.String(), false
		}
	}
	return "", true
}

// checkClassPermissions checks if each class is allowed to be enabled
func checkClassPermissions(ctx context.Context, classes []string) (string, bool) {
	if len(classes) == 0 {
		return "", true
	}

	meta := attrs.NewBag()
	if frameID, ok := runtime.GetFrameID(ctx); ok {
		meta.Set("entry_id", frameID.String())
	}

	for _, class := range classes {
		if !security.IsAllowed(ctx, "eval.class", class, meta) {
			return class, false
		}
	}
	return "", true
}

// compileFunc is runner.compile(source, method, options?) -> Program
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
	var imports map[string]registry.ID
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
		if importsVal := opts.RawGetString("imports"); importsVal.Type() == lua.LTTable {
			imports = make(map[string]registry.ID)
			importsTable := importsVal.(*lua.LTable)
			importsTable.ForEach(func(k, v lua.LValue) {
				if alias, ok := k.(lua.LString); ok {
					if idStr, ok := v.(lua.LString); ok {
						imports[string(alias)] = registry.ParseID(string(idStr))
					}
				}
			})
		}
	}

	if denied, ok := checkModulePermissions(ctx, modules); !ok {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: eval.module "+denied).
			WithKind(lua.PermissionDenied).WithRetryable(false))
		return 2
	}

	if denied, ok := checkImportPermissions(ctx, imports); !ok {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: eval.import "+denied).
			WithKind(lua.PermissionDenied).WithRetryable(false))
		return 2
	}

	yield := AcquireCompileYield()
	yield.Source = source
	yield.Method = method
	yield.Modules = modules
	yield.Imports = imports

	l.Push(yield)
	return -1
}

// runFunc is runner.run(config) -> result
func runFunc(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "no context").WithKind(lua.Internal))
		return 2
	}

	if !security.IsAllowed(ctx, "eval.run", "", nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: eval.run").
			WithKind(lua.PermissionDenied).WithRetryable(false))
		return 2
	}

	config := l.CheckTable(1)

	source := ""
	if v := config.RawGetString("source"); v.Type() == lua.LTString {
		source = string(v.(lua.LString))
	}
	if source == "" {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "source is required").
			WithKind(lua.Invalid).WithRetryable(false))
		return 2
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

	var imports map[string]registry.ID
	if v := config.RawGetString("imports"); v.Type() == lua.LTTable {
		imports = make(map[string]registry.ID)
		v.(*lua.LTable).ForEach(func(k, iv lua.LValue) {
			if alias, ok := k.(lua.LString); ok {
				if idStr, ok := iv.(lua.LString); ok {
					imports[string(alias)] = registry.ParseID(string(idStr))
				}
			}
		})
	}

	if denied, ok := checkModulePermissions(ctx, modules); !ok {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: eval.module "+denied).
			WithKind(lua.PermissionDenied).WithRetryable(false))
		return 2
	}

	if denied, ok := checkImportPermissions(ctx, imports); !ok {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: eval.import "+denied).
			WithKind(lua.PermissionDenied).WithRetryable(false))
		return 2
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

	// Parse allow_classes to permit additional module classes
	var allowClasses []string
	if v := config.RawGetString("allow_classes"); v.Type() == lua.LTTable {
		v.(*lua.LTable).ForEach(func(_, cv lua.LValue) {
			if s, ok := cv.(lua.LString); ok {
				allowClasses = append(allowClasses, string(s))
			}
		})
	}

	// Check permission for each allowed class
	if denied, ok := checkClassPermissions(ctx, allowClasses); !ok {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: eval.class "+denied).
			WithKind(lua.PermissionDenied).WithRetryable(false))
		return 2
	}

	// Parse custom_modules - Lua tables to inject as modules
	customModules := make(map[string]any)
	if v := config.RawGetString("custom_modules"); v.Type() == lua.LTTable {
		v.(*lua.LTable).ForEach(func(k, cv lua.LValue) {
			if ks, ok := k.(lua.LString); ok {
				customModules[string(ks)] = value.ToGoAny(cv)
			}
		})
	}

	yield := AcquireRunYield()
	yield.Source = source
	yield.Method = method
	yield.Args = args
	yield.Modules = modules
	yield.Imports = imports
	yield.Context = contextVals
	yield.AllowClasses = allowClasses
	yield.CustomModules = customModules

	l.Push(yield)
	return -1
}
