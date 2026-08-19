// SPDX-License-Identifier: MPL-2.0

package json

import (
	"testing"

	lua "github.com/wippyai/go-lua"
)

// TestValidateSchemaWithEnum guards the build configuration. Under
// GOEXPERIMENT=jsonv2 the github.com/go-json-experiment/json import resolves to
// the standard library's encoding/json/v2 adoption, which treats
// errors.ErrUnsupported from a custom unmarshaler as fatal rather than as the
// "fall through to the default unmarshaler" signal the standalone package
// documents. github.com/kaptinlin/jsonschema relies on that signal in its *any
// unmarshaler, so with the experiment on this fails while compiling the schema,
// before any data is looked at:
//
//	json: cannot unmarshal into Go *interface {} within
//	"/properties/state/enum/0": unsupported operation
//
// enum is one of the most common JSON Schema keywords, so the failure takes out
// most real contract validation.
func TestValidateSchemaWithEnum(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindJSON(l)

	err := l.DoString(`
		local schema = {
			type = "object",
			additionalProperties = false,
			required = { "state" },
			properties = {
				state = { type = "string", enum = { "supported", "contradicted" } },
			},
		}
		local valid, err = json.validate(schema, { state = "supported" })
		if err then error("enum schema failed to compile: " .. tostring(err)) end
		if valid ~= true then error("enum schema rejected a permitted value") end

		local rejected = json.validate(schema, { state = "not_in_the_enum" })
		if rejected == true then error("enum schema admitted a value outside the enum") end
	`)
	if err != nil {
		t.Errorf("validate schema with enum failed: %v", err)
	}
}

// TestDecodeEncodeEmptyObject pins the object/array distinction. Lua has one
// table type, so the encoder tells empty objects and arrays apart by whether
// the table's string dictionary was allocated.
func TestDecodeEncodeEmptyObject(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindJSON(l)

	err := l.DoString(`
		local object = json.encode(json.decode("{}"))
		if object ~= "{}" then error("empty object re-encoded as " .. object) end

		local array = json.encode(json.decode("[]"))
		if array ~= "[]" then error("empty array re-encoded as " .. array) end

		local nested = json.encode(json.decode('{"inner":{}}'))
		if nested ~= '{"inner":{}}' then error("nested empty object re-encoded as " .. nested) end
	`)
	if err != nil {
		t.Errorf("empty object round trip failed: %v", err)
	}
}
