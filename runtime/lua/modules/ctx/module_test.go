package ctx

import (
	"context"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	lua "github.com/yuin/gopher-lua"
)

// setupContextWithValues creates a frame context with pre-set values
func setupContextWithValues(keyValues map[string]any) context.Context {
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	values := ctxapi.NewValues()
	for k, v := range keyValues {
		values.Set(k, v)
	}
	_ = fc.Set(ctxapi.ValuesCtx, values)
	return ctx
}

func TestModuleLoad(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	Module.Load(l)

	mod := l.GetGlobal("ctx")
	if mod.Type() != lua.LTTable {
		t.Fatal("module not registered")
	}

	tbl := mod.(*lua.LTable)
	if tbl.RawGetString("get").Type() != lua.LTFunction {
		t.Error("get function not registered")
	}
	if tbl.RawGetString("all").Type() != lua.LTFunction {
		t.Error("all function not registered")
	}
}

func TestModuleLoadReuse(t *testing.T) {
	l1 := lua.NewState()
	defer l1.Close()
	l2 := lua.NewState()
	defer l2.Close()

	Module.Load(l1)
	Module.Load(l2)

	mod1 := l1.GetGlobal("ctx").(*lua.LTable)
	mod2 := l2.GetGlobal("ctx").(*lua.LTable)

	if mod1 != mod2 {
		t.Error("module table should be reused across states")
	}
}

func TestGet(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ctx := setupContextWithValues(map[string]any{
		"foo": "bar",
	})
	l.SetContext(ctx)

	Module.Load(l)

	err := l.DoString(`
		local val, err = ctx.get("foo")
		if err then
			error("get failed: " .. tostring(err))
		end
		if val ~= "bar" then
			error("expected 'bar', got '" .. tostring(val) .. "'")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestGetNotFound(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)

	ctx := setupContextWithValues(map[string]any{})
	l.SetContext(ctx)

	Module.Load(l)

	err := l.DoString(`
		local val, err = ctx.get("nonexistent")
		if val ~= nil then
			error("expected nil for nonexistent key")
		end
		if err == nil then
			error("expected error for nonexistent key")
		end
		if err:kind() ~= errors.NOT_FOUND then
			error("expected NOT_FOUND kind, got: " .. tostring(err:kind()))
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestGetEmptyKey(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)

	ctx := setupContextWithValues(map[string]any{})
	l.SetContext(ctx)

	Module.Load(l)

	err := l.DoString(`
		local val, err = ctx.get("")
		if val ~= nil then
			error("expected nil for empty key")
		end
		if err == nil then
			error("expected error for empty key")
		end
		if err:kind() ~= errors.INVALID then
			error("expected INVALID kind, got: " .. tostring(err:kind()))
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestAll(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ctx := setupContextWithValues(map[string]any{
		"key1": "value1",
		"key2": 42.0,
		"key3": true,
	})
	l.SetContext(ctx)

	Module.Load(l)

	err := l.DoString(`
		local all, err = ctx.all()
		if err then
			error("all failed: " .. tostring(err))
		end

		if all.key1 ~= "value1" then
			error("key1 mismatch")
		end
		if all.key2 ~= 42 then
			error("key2 mismatch")
		end
		if all.key3 ~= true then
			error("key3 mismatch")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestAllEmpty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	// No values in context
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	l.SetContext(ctx)

	Module.Load(l)

	err := l.DoString(`
		local all, err = ctx.all()
		if err then
			error("all failed: " .. tostring(err))
		end

		local count = 0
		for _ in pairs(all) do
			count = count + 1
		end
		if count ~= 0 then
			error("expected empty table")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestGetDifferentTypes(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ctx := setupContextWithValues(map[string]any{
		"str":  "hello",
		"num":  123.45,
		"bool": true,
		"obj":  map[string]any{"name": "test", "count": float64(5)},
		"arr":  []any{float64(1), float64(2), float64(3)},
	})
	l.SetContext(ctx)

	Module.Load(l)

	err := l.DoString(`
		-- String
		local v, _ = ctx.get("str")
		if v ~= "hello" then error("string mismatch") end

		-- Number
		v, _ = ctx.get("num")
		if v ~= 123.45 then error("number mismatch") end

		-- Boolean
		v, _ = ctx.get("bool")
		if v ~= true then error("bool mismatch") end

		-- Table (object)
		v, _ = ctx.get("obj")
		if type(v) ~= "table" then error("expected table, got " .. type(v)) end
		if v.name ~= "test" then error("name mismatch") end
		if v.count ~= 5 then error("count mismatch") end

		-- Array
		v, _ = ctx.get("arr")
		if type(v) ~= "table" then error("expected table for array") end
		if v[1] ~= 1 or v[2] ~= 2 or v[3] ~= 3 then error("array mismatch") end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestNoValues(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)

	// Don't set context - values bag won't exist
	Module.Load(l)

	err := l.DoString(`
		local val, err = ctx.get("key")
		if val ~= nil then
			error("expected nil without values")
		end
		if err == nil then
			error("expected error without values")
		end
		-- No values bag means NOT_FOUND for any key
		if err:kind() ~= errors.NOT_FOUND then
			error("expected NOT_FOUND kind, got: " .. tostring(err:kind()))
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}
