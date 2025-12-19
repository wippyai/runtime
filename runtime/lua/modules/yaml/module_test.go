package yaml

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestLoad(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	Module.Load(l)

	mod := l.GetGlobal("yaml")
	if mod.Type() != lua.LTTable {
		t.Fatal("yaml module not registered")
	}

	tbl := mod.(*lua.LTable)
	if tbl.RawGetString("encode").Type() != lua.LTFunction {
		t.Error("encode function not registered")
	}
	if tbl.RawGetString("decode").Type() != lua.LTFunction {
		t.Error("decode function not registered")
	}
}

func TestLoadReuse(t *testing.T) {
	l1 := lua.NewState()
	defer l1.Close()
	l2 := lua.NewState()
	defer l2.Close()

	Module.Load(l1)
	Module.Load(l2)

	mod1 := l1.GetGlobal("yaml").(*lua.LTable)
	mod2 := l2.GetGlobal("yaml").(*lua.LTable)

	if mod1 != mod2 {
		t.Error("module table should be reused across states")
	}
}

func TestEncodeTable(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local result, err = yaml.encode({name = "test", value = 123})
		if not result then error(err) end
		if not result:find("name") then error("name not found in output") end
		if not result:find("test") then error("test value not found") end
	`)
	if err != nil {
		t.Errorf("encode table test failed: %v", err)
	}
}

func TestEncodeArray(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local result, err = yaml.encode({1, 2, 3})
		if not result then error(err) end
	`)
	if err != nil {
		t.Errorf("encode array test failed: %v", err)
	}
}

