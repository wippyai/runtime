// SPDX-License-Identifier: MPL-2.0

package json

import (
	"testing"

	lua "github.com/wippyai/go-lua"
)

type codeOutcome struct {
	code      lua.LValue
	kind      string
	hasErrors bool
}

func runValidateCode(t *testing.T, fn, schema, data string) codeOutcome {
	t.Helper()
	l := lua.NewState()
	defer l.Close()
	bindJSON(l)
	l.SetGlobal("schema_src", lua.LString(schema))
	l.SetGlobal("data_src", lua.LString(data))
	err := l.DoString(`
		local data = data_src
		if "` + fn + `" == "validate" then data = json.decode(data_src) end
		local ok, e = json.` + fn + `(schema_src, data)
		assert(ok == false, "expected validation failure")
		result_kind = e:kind()
		local d = e:details() or {}
		result_code = d.code
		result_has_errors = d.errors ~= nil
	`)
	if err != nil {
		t.Fatal(err)
	}
	return codeOutcome{
		code:      l.GetGlobal("result_code"),
		kind:      string(l.GetGlobal("result_kind").(lua.LString)),
		hasErrors: l.GetGlobal("result_has_errors") == lua.LTrue,
	}
}

func TestValidateCodeSeparatesSchemaFromInstance(t *testing.T) {
	cases := []struct {
		name      string
		schema    string
		data      string
		code      string
		hasErrors bool
	}{
		{"unresolved ref", `{"$ref":"#/$defs/missing"}`, `1`, "schema", false},
		{"nested unresolved ref", `{"type":"object","properties":{"a":{"$ref":"#/$defs/missing"}}}`, `{"a":1}`, "schema", false},
		{"malformed schema json", `{"type":`, `1`, "schema", false},
		{"schema of wrong type", `[1]`, `1`, "schema", false},
		{"instance mismatch", `{"type":"string"}`, `1`, "validation", true},
		{"instance mismatch under resolved ref", `{"$defs":{"s":{"type":"string"}},"$ref":"#/$defs/s"}`, `1`, "validation", true},
	}
	for _, fn := range []string{"validate", "validate_string"} {
		for _, tc := range cases {
			t.Run(fn+"/"+tc.name, func(t *testing.T) {
				out := runValidateCode(t, fn, tc.schema, tc.data)
				if out.kind != string(lua.Invalid) {
					t.Fatalf("kind = %s, want %s", out.kind, lua.Invalid)
				}
				if out.code.String() != tc.code {
					t.Fatalf("details.code = %v, want %s", out.code, tc.code)
				}
				if out.hasErrors != tc.hasErrors {
					t.Fatalf("details.errors present = %v, want %v", out.hasErrors, tc.hasErrors)
				}
			})
		}
	}
}
