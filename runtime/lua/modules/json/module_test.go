package json

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestBind(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	// Directly register the module
	reg := Module.Register(l)
	l.SetGlobal("json", reg.Table)

	mod := l.GetGlobal("json")
	if mod.Type() != lua.LTTable {
		t.Fatal("json module not registered")
	}

	tbl := mod.(*lua.LTable)
	if tbl.RawGetString("encode").Type() != lua.LTFunction {
		t.Error("encode function not registered")
	}
	if tbl.RawGetString("decode").Type() != lua.LTFunction {
		t.Error("decode function not registered")
	}
}

func bindJSON(l *lua.LState) {
	reg := Module.Register(l)
	l.SetGlobal("json", reg.Table)
}

func TestEncodeTable(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindJSON(l)

	err := l.DoString(`
		local result = json.encode({name = "test", value = 123})
		if not result then error("encode failed") end
	`)
	if err != nil {
		t.Errorf("encode table test failed: %v", err)
	}
}

func TestEncodeArray(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindJSON(l)

	err := l.DoString(`
		local result = json.encode({1, 2, 3})
		if result ~= "[1,2,3]" then error("encode array mismatch: " .. result) end
	`)
	if err != nil {
		t.Errorf("encode array test failed: %v", err)
	}
}

func TestEncodeNil(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindJSON(l)

	err := l.DoString(`
		local result = json.encode(nil)
		if result ~= "null" then error("encode nil mismatch: " .. result) end
	`)
	if err != nil {
		t.Errorf("encode nil test failed: %v", err)
	}
}

func TestEncodeString(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindJSON(l)

	err := l.DoString(`
		local result = json.encode("hello")
		if result ~= '"hello"' then error("encode string mismatch: " .. result) end
	`)
	if err != nil {
		t.Errorf("encode string test failed: %v", err)
	}
}

func TestEncodeNumber(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindJSON(l)

	err := l.DoString(`
		local result = json.encode(42)
		if result ~= "42" then error("encode number mismatch: " .. result) end
	`)
	if err != nil {
		t.Errorf("encode number test failed: %v", err)
	}
}

func TestEncodeBool(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindJSON(l)

	err := l.DoString(`
		local result = json.encode(true)
		if result ~= "true" then error("encode bool mismatch: " .. result) end
	`)
	if err != nil {
		t.Errorf("encode bool test failed: %v", err)
	}
}

func TestDecodeObject(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindJSON(l)

	err := l.DoString(`
		local result, err = json.decode('{"name":"test","value":123}')
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
	bindJSON(l)

	err := l.DoString(`
		local result, err = json.decode('[1, 2, 3]')
		if not result then error(err) end
		if result[1] ~= 1 then error("first element mismatch") end
		if result[2] ~= 2 then error("second element mismatch") end
		if result[3] ~= 3 then error("third element mismatch") end
	`)
	if err != nil {
		t.Errorf("decode array test failed: %v", err)
	}
}

func TestDecodeInvalidInput(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindJSON(l)

	err := l.DoString(`
		local result, err = json.decode(123)
		if result ~= nil then error("expected nil for non-string") end
		if err == nil then error("expected error") end
	`)
	if err != nil {
		t.Errorf("decode invalid input test failed: %v", err)
	}
}

func TestDecodeEmpty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindJSON(l)

	err := l.DoString(`
		local result, err = json.decode("")
		if result ~= nil then error("expected nil for empty string") end
		if err == nil then error("expected error") end
	`)
	if err != nil {
		t.Errorf("decode empty test failed: %v", err)
	}
}

func TestDecodeInvalidJSON(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindJSON(l)

	err := l.DoString(`
		local result, err = json.decode("not json")
		if result ~= nil then error("expected nil for invalid json") end
		if err == nil then error("expected error") end
	`)
	if err != nil {
		t.Errorf("decode invalid json test failed: %v", err)
	}
}

func TestRoundTrip(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindJSON(l)

	err := l.DoString(`
		local original = {name = "test", numbers = {1, 2, 3}, nested = {a = 1}}
		local encoded = json.encode(original)
		local decoded = json.decode(encoded)
		if decoded.name ~= "test" then error("name mismatch") end
		if decoded.numbers[1] ~= 1 then error("numbers mismatch") end
		if decoded.nested.a ~= 1 then error("nested mismatch") end
	`)
	if err != nil {
		t.Errorf("round trip test failed: %v", err)
	}
}
