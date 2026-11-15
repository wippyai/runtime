// SPDX-License-Identifier: MPL-2.0

package json

import (
	"testing"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/go-lua/compiler/check/tests/testutil"
)

func runJSONLua(t *testing.T, source string) {
	t.Helper()
	l := lua.NewState()
	defer l.Close()
	bindJSON(l)
	if err := l.DoString(source); err != nil {
		t.Fatal(err)
	}
}

func TestNullEncodesAsJSONNull(t *testing.T) {
	runJSONLua(t, `
		assert(json.null ~= nil, "json.null must exist")
		assert(json.encode(json.null) == "null", "top-level json.null: " .. json.encode(json.null))
		local got = json.encode({a = json.null, b = {1, json.null, 3, json.null}, c = "x"})
		local want = '{"a":null,"b":[1,null,3,null],"c":"x"}'
		assert(got == want, "got " .. got .. " want " .. want)
	`)
}

func TestNullIsOneValueAcrossModuleBuilds(t *testing.T) {
	first, _ := Module.Build()
	second, _ := Module.Build()
	null := first.RawGetString("null")
	if null.Type() != lua.LTUserData {
		t.Fatalf("json.null type = %s, want userdata", null.Type())
	}
	if null != second.RawGetString("null") {
		t.Fatal("json.null differs between module builds")
	}
}

func TestDecodeWithNullsKeepsNullMembersAndElements(t *testing.T) {
	runJSONLua(t, `
		local text = '{"a":null,"b":[null,2,null],"c":{"d":null}}'
		local v, err = json.decode_with_nulls(text)
		assert(err == nil, tostring(err))
		assert(v.a == json.null, "a must be json.null")
		assert(#v.b == 3, "b length " .. #v.b)
		assert(v.b[1] == json.null and v.b[2] == 2 and v.b[3] == json.null, "b elements")
		assert(v.c.d == json.null, "c.d must be json.null")
		local got = json.encode(v)
		assert(got == text, "round trip got " .. got)
	`)
}

func TestDecodeWithNullsTopLevelNull(t *testing.T) {
	runJSONLua(t, `
		local v, err = json.decode_with_nulls("null")
		assert(err == nil, tostring(err))
		assert(v == json.null, "top-level null must be json.null")
	`)
}

func TestDecodeWithNullsRejectsInvalidInput(t *testing.T) {
	runJSONLua(t, `
		local v, err = json.decode_with_nulls('{"a":')
		assert(v == nil and err ~= nil, "malformed JSON must fail")
		v, err = json.decode_with_nulls("")
		assert(v == nil and err:kind() == errors.INVALID, "empty input must be INVALID")
		v, err = json.decode_with_nulls(42)
		assert(v == nil and err:kind() == errors.INVALID, "non-string input must be INVALID")
	`)
}

func TestDecodeDropsNullMembers(t *testing.T) {
	runJSONLua(t, `
		local v = json.decode('{"a":null,"b":1}')
		assert(v.a == nil and v.b == 1, "decode keeps null members absent")
	`)
}

func TestValidateTreatsNullAsJSONNull(t *testing.T) {
	runJSONLua(t, `
		local schema = {type = "object", properties = {a = {type = "null"}}, required = {"a"}}
		local ok, err = json.validate(schema, {a = json.null})
		assert(ok, "json.null must satisfy type null: " .. tostring(err))
		ok = json.validate({type = "null"}, json.null)
		assert(ok, "top-level json.null must satisfy type null")
		ok = json.validate({type = "object", properties = {a = {type = "string"}}}, {a = json.null})
		assert(not ok, "json.null must not satisfy type string")
	`)
}

func TestRawEncodesTextWithMemberOrder(t *testing.T) {
	runJSONLua(t, `
		local schema = '{"type":"object","properties":{"zeta":{"type":"string"},"alpha":{"type":"number"}},"required":["zeta"]}'
		local raw, err = json.raw(schema)
		assert(err == nil, tostring(err))
		assert(json.encode(raw) == schema, "top-level raw: " .. json.encode(raw))
		local got = json.encode({name = "t", parameters = raw})
		local want = '{"name":"t","parameters":' .. schema .. '}'
		assert(got == want, "got " .. got .. " want " .. want)
		local list = json.encode({raw, json.raw("1.50")})
		assert(list == "[" .. schema .. ",1.50]", "array: " .. list)
	`)
}

func TestRawStoresCompactText(t *testing.T) {
	runJSONLua(t, `
		local raw = json.raw(' { "b" : [ 1 , 2 ] ,\n "a" : "x y" } ')
		assert(json.encode(raw) == '{"b":[1,2],"a":"x y"}', "top-level: " .. json.encode(raw))
		assert(json.encode({v = raw}) == '{"v":{"b":[1,2],"a":"x y"}}', "nested: " .. json.encode({v = raw}))
	`)
}

func TestRawRejectsInvalidJSON(t *testing.T) {
	runJSONLua(t, `
		for _, text in ipairs({'{"a":', "", "  ", "1 2", '{"a":1,"a":2}', "nope", "1e400", '"\255"'}) do
			local raw, err = json.raw(text)
			assert(raw == nil, "accepted " .. text)
			assert(err:kind() == errors.INVALID, "kind for " .. text)
		end
		local raw, err = json.raw({})
		assert(raw == nil and err:kind() == errors.INVALID, "non-string input must be INVALID")
	`)
}

func TestNullAndRawTypes(t *testing.T) {
	source := `
		local json = require("json")
		local function build(text: string): string?
			local schema, err = json.raw(text)
			if err then return nil end
			local args, decode_err = json.decode_with_nulls('{"a":null}')
			if decode_err then return nil end
			local present = args.a == json.null
			local encoded = json.encode({schema = schema, present = present, none = json.null})
			return encoded
		end
		return build
	`
	options := []testutil.Option{testutil.WithStdlib(), testutil.WithManifest("json", ModuleTypes())}
	if result := testutil.Check(source, options...); result.HasError() {
		for _, d := range result.Errors {
			t.Log(d.Message)
		}
		t.Fatal("json.null, json.raw and json.decode_with_nulls must type-check")
	}
}

func TestValidateReadsRawAsItsJSONValue(t *testing.T) {
	runJSONLua(t, `
		local schema = {type = "object", properties = {s = {type = "object", required = {"a"}}}}
		local ok, err = json.validate(schema, {s = json.raw('{"a":1}')})
		assert(ok, "raw object must validate as an object: " .. tostring(err))
		ok = json.validate(schema, {s = json.raw('{"b":1}')})
		assert(not ok, "raw object without a must fail")
		ok, err = json.validate({type = "integer"}, json.raw("5"))
		assert(ok, "top-level raw number: " .. tostring(err))
	`)
}

func TestValidateAcceptsRawSchema(t *testing.T) {
	runJSONLua(t, `
		local schema = json.raw('{"type":"object","properties":{"zeta":{"type":"string"}},"required":["zeta"]}')
		local ok, err = json.validate(schema, {zeta = "z"})
		assert(ok, "raw schema must validate: " .. tostring(err))
		ok = json.validate(schema, {zeta = 1})
		assert(not ok, "raw schema must reject a number")
		ok, err = json.validate_string(schema, '{"zeta":"z"}')
		assert(ok, "raw schema must validate a string: " .. tostring(err))
		ok, err = json.validate(json.null, {zeta = "z"})
		assert(not ok and err:kind() == errors.INVALID, "json.null is not a schema")
	`)
}
