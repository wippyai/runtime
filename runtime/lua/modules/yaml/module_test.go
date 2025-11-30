package yaml

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestBind(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	Bind(l)

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

func TestEncodeTable(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

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
	Bind(l)

	err := l.DoString(`
		local result, err = yaml.encode({1, 2, 3})
		if not result then error(err) end
	`)
	if err != nil {
		t.Errorf("encode array test failed: %v", err)
	}
}

func TestEncodeMissingInput(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = yaml.encode()
		if err == nil then error("expected error for missing input") end
	`)
	if err != nil {
		t.Errorf("encode missing input test failed: %v", err)
	}
}

func TestEncodeNonTable(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = yaml.encode("not a table")
		if err == nil then error("expected error for non-table input") end
	`)
	if err != nil {
		t.Errorf("encode non-table test failed: %v", err)
	}
}

func TestDecodeObject(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

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
	Bind(l)

	err := l.DoString(`
		local result, err = yaml.decode("- 1\n- 2\n- 3")
		if not result then error(err) end
		if result[1] ~= 1 then error("first element mismatch") end
	`)
	if err != nil {
		t.Errorf("decode array test failed: %v", err)
	}
}

func TestDecodeMissingInput(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = yaml.decode()
		if err == nil then error("expected error for missing input") end
	`)
	if err != nil {
		t.Errorf("decode missing input test failed: %v", err)
	}
}

func TestDecodeEmpty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = yaml.decode("")
		if err == nil then error("expected error for empty input") end
	`)
	if err != nil {
		t.Errorf("decode empty test failed: %v", err)
	}
}

func TestDecodeInvalidYAML(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = yaml.decode(":\n  :\n  invalid")
		if err == nil then error("expected error for invalid yaml") end
	`)
	if err != nil {
		t.Errorf("decode invalid yaml test failed: %v", err)
	}
}

func TestRoundTrip(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

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
	Bind(l)

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
