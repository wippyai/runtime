package env

import (
	"context"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestBind(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	Bind(l)

	mod := l.GetGlobal("env")
	if mod.Type() != lua.LTTable {
		t.Fatal("env module not registered")
	}

	tbl := mod.(*lua.LTable)
	funcs := []string{"get", "set", "get_all"}
	for _, fn := range funcs {
		if tbl.RawGetString(fn).Type() != lua.LTFunction {
			t.Errorf("%s function not registered", fn)
		}
	}
}

func TestImmutability(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	Bind(l)

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

	Bind(l)

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

	Bind(l)

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

	Bind(l)

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
	l.SetContext(context.Background())

	Bind(l)

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

	Bind(l)

	err := l.DoString(`
		local ok, err = pcall(function()
			env.get("")
		end)
		if ok then
			error("expected error for empty key")
		end
	`)
	if err != nil {
		t.Errorf("get empty key test failed: %v", err)
	}
}

func TestSetEmptyKey(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	Bind(l)

	err := l.DoString(`
		local ok, err = pcall(function()
			env.set("", "value")
		end)
		if ok then
			error("expected error for empty key")
		end
	`)
	if err != nil {
		t.Errorf("set empty key test failed: %v", err)
	}
}

func TestBindIdempotent(t *testing.T) {
	l1 := lua.NewState()
	defer l1.Close()

	l2 := lua.NewState()
	defer l2.Close()

	Bind(l1)
	Bind(l2)

	mod1 := l1.GetGlobal("env")
	mod2 := l2.GetGlobal("env")

	if mod1.Type() != lua.LTTable || mod2.Type() != lua.LTTable {
		t.Fatal("module should be registered in both states")
	}
}