func TestEncodeInvalidInput(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	Module.Load(l)

	err := l.DoString(`
		local result, err = yaml.encode(123)
		if result ~= nil then
			error("expected nil result")
		end
		if err == nil then
			error("expected error")
		end
		if err:kind() ~= errors.INVALID then
			error("expected Invalid kind, got: " .. tostring(err:kind()))
		end
		if err:retryable() ~= false then
			error("expected retryable to be false")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestEncodeMissingInput(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	Module.Load(l)

	err := l.DoString(`
		local result, err = yaml.encode()
		if result ~= nil then
			error("expected nil result")
		end
		if err == nil then
			error("expected error")
		end
		if err:kind() ~= errors.INVALID then
			error("expected Invalid kind, got: " .. tostring(err:kind()))
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestDecodeObject(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local result, err = yaml.decode("name: test\nvalue: 123")
		if not result then error(err) end
		if result.name ~= "test" then error("name mismatch") end
		if result.value ~= 123 then error("value mismatch") end
	`)
	if err != nil {
		t.Errorf("decode object test failed: %v", err)
	}
}

func TestDecodeArray(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local result, err = yaml.decode("- 1\n- 2\n- 3")
		if not result then error(err) end
		if result[1] ~= 1 then error("first element mismatch") end
	`)
	if err != nil {
		t.Errorf("decode array test failed: %v", err)
	}
}

func TestDecodeInvalidInput(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	Module.Load(l)

	err := l.DoString(`
		local result, err = yaml.decode(123)
		if result ~= nil then
			error("expected nil result")
		end
		if err == nil then
			error("expected error")
		end
		if err:kind() ~= errors.INVALID then
			error("expected Invalid kind, got: " .. tostring(err:kind()))
		end
		if err:retryable() ~= false then
			error("expected retryable to be false")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestDecodeEmpty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	Module.Load(l)

	err := l.DoString(`
		local result, err = yaml.decode("")
		if result ~= nil then
			error("expected nil result")
		end
		if err == nil then
			error("expected error")
		end
		if err:kind() ~= errors.INVALID then
			error("expected Invalid kind, got: " .. tostring(err:kind()))
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestDecodeInvalidYAML(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	Module.Load(l)

	err := l.DoString(`
		local result, err = yaml.decode(":\n  :\n  invalid")
		if result ~= nil then
			error("expected nil result")
		end
		if err == nil then
			error("expected error")
		end
		if err:kind() ~= errors.INTERNAL then
			error("expected Internal kind, got: " .. tostring(err:kind()))
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestRoundTrip(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local original = {name = "test", numbers = {1, 2, 3}}
		local encoded, err = yaml.encode(original)
		if not encoded then error(err) end
		local decoded, err = yaml.decode(encoded)
		if not decoded then error(err) end
		if decoded.name ~= "test" then error("name mismatch") end
	`)
	if err != nil {
		t.Errorf("round trip test failed: %v", err)
	}
}

func TestDecodeNestedStructure(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local yamlStr = [[
parent:
  child:
    value: 123
]]
		local result, err = yaml.decode(yamlStr)
		if not result then error(err) end
		if result.parent.child.value ~= 123 then error("nested value mismatch") end
	`)
	if err != nil {
		t.Errorf("decode nested structure test failed: %v", err)
	}
}

func TestEncodeWithFieldOrder(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local data = {zebra = 1, alpha = 2, beta = 3}
		local result, err = yaml.encode(data, {field_order = {"alpha", "beta", "zebra"}})
		if not result then error(err) end

		local alpha_pos = result:find("alpha")
		local beta_pos = result:find("beta")
		local zebra_pos = result:find("zebra")

		if not (alpha_pos < beta_pos and beta_pos < zebra_pos) then
			error("fields not in expected order: " .. result)
		end
	`)
	if err != nil {
		t.Errorf("encode with field_order test failed: %v", err)
	}
}

func TestEncodeWithSortUnordered(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local data = {zebra = 1, alpha = 2, beta = 3}
		local result, err = yaml.encode(data, {sort_unordered = true})
		if not result then error(err) end

		local alpha_pos = result:find("alpha")
		local beta_pos = result:find("beta")
		local zebra_pos = result:find("zebra")

		if not (alpha_pos < beta_pos and beta_pos < zebra_pos) then
			error("fields not sorted alphabetically: " .. result)
		end
	`)
	if err != nil {
		t.Errorf("encode with sort_unordered test failed: %v", err)
	}
}

func TestEncodeWithFieldOrderAndSortUnordered(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local data = {zebra = 1, alpha = 2, beta = 3, name = "test", kind = "demo"}
		local result, err = yaml.encode(data, {
			field_order = {"name", "kind"},
			sort_unordered = true
		})
		if not result then error(err) end

		local name_pos = result:find("name")
		local kind_pos = result:find("kind")
		local alpha_pos = result:find("alpha")
		local beta_pos = result:find("beta")
		local zebra_pos = result:find("zebra")

		-- name and kind should come first (in that order)
		-- then alpha, beta, zebra (alphabetically)
		if not (name_pos < kind_pos) then
			error("name should come before kind")
		end
		if not (kind_pos < alpha_pos) then
			error("kind should come before alpha")
		end
		if not (alpha_pos < beta_pos and beta_pos < zebra_pos) then
			error("remaining fields not sorted alphabetically: " .. result)
		end
	`)
	if err != nil {
		t.Errorf("encode with field_order and sort_unordered test failed: %v", err)
	}
}

func TestEncodeNestedWithFieldOrder(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local data = {
			outer = {zebra = 1, alpha = 2},
			name = "test"
		}
		local result, err = yaml.encode(data, {
			field_order = {"name", "outer"},
			sort_unordered = true
		})
		if not result then error(err) end

		local name_pos = result:find("name")
		local outer_pos = result:find("outer")

		if not (name_pos < outer_pos) then
			error("name should come before outer: " .. result)
		end

		-- nested fields should also be sorted
		local alpha_pos = result:find("alpha")
		local zebra_pos = result:find("zebra")
		if not (alpha_pos < zebra_pos) then
			error("nested fields not sorted: " .. result)
		end
	`)
	if err != nil {
		t.Errorf("encode nested with field_order test failed: %v", err)
	}
}
