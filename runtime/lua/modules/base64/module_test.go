package base64

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestBind(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	Bind(l)

	mod := l.GetGlobal("base64")
	if mod.Type() != lua.LTTable {
		t.Fatal("base64 module not registered")
	}

	tbl := mod.(*lua.LTable)
	if tbl.RawGetString("encode").Type() != lua.LTFunction {
		t.Error("encode function not registered")
	}
	if tbl.RawGetString("decode").Type() != lua.LTFunction {
		t.Error("decode function not registered")
	}
}

func TestBindReuse(t *testing.T) {
	l1 := lua.NewState()
	defer l1.Close()
	l2 := lua.NewState()
	defer l2.Close()

	Bind(l1)
	Bind(l2)

	mod1 := l1.GetGlobal("base64").(*lua.LTable)
	mod2 := l2.GetGlobal("base64").(*lua.LTable)

	if mod1 != mod2 {
		t.Error("module table should be reused across states")
	}
}

func TestEncode(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple", "hello", "aGVsbG8="},
		{"empty", "", ""},
		{"binary", "\x00\x01\x02", "AAEC"},
		{"unicode", "hello", "aGVsbG8="},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()
			Bind(l)

			l.Push(l.GetGlobal("base64").(*lua.LTable).RawGetString("encode"))
			l.Push(lua.LString(tt.input))
			l.Call(1, 1)

			result := l.ToString(-1)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestEncodeInvalidInput(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local result, err = base64.encode(123)
		if result ~= nil then
			error("expected nil result")
		end
		if err == nil then
			error("expected error")
		end
		if err:kind() ~= "Invalid" then
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

func TestDecode(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple", "aGVsbG8=", "hello"},
		{"empty", "", ""},
		{"binary", "AAEC", "\x00\x01\x02"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()
			Bind(l)

			l.Push(l.GetGlobal("base64").(*lua.LTable).RawGetString("decode"))
			l.Push(lua.LString(tt.input))
			l.Call(1, 1)

			result := l.ToString(-1)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestDecodeInvalidInput(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local result, err = base64.decode(123)
		if result ~= nil then
			error("expected nil result")
		end
		if err == nil then
			error("expected error")
		end
		if err:kind() ~= "Invalid" then
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

func TestDecodeInvalidBase64(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local result, err = base64.decode("!!!invalid!!!")
		if result ~= nil then
			error("expected nil result")
		end
		if err == nil then
			error("expected error")
		end
		if err:kind() ~= "Invalid" then
			error("expected Invalid kind, got: " .. tostring(err:kind()))
		end
		if err:retryable() ~= false then
			error("expected retryable to be false")
		end
		-- Verify error can be converted to string
		local str = tostring(err)
		if not str or str == "" then
			error("error should have string representation")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestRoundTrip(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local original = "Hello, World! 123"
		local encoded = base64.encode(original)
		local decoded = base64.decode(encoded)
		if decoded ~= original then
			error("round trip failed: " .. tostring(decoded) .. " ~= " .. original)
		end
	`)
	if err != nil {
		t.Errorf("round trip test failed: %v", err)
	}
}
