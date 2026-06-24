// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"testing"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
)

// isolationSecretModule mimics a capability module (e.g. funcs) that must not
// leak to code which never declared it as an import.
var isolationSecretModule = &luaapi.ModuleDef{
	Name:        "funcs",
	Description: "Capability module that must not leak",
	Class:       []string{luaapi.ClassNondeterministic},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		tbl := lua.CreateTable(0, 2)
		tbl.RawSetString("marker", lua.LString("secret"))
		tbl.RawSetString("call", lua.LGoFunc(func(s *lua.LState) int {
			s.Push(lua.LString("privileged-result"))
			return 1
		}))
		return tbl, nil
	},
}

// TestFactory_ImportIsolation_LibDepDoesNotLeak proves that a module pulled in
// only by a library is not reachable from a function that imports the library
// but never declared the module itself.
func TestFactory_ImportIsolation_LibDepDoesNotLeak(t *testing.T) {
	cm := setupFactoryCodeManager(t)

	// Capability module.
	if err := cm.AddNode(context.Background(), code.Node{
		ID:     registry.NewID("", "funcs"),
		Kind:   luaapi.ModuleKind,
		Module: isolationSecretModule,
	}, nil); err != nil {
		t.Fatalf("AddNode funcs failed: %v", err)
	}

	// Library that legitimately declares and uses the module.
	libID := registry.NewID("app", "leaky_lib")
	if err := cm.AddNode(context.Background(), code.Node{
		ID:     libID,
		Kind:   luaapi.Library,
		Source: `local f = require("funcs"); return { leaked = f.marker }`,
	}, []code.Import{{ID: registry.NewID("", "funcs"), Alias: "funcs"}}); err != nil {
		t.Fatalf("AddNode lib failed: %v", err)
	}

	// Function that imports ONLY the library, never the module.
	fnID := registry.NewID("app", "consumer")
	if err := cm.AddNode(context.Background(), code.Node{
		ID:     fnID,
		Kind:   luaapi.Function,
		Source: `return function() return lib end`,
		Method: "main",
	}, []code.Import{{ID: libID, Alias: "lib"}}); err != nil {
		t.Fatalf("AddNode fn failed: %v", err)
	}

	pf := NewProcessFactory(cm)
	factoryFn, err := pf.CreateFactory(fnID)
	if err != nil {
		t.Fatalf("CreateFactory failed: %v", err)
	}
	proc, err := factoryFn()
	if err != nil {
		t.Fatalf("factory() failed: %v", err)
	}
	state := proc.(*Process).State()
	env := state.Env

	// The library resolved its declared module: proves libs keep working.
	libVal := env.RawGetString("lib")
	libTbl, ok := libVal.(*lua.LTable)
	if !ok {
		t.Fatalf("entry env missing 'lib' table, got %T", libVal)
	}
	if got := libTbl.RawGetString("leaked").String(); got != "secret" {
		t.Fatalf("library failed to resolve its own 'funcs' import: got %q", got)
	}

	// The function must NOT see the module as a free global.
	if v := env.RawGetString("funcs"); v != lua.LNil {
		t.Fatalf("LEAK: 'funcs' is visible in the function env: %v", v)
	}

	// The module must not be a real global either.
	if v := state.GetGlobal("funcs"); v != lua.LNil {
		t.Fatalf("LEAK: 'funcs' is a real global: %v", v)
	}

	// Scoped require in the function must reject the undeclared module.
	reqVal := env.RawGetString("require")
	reqFn, ok := reqVal.(*lua.LFunction)
	if !ok {
		t.Fatalf("entry env missing scoped require, got %T", reqVal)
	}
	err = state.CallByParam(lua.P{Fn: reqFn, NRet: 1, Protect: true}, lua.LString("funcs"))
	if err == nil {
		t.Fatal("LEAK: require('funcs') succeeded in a function that never declared it")
	}
}

// TestFactory_ImportIsolation_OverlayLibrary proves the intended capability
// pattern: an overlay library is granted privileged infrastructure (funcs) and
// exposes a callable API; an untrusted "evil runner" function may invoke that
// API but can never reach funcs itself. The overlay's callable keeps its own
// (privileged) environment when invoked from the runner, because closures
// capture the chunk environment they were created in.
func TestFactory_ImportIsolation_OverlayLibrary(t *testing.T) {
	cm := setupFactoryCodeManager(t)

	if err := cm.AddNode(context.Background(), code.Node{
		ID:     registry.NewID("", "funcs"),
		Kind:   luaapi.ModuleKind,
		Module: isolationSecretModule,
	}, nil); err != nil {
		t.Fatalf("AddNode funcs failed: %v", err)
	}

	// Overlay library: declares funcs, exposes a safe callable that uses it.
	overlayID := registry.NewID("app", "overlay")
	if err := cm.AddNode(context.Background(), code.Node{
		ID:     overlayID,
		Kind:   luaapi.Library,
		Source: `local f = require("funcs"); return { run = function() return f.call() end }`,
	}, []code.Import{{ID: registry.NewID("", "funcs"), Alias: "funcs"}}); err != nil {
		t.Fatalf("AddNode overlay failed: %v", err)
	}

	// Evil runner: imports ONLY the overlay, never funcs.
	runnerID := registry.NewID("app", "evil_runner")
	if err := cm.AddNode(context.Background(), code.Node{
		ID:     runnerID,
		Kind:   luaapi.Function,
		Source: `return function() return overlay.run() end`,
		Method: "main",
	}, []code.Import{{ID: overlayID, Alias: "overlay"}}); err != nil {
		t.Fatalf("AddNode runner failed: %v", err)
	}

	pf := NewProcessFactory(cm)
	factoryFn, err := pf.CreateFactory(runnerID)
	if err != nil {
		t.Fatalf("CreateFactory failed: %v", err)
	}
	proc, err := factoryFn()
	if err != nil {
		t.Fatalf("factory() failed: %v", err)
	}
	state := proc.(*Process).State()
	env := state.Env

	// The runner can call the overlay's privileged API and get its result.
	overlayVal := env.RawGetString("overlay")
	overlayTbl, ok := overlayVal.(*lua.LTable)
	if !ok {
		t.Fatalf("runner env missing 'overlay' table, got %T", overlayVal)
	}
	runFn, ok := overlayTbl.RawGetString("run").(*lua.LFunction)
	if !ok {
		t.Fatalf("overlay.run is not a function")
	}
	if err := state.CallByParam(lua.P{Fn: runFn, NRet: 1, Protect: true}); err != nil {
		t.Fatalf("overlay.run() failed: %v", err)
	}
	if got := state.Get(-1).String(); got != "privileged-result" {
		t.Fatalf("overlay.run() returned %q, want privileged-result", got)
	}
	state.Pop(1)

	// The runner itself cannot reach funcs by any path.
	if v := env.RawGetString("funcs"); v != lua.LNil {
		t.Fatalf("LEAK: 'funcs' visible to evil runner: %v", v)
	}
	if v := state.GetGlobal("funcs"); v != lua.LNil {
		t.Fatalf("LEAK: 'funcs' is a real global: %v", v)
	}
}
