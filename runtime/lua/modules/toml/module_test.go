// SPDX-License-Identifier: MPL-2.0

package toml

import (
	"fmt"
	"testing"

	lua "github.com/wippyai/go-lua"
)

func newState(t *testing.T) *lua.LState {
	t.Helper()
	l := lua.NewState()
	t.Cleanup(l.Close)
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)
	return l
}

func TestLoad(t *testing.T) {
	l := newState(t)

	mod := l.GetGlobal("toml")
	if mod.Type() != lua.LTTable {
		t.Fatal("toml module not registered")
	}

	modTbl := mod.(*lua.LTable)
	if modTbl.RawGetString("encode").Type() != lua.LTFunction {
		t.Error("encode function not registered")
	}
	if modTbl.RawGetString("decode").Type() != lua.LTFunction {
		t.Error("decode function not registered")
	}
	if modTbl.RawGetString("insert") != lua.LNil {
		t.Error("insert must not be part of the surface")
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

	mod1 := l1.GetGlobal("toml").(*lua.LTable)
	mod2 := l2.GetGlobal("toml").(*lua.LTable)

	if mod1 != mod2 {
		t.Error("module table should be reused across states")
	}
}

func TestEncodeTable(t *testing.T) {
	l := newState(t)

	err := l.DoString(`
		local result, err = toml.encode({name = "test", value = 123})
		if not result then error(err) end
		if not result:find("name") then error("name not found in output") end
		if not result:find("test") then error("test value not found") end
		if not result:find("value = 123") then error("integer not written as integer: " .. result) end
	`)
	if err != nil {
		t.Errorf("encode table test failed: %v", err)
	}
}

func TestEncodeNestedTable(t *testing.T) {
	l := newState(t)

	err := l.DoString(`
		local result, err = toml.encode({parent = {child = {value = 123}}})
		if not result then error(err) end
		if not result:find("%[parent%.child%]") then error("nested header missing: " .. result) end
	`)
	if err != nil {
		t.Errorf("encode nested table test failed: %v", err)
	}
}

func TestEncodeArrayValue(t *testing.T) {
	l := newState(t)

	err := l.DoString(`
		local result, err = toml.encode({numbers = {1, 2, 3}, flags = {true, false}})
		if not result then error(err) end
		if not result:find("numbers = %[1, 2, 3%]") then error("array not written: " .. result) end
		if not result:find("flags = %[true, false%]") then error("boolean array not written: " .. result) end
	`)
	if err != nil {
		t.Errorf("encode array value test failed: %v", err)
	}
}

func TestEncodeArrayRoot(t *testing.T) {
	l := newState(t)

	err := l.DoString(`
		local result, err = toml.encode({1, 2, 3})
		if result ~= nil then
			error("expected nil result")
		end
		if err == nil then
			error("expected error")
		end
		if err:kind() ~= errors.INTERNAL then
			error("expected Internal kind, got: " .. tostring(err:kind()))
		end
		if err:retryable() ~= false then
			error("expected retryable to be false")
		end
	`)
	if err != nil {
		t.Errorf("encode array root test failed: %v", err)
	}
}

func TestEncodeInvalidInput(t *testing.T) {
	l := newState(t)

	err := l.DoString(`
		local result, err = toml.encode(123)
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
		if not tostring(err):find("table expected") then
			error("expected table expected message, got: " .. tostring(err))
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestEncodeMissingInput(t *testing.T) {
	l := newState(t)

	err := l.DoString(`
		local result, err = toml.encode()
		if result ~= nil then
			error("expected nil result")
		end
		if err == nil then
			error("expected error")
		end
		if err:kind() ~= errors.INVALID then
			error("expected Invalid kind, got: " .. tostring(err:kind()))
		end
		if not tostring(err):find("table expected") then
			error("expected table expected message, got: " .. tostring(err))
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestEncodeOutputByteLimit(t *testing.T) {
	l := newState(t)

	err := l.DoString(fmt.Sprintf(`
		local result, err = toml.encode({value = string.rep("x", %d)})
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
		if not tostring(err):find("output exceeds byte limit") then
			error("expected byte limit message, got: " .. tostring(err))
		end
	`, maxEncodeBytes+1))
	if err != nil {
		t.Errorf("encode output byte limit test failed: %v", err)
	}
}

func TestEncodeOutputAtByteLimit(t *testing.T) {
	l := newState(t)

	err := l.DoString(fmt.Sprintf(`
		local result, err = toml.encode({value = string.rep("x", %d)})
		if not result then error(err) end
		if #result > %d then error("output above limit was accepted") end
	`, maxEncodeBytes/2, maxEncodeBytes))
	if err != nil {
		t.Errorf("encode below byte limit test failed: %v", err)
	}
}

func TestDecodeTable(t *testing.T) {
	l := newState(t)

	err := l.DoString(`
		local result, err = toml.decode("name = 'test'\nvalue = 123\nratio = 1.5\nflag = true")
		if not result then error(err) end
		if result.name ~= "test" then error("name mismatch") end
		if result.value ~= 123 then error("value mismatch") end
		if math.type(result.value) ~= "integer" then error("integer decoded as " .. tostring(math.type(result.value))) end
		if result.ratio ~= 1.5 then error("ratio mismatch") end
		if math.type(result.ratio) ~= "float" then error("float decoded as " .. tostring(math.type(result.ratio))) end
		if result.flag ~= true then error("flag mismatch") end
	`)
	if err != nil {
		t.Errorf("decode table test failed: %v", err)
	}
}

func TestDecodeArray(t *testing.T) {
	l := newState(t)

	err := l.DoString(`
		local result, err = toml.decode("numbers = [1, 2, 3]")
		if not result then error(err) end
		if result.numbers[1] ~= 1 then error("first element mismatch") end
		if #result.numbers ~= 3 then error("array length mismatch") end
	`)
	if err != nil {
		t.Errorf("decode array test failed: %v", err)
	}
}

func TestDecodeArrayOfTables(t *testing.T) {
	l := newState(t)

	err := l.DoString(`
		local result, err = toml.decode("[[items]]\nname = 'a'\n\n[[items]]\nname = 'b'\n")
		if not result then error(err) end
		if #result.items ~= 2 then error("array of tables length mismatch") end
		if result.items[2].name ~= "b" then error("array of tables value mismatch") end
	`)
	if err != nil {
		t.Errorf("decode array of tables test failed: %v", err)
	}
}

func TestDecodeNestedStructure(t *testing.T) {
	l := newState(t)

	err := l.DoString(`
		local tomlStr = [[
[parent.child]
value = 123
]]
		local result, err = toml.decode(tomlStr)
		if not result then error(err) end
		if result.parent.child.value ~= 123 then error("nested value mismatch") end
	`)
	if err != nil {
		t.Errorf("decode nested structure test failed: %v", err)
	}
}

func TestDecodeDatesAsText(t *testing.T) {
	l := newState(t)

	err := l.DoString(`
		local result, err = toml.decode([[
odt = 1979-05-27T07:32:00Z
ldt = 1979-05-27T07:32:00
ld = 1979-05-27
lt = 07:32:00
nested = [{ at = 1979-05-27T07:32:00Z }]
]])
		if not result then error(err) end
		if result.odt ~= "1979-05-27T07:32:00Z" then error("offset date-time: " .. tostring(result.odt)) end
		if result.ldt ~= "1979-05-27T07:32:00" then error("local date-time: " .. tostring(result.ldt)) end
		if result.ld ~= "1979-05-27" then error("local date: " .. tostring(result.ld)) end
		if result.lt ~= "07:32:00" then error("local time: " .. tostring(result.lt)) end
		if result.nested[1].at ~= "1979-05-27T07:32:00Z" then error("nested date: " .. tostring(result.nested[1].at)) end
	`)
	if err != nil {
		t.Errorf("decode dates test failed: %v", err)
	}
}

func TestEncodeDoesNotSynthesizeDates(t *testing.T) {
	l := newState(t)

	err := l.DoString(`
		local result, err = toml.encode({at = "1979-05-27T07:32:00Z"})
		if not result then error(err) end
		if not result:find("'1979%-05%-27T07:32:00Z'") then
			error("date text must stay a string: " .. result)
		end
	`)
	if err != nil {
		t.Errorf("encode date text test failed: %v", err)
	}
}

func TestDecodeInvalidInput(t *testing.T) {
	l := newState(t)

	err := l.DoString(`
		local result, err = toml.decode(123)
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
		if not tostring(err):find("string expected") then
			error("expected string expected message, got: " .. tostring(err))
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestDecodeEmpty(t *testing.T) {
	l := newState(t)

	err := l.DoString(`
		local result, err = toml.decode("")
		if result ~= nil then
			error("expected nil result")
		end
		if err == nil then
			error("expected error")
		end
		if err:kind() ~= errors.INVALID then
			error("expected Invalid kind, got: " .. tostring(err:kind()))
		end
		if not tostring(err):find("input cannot be empty") then
			error("expected empty input message, got: " .. tostring(err))
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestDecodeInvalidTOML(t *testing.T) {
	l := newState(t)

	err := l.DoString(`
		local result, err = toml.decode("value = [1,")
		if result ~= nil then
			error("expected nil result")
		end
		if err == nil then
			error("expected error")
		end
		if err:kind() ~= errors.INTERNAL then
			error("expected Internal kind, got: " .. tostring(err:kind()))
		end
		if err:retryable() ~= false then
			error("expected retryable to be false")
		end
		if #tostring(err) == 0 then
			error("expected the parser failure in the message")
		end
		failure = err
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}

	luaErr, ok := lua.AsError(l.GetGlobal("failure"))
	if !ok {
		t.Fatal("decode did not return a structured error")
	}
	if luaErr.Context != "decode failed" {
		t.Errorf("expected decode failed context, got: %q", luaErr.Context)
	}
}

func TestDecodeInputByteLimit(t *testing.T) {
	l := newState(t)

	err := l.DoString(fmt.Sprintf(`
		local result, err = toml.decode(string.rep("x", %d))
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
		if not tostring(err):find("input exceeds byte limit") then
			error("expected byte limit message, got: " .. tostring(err))
		end
	`, maxDecodeBytes+1))
	if err != nil {
		t.Errorf("decode input byte limit test failed: %v", err)
	}
}

func TestDecodeInputAtByteLimit(t *testing.T) {
	l := newState(t)

	padding := maxDecodeBytes - len("value = ''\n")
	err := l.DoString(fmt.Sprintf(`
		local input = "value = '" .. string.rep("x", %d) .. "'\n"
		if #input ~= %d then error("test input is not exactly at the limit: " .. #input) end
		local result, err = toml.decode(input)
		if not result then error(err) end
		if #result.value ~= %d then error("value length mismatch") end
	`, padding, maxDecodeBytes, padding))
	if err != nil {
		t.Errorf("decode at byte limit test failed: %v", err)
	}
}

func TestRoundTrip(t *testing.T) {
	l := newState(t)

	err := l.DoString(`
		local original = {name = "test", numbers = {1, 2, 3}, nested = {flag = true, ratio = 1.5}}
		local encoded, err = toml.encode(original)
		if not encoded then error(err) end
		local decoded, err = toml.decode(encoded)
		if not decoded then error(err) end
		if decoded.name ~= "test" then error("name mismatch") end
		if #decoded.numbers ~= 3 then error("numbers mismatch") end
		if decoded.numbers[3] ~= 3 then error("numbers element mismatch") end
		if decoded.nested.flag ~= true then error("flag mismatch") end
		if decoded.nested.ratio ~= 1.5 then error("ratio mismatch") end
	`)
	if err != nil {
		t.Errorf("round trip test failed: %v", err)
	}
}

func TestRoundTripProviderConfig(t *testing.T) {
	l := newState(t)

	err := l.DoString(`
		local existing = "[mcp_servers.other]\nurl = 'http://127.0.0.1:1111/mcp'\n"
		local config, err = toml.decode(existing)
		if not config then error(err) end
		if config.mcp_servers.bee ~= nil then error("bee must be absent") end

		config.mcp_servers.bee = {url = "http://127.0.0.1:4321/mcp/action"}
		local encoded, err = toml.encode(config)
		if not encoded then error(err) end
		if not encoded:find("%[mcp_servers%.bee%]") then
			error("mcp_servers.bee header missing: " .. encoded)
		end

		local decoded, err = toml.decode(encoded)
		if not decoded then error(err) end
		if decoded.mcp_servers.bee.url ~= "http://127.0.0.1:4321/mcp/action" then
			error("bee url mismatch: " .. tostring(decoded.mcp_servers.bee.url))
		end
		if decoded.mcp_servers.other.url ~= "http://127.0.0.1:1111/mcp" then
			error("existing entry lost: " .. tostring(decoded.mcp_servers.other))
		end
	`)
	if err != nil {
		t.Errorf("provider config round trip test failed: %v", err)
	}
}
