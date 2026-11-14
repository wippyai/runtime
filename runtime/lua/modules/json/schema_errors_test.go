// SPDX-License-Identifier: MPL-2.0

package json

import (
	"strings"
	"testing"

	lua "github.com/wippyai/go-lua"
)

type leafErr struct {
	path    string
	keyword string
	message string
	schema  string
}

type validateOutcome struct {
	message string
	kind    string
	leaves  []leafErr
	total   int
}

func runValidate(t *testing.T, fn, schema, data string) validateOutcome {
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
		result_message = e:message()
		result_kind = e:kind()
		result_details = e:details()
	`)
	if err != nil {
		t.Fatal(err)
	}
	out := validateOutcome{
		message: string(l.GetGlobal("result_message").(lua.LString)),
		kind:    string(l.GetGlobal("result_kind").(lua.LString)),
	}
	details, ok := l.GetGlobal("result_details").(*lua.LTable)
	if !ok {
		t.Fatalf("expected details table, message: %s", out.message)
	}
	out.total = int(details.RawGetString("total").(lua.LInteger))
	list := details.RawGetString("errors").(*lua.LTable)
	for i := 1; i <= list.Len(); i++ {
		e := list.RawGetInt(i).(*lua.LTable)
		out.leaves = append(out.leaves, leafErr{
			path:    e.RawGetString("instance_path").String(),
			keyword: e.RawGetString("keyword").String(),
			message: e.RawGetString("message").String(),
			schema:  e.RawGetString("schema_path").String(),
		})
	}
	return out
}

func findLeaf(t *testing.T, out validateOutcome, path, keyword string) leafErr {
	t.Helper()
	for _, e := range out.leaves {
		if e.path == path && e.keyword == keyword {
			return e
		}
	}
	t.Fatalf("leaf %s %s not found in %+v", path, keyword, out.leaves)
	return leafErr{}
}

func TestValidateErrorsIfIsNotViolation(t *testing.T) {
	schema := `{"type":"object","allOf":[{"if":{"properties":{"kind":{"const":"a"}},"required":["kind"]},
		"then":{"properties":{"title":{"const":"ok"}},"required":["title"]}}]}`
	out := runValidate(t, "validate_string", schema, `{"kind":"a","title":"bad"}`)
	if out.kind != string(lua.Invalid) {
		t.Fatalf("kind = %s", out.kind)
	}
	if len(out.leaves) != 1 {
		t.Fatalf("expected 1 leaf, got %+v", out.leaves)
	}
	leaf := out.leaves[0]
	if leaf.path != "/title" || leaf.keyword != "const" {
		t.Fatalf("unexpected leaf %+v", leaf)
	}
	if !strings.Contains(out.message, "/title") || !strings.Contains(out.message, "const") {
		t.Fatalf("message does not name leaf: %s", out.message)
	}
	if out.total != 1 {
		t.Fatalf("count = %d", out.total)
	}
}

func TestValidateErrorsDescendRef(t *testing.T) {
	schema := `{"$defs":{"g":{"type":"object","properties":{"method":{"type":"object",
		"properties":{"kind":{"const":"x"}},"required":["kind"]}},"required":["method"]}},
		"type":"object","properties":{"grading":{"$ref":"#/$defs/g"}}}`
	for _, fn := range []string{"validate_string", "validate"} {
		out := runValidate(t, fn, schema, `{"grading":{"method":{"kind":"y"}}}`)
		if len(out.leaves) != 1 {
			t.Fatalf("%s: expected 1 leaf, got %+v", fn, out.leaves)
		}
		leaf := findLeaf(t, out, "/grading/method/kind", "const")
		if !strings.Contains(leaf.schema, "$ref") {
			t.Fatalf("%s: schema path lacks $ref: %s", fn, leaf.schema)
		}
		if strings.Contains(out.message, "reference schema") {
			t.Fatalf("%s: message names wrapper: %s", fn, out.message)
		}
		if !strings.HasPrefix(out.message, "validation failed: /grading/method/kind") {
			t.Fatalf("%s: message = %s", fn, out.message)
		}
	}
}

func TestValidateErrorsRequiredBeatsMissingNull(t *testing.T) {
	schema := `{"type":"object","properties":{"field":{"type":"string"},"other":{"const":"x"}},"required":["field","other"]}`
	out := runValidate(t, "validate_string", schema, `{"other":"y"}`)
	req := findLeaf(t, out, "/field", "required")
	if !strings.Contains(req.message, "'field'") {
		t.Fatalf("required message = %s", req.message)
	}
	for _, e := range out.leaves {
		if e.path == "/field" && e.keyword != "required" {
			t.Fatalf("missing property reported as instance error: %+v", e)
		}
	}
	findLeaf(t, out, "/other", "const")
	if out.leaves[0].keyword != "required" {
		t.Fatalf("required must be first: %+v", out.leaves)
	}
	if out.message != "validation failed: /field: required: Required property 'field' is missing" {
		t.Fatalf("message = %s", out.message)
	}
	if out.total != 2 {
		t.Fatalf("total = %d", out.total)
	}
}

func TestValidateErrorsOrdering(t *testing.T) {
	schema := `{"type":"object","additionalProperties":false,
		"properties":{"a":{"type":"integer"},"b":{"enum":["x","y"]},
		"c":{"type":"object","properties":{"d":{"type":"string","minLength":3}}},"e":{"type":"string","minLength":5}},
		"required":["r"]}`
	out := runValidate(t, "validate_string", schema, `{"a":"s","b":"z","c":{"d":"q"},"e":"ab","zz":1}`)
	var order []string
	for _, e := range out.leaves {
		order = append(order, e.keyword+"@"+e.path)
	}
	want := []string{
		"required@/r", "additionalProperties@/zz", "enum@/b", "type@/a",
		"minLength@/c/d", "minLength@/e",
	}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v\nwant  %v", order, want)
	}
	if out.total != len(want) {
		t.Fatalf("count = %d", out.total)
	}
}

func TestValidateErrorsOneOfAndNestedArrays(t *testing.T) {
	schema := `{"type":"object","properties":{"items":{"type":"array","items":{"type":"object",
		"properties":{"n":{"type":"integer"},"v":{"oneOf":[{"const":1},{"const":2}]}},
		"required":["n"],"additionalProperties":false}}}}`
	out := runValidate(t, "validate_string", schema, `{"items":[{"n":1,"v":1},{"n":"s","v":3},{"v":1,"z":1}]}`)
	findLeaf(t, out, "/items/1/n", "type")
	findLeaf(t, out, "/items/1/v", "const")
	findLeaf(t, out, "/items/2/n", "required")
	findLeaf(t, out, "/items/2/z", "additionalProperties")
	for _, e := range out.leaves {
		if (e.path == "/items/2/n" && e.keyword != "required") || strings.HasPrefix(e.path, "/items/0") {
			t.Fatalf("spurious leaf %+v", e)
		}
	}
}

func TestValidateErrorsDetailsBounded(t *testing.T) {
	schema := `{"type":"array","items":{"type":"integer"}}`
	var b strings.Builder
	b.WriteString("[")
	for i := 0; i < 80; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`"s"`)
	}
	b.WriteString("]")
	out := runValidate(t, "validate_string", schema, b.String())
	if len(out.leaves) != 50 {
		t.Fatalf("leaves = %d", len(out.leaves))
	}
	if out.total != 80 {
		t.Fatalf("count = %d", out.total)
	}
	if strings.Contains(out.message, "more") {
		t.Fatalf("message = %s", out.message)
	}
}
