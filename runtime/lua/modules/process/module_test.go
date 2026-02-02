package process

import (
	"context"
	"testing"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
)

func bindProcess(l *lua.LState) {
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)
}

func TestModuleInfo(t *testing.T) {
	info := Module.Info()
	if info.Name != "process" {
		t.Errorf("expected module name 'process', got %s", info.Name)
	}
	if info.Description == "" {
		t.Error("module description should not be empty")
	}
}

func TestRegister(t *testing.T) {
	tbl, yields := Module.Build()
	if tbl == nil {
		t.Fatal("module table should not be nil")
	}
	if yields == nil {
		t.Fatal("process module should have yields")
	}
}

func TestLoader(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindProcess(l)

	mod := l.GetGlobal("process")
	if mod.Type() != lua.LTTable {
		t.Fatal("process module not registered")
	}

	modTbl := mod.(*lua.LTable)

	functions := []string{
		"id", "pid", "send", "spawn", "spawn_monitored",
		"spawn_linked", "spawn_linked_monitored", "terminate",
		"cancel", "get_options", "set_options", "monitor",
		"unmonitor", "link", "unlink", "inbox", "events",
		"listen", "unlisten", "with_context", "upgrade",
	}

	for _, fn := range functions {
		if modTbl.RawGetString(fn).Type() != lua.LTFunction {
			t.Errorf("%s function not registered", fn)
		}
	}

	event := modTbl.RawGetString("event")
	if event.Type() != lua.LTTable {
		t.Error("event table not registered")
	}

	registry := modTbl.RawGetString("registry")
	if registry.Type() != lua.LTTable {
		t.Error("registry table not registered")
	}
}

func TestEventConstants(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindProcess(l)

	err := l.DoString(`
		if process.event.CANCEL == nil then error("CANCEL event not defined") end
		if process.event.EXIT == nil then error("EXIT event not defined") end
		if process.event.LINK_DOWN == nil then error("LINK_DOWN event not defined") end
	`)
	if err != nil {
		t.Errorf("event constants test failed: %v", err)
	}
}

func TestRegistryMethods(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindProcess(l)

	err := l.DoString(`
		if type(process.registry.register) ~= "function" then
			error("register function not found")
		end
		if type(process.registry.lookup) ~= "function" then
			error("lookup function not found")
		end
		if type(process.registry.unregister) ~= "function" then
			error("unregister function not found")
		end
	`)
	if err != nil {
		t.Errorf("registry methods test failed: %v", err)
	}
}

func TestPID_NoContext(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindProcess(l)

	err := l.DoString(`
		local pid, err = process.pid()
		if err == nil then error("expected error without context") end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestID_NoContext(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindProcess(l)

	err := l.DoString(`
		local id, err = process.id()
		if id ~= nil then error("expected nil without context") end
		if err == nil then error("expected error without context") end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestGetOptions(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindProcess(l)

	err := l.DoString(`
		local opts = process.get_options()
		if type(opts) ~= "table" then error("expected table from get_options") end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestSetOptions_NoProcessContext(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindProcess(l)

	// Without a process context, set_options should fail gracefully
	err := l.DoString(`
		local ok, err = process.set_options({})
		if ok then error("set_options without process context should fail") end
		if err ~= "no process context" then error("expected 'no process context' error, got: " .. tostring(err)) end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestSetOptions_InvalidType(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindProcess(l)

	err := l.DoString(`
		local ok, err = process.set_options("not a table")
		if ok then error("set_options with string should fail") end
		if err == nil then error("expected error") end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestContextValuesInheritance(t *testing.T) {
	// Test that context values are properly set up for inheritance
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(fc)

	// Set values on parent frame
	values := ctxapi.NewValues()
	values.Set("parent_key", "parent_value")
	if err := fc.Set(ctxapi.ValuesCtx, values); err != nil {
		t.Fatalf("failed to set values: %v", err)
	}

	// Seal parent frame (simulating yield)
	fc.Seal()

	// Open child frame (simulating nested call)
	childCtx, childFC := ctxapi.OpenFrameContext(ctx)
	defer ctxapi.ReleaseFrameContext(childFC)

	// Child should inherit values
	childValues := ctxapi.GetValues(childCtx)
	if childValues == nil {
		t.Fatal("child should inherit values from parent")
	}

	if v, ok := childValues.Get("parent_key"); !ok || v != "parent_value" {
		t.Error("child should have parent's value")
	}
}

func TestSpawnerContextValues(t *testing.T) {
	// Test that Spawner properly stores context values
	spawner := &Spawner{}

	if spawner.values != nil {
		t.Error("new spawner should have nil values")
	}

	// Set values
	values := ctxapi.NewValues()
	values.Set("spawn_key", "spawn_value")
	spawner.values = values

	if spawner.values == nil || spawner.values.Len() == 0 {
		t.Error("spawner should have values after setting")
	}

	if v, ok := spawner.values.Get("spawn_key"); !ok || v != "spawn_value" {
		t.Error("spawner values incorrect")
	}
}
