// SPDX-License-Identifier: MPL-2.0

package env

import (
	"testing"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/security"
)

func TestLoad(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	mod := l.GetGlobal("env")
	if mod.Type() != lua.LTTable {
		t.Fatal("env module not registered")
	}

	modTbl := mod.(*lua.LTable)
	funcs := []string{"get", "set", "get_all"}
	for _, fn := range funcs {
		if modTbl.RawGetString(fn).Type() != lua.LTFunction {
			t.Errorf("%s function not registered", fn)
		}
	}
}

func TestLoadReuse(t *testing.T) {
	l1 := lua.NewState()
	defer l1.Close()
	l2 := lua.NewState()
	defer l2.Close()

	tbl, _ := Module.Build()
	l1.SetGlobal(Module.Name, tbl)
	l2.SetGlobal(Module.Name, tbl)

	mod1 := l1.GetGlobal("env").(*lua.LTable)
	mod2 := l2.GetGlobal("env").(*lua.LTable)

	if mod1 != mod2 {
		t.Error("module table should be reused across states")
	}
}

func TestImmutability(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local success = pcall(function()
			env.foo = "bar"
		end)
	`)
	if err != nil {
		t.Errorf("immutability test failed: %v", err)
	}
}

func TestGetNoContext(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local val, err = env.get("TEST_VAR")
		if err == nil then
			error("expected error for no context")
		end
	`)
	if err != nil {
		t.Errorf("get no context test failed: %v", err)
	}
}

func TestSetNoContext(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local val, err = env.set("TEST_VAR", "value")
		if err == nil then
			error("expected error for no context")
		end
	`)
	if err != nil {
		t.Errorf("set no context test failed: %v", err)
	}
}

func TestGetAllNoContext(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local val, err = env.get_all()
		if err == nil then
			error("expected error for no context")
		end
	`)
	if err != nil {
		t.Errorf("get_all no context test failed: %v", err)
	}
}

func TestGetWithEmptyContext(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	l.SetContext(ctx)

	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local val, err = env.get("TEST_VAR")
		if err == nil then
			error("expected error for missing registry")
		end
	`)
	if err != nil {
		t.Errorf("get with empty context test failed: %v", err)
	}
}

func TestGetEmptyKey(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	l.SetContext(ctx)

	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local val, err = env.get("")
		if val ~= nil or err == nil then
			error("expected nil value and error for empty key")
		end
	`)
	if err != nil {
		t.Errorf("get empty key test failed: %v", err)
	}
}

func TestSetEmptyKey(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	l.SetContext(ctx)

	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local ok, err = env.set("", "value")
		if ok ~= nil or err == nil then
			error("expected nil result and error for empty key")
		end
	`)
	if err != nil {
		t.Errorf("set empty key test failed: %v", err)
	}
}
