// SPDX-License-Identifier: MPL-2.0

package json

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/go-lua/types/typ"
)

var differentialTypes = []*lua.LType{
	lua.LTypeString, lua.LTypeInteger, lua.LTypeNumber, lua.LTypeBoolean,
	lua.NewLType(typ.NewOptional(typ.String)),
	lua.NewLType(typ.NewArray(typ.NewRecord().Field("id", typ.String).Build())),
	lua.NewLType(typ.NewRecord().Field("id", typ.String).OptField("age", typ.Integer).Build()),
	lua.NewLType(typ.NewMap(typ.String, typ.NewRecord().Field("id", typ.Integer).Build())),
	lua.NewLType(typ.NewUnion(typ.String, typ.Integer)),
	lua.NewLType(typ.LiteralString("ok")),
}

var differentialJSON = []string{
	`null`, `true`, `false`, `0`, `1.0`, `1e0`, `9223372036854775808`, `"ok"`, `"bad"`,
	`{"id":"a"}`, `{"id":1}`, `{"age":2}`, `{"id":"a","age":null}`,
	`{"id":"a","id":2}`, `{"id":1,"extra":true}`, `[null,{"id":"a"}]`,
	`{"a":{"id":1}}`, `[]`, `{}`, ``, `{`, `{"id":"a"} {}`,
}

func checkTypedDifferential(t *testing.T, target *lua.LType, raw string) {
	t.Helper()
	l := lua.NewState()
	defer l.Close()
	want, decodeErr := Decode([]byte(raw))
	wantOK := decodeErr == nil && want != lua.LNil && target.Validate(l, want)
	got, err := DecodeTyped([]byte(raw), target, l)
	if (err == nil) != wantOK {
		t.Fatalf("target=%s JSON=%q: got (%v,%v), oracle (%v,%v)", target, raw, got, err, want, decodeErr)
	}
	if wantOK && !reflect.DeepEqual(got, want) {
		t.Fatalf("target=%s JSON=%q: value differs: got %#v, want %#v", target, raw, got, want)
	}
}

func TestTypedDecodeDifferential(t *testing.T) {
	for _, target := range differentialTypes {
		for _, raw := range differentialJSON {
			checkTypedDifferential(t, target, raw)
		}
	}
	l := lua.NewState()
	defer l.Close()
	_, err := DecodeTyped([]byte(`{"items":[{"id":"a"},{"id":3}]}`),
		lua.NewLType(typ.NewRecord().Field("items", typ.NewArray(typ.NewRecord().Field("id", typ.String).Build())).Build()), l)
	if err == nil || !strings.Contains(err.Error(), "items[2].id") {
		t.Fatalf("want path-qualified error, got %v", err)
	}
}

func TestTypedDecodeOwnsStringsFromMutableInput(t *testing.T) {
	data := []byte(`{"id":"original"}`)
	value, err := DecodeTyped(data, lua.NewLType(typ.NewRecord().Field("id", typ.String).Build()), nil)
	if err != nil {
		t.Fatal(err)
	}
	copy(data, []byte(`{"id":"modified"}`))
	if got := value.(*lua.LTable).RawGetString("id"); got != lua.LString("original") {
		t.Fatalf("decoded string changed with input: %v", got)
	}
}

func TestTypedDecodeRejectsTopLevelNull(t *testing.T) {
	cases := []struct {
		target *lua.LType
		name   string
	}{
		{lua.NewLType(typ.Nil), "nil"},
		{lua.NewLType(typ.NewOptional(typ.String)), "optional"},
		{lua.NewLType(typ.NewUnion(typ.Nil, typ.String, typ.Boolean)), "nullable union"},
		{lua.NewLType(typ.Any), "any fallback"},
		{lua.NewLType(typ.NewOptional(typ.NewAnnotated(typ.String, []typ.Annotation{{Name: "min_len", Arg: float64(1)}}))), "annotated fallback"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value, err := DecodeTyped([]byte(`null`), tc.target, nil)
			if value != lua.LNil || err == nil || !strings.Contains(err.Error(), "top-level null") {
				t.Fatalf("expected null error, got (%v, %v)", value, err)
			}
		})
	}
}

func TestTypedDecodeAllowsNestedNull(t *testing.T) {
	checkTypedDifferential(t, lua.NewLType(typ.NewRecord().Field("id", typ.NewOptional(typ.String)).Build()), `{"id":null}`)
	checkTypedDifferential(t, lua.NewLType(typ.NewArray(typ.NewOptional(typ.String))), `[null,"ok"]`)
	checkTypedDifferential(t, lua.NewLType(typ.NewMap(typ.String, typ.NewOptional(typ.String))), `{"id":null}`)
}

func TestTypedDecodeFallbackShapes(t *testing.T) {
	cases := []struct {
		name      string
		typeValue typ.Type
		raw       string
	}{
		{"open record", typ.NewRecord().Field("id", typ.String).SetOpen(true).Build(), `{"id":"ok","extra":1}`},
		{"large record", largeFallbackRecord(), `{}`},
		{"union of records", typ.NewUnion(typ.NewRecord().Field("a", typ.String).Build(), typ.NewRecord().Field("b", typ.Integer).Build()), `{"a":"ok"}`},
		{"non-string map key", typ.NewMap(typ.Integer, typ.String), `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if compilePlan(tc.typeValue, 0) != nil {
				t.Fatal("expected validator fallback")
			}
			checkTypedDifferential(t, lua.NewLType(tc.typeValue), tc.raw)
		})
	}
}

func TestTypedDecodeAnnotatedDifferential(t *testing.T) {
	boundedNumber := typ.NewAnnotated(typ.Number, []typ.Annotation{
		{Name: "min", Arg: float64(0)},
		{Name: "max", Arg: float64(100)},
	})
	boundedString := typ.NewAnnotated(typ.String, []typ.Annotation{
		{Name: "min_len", Arg: float64(2)},
		{Name: "pattern", Arg: "^[a-z]+$"},
	})
	cases := []struct {
		typeValue typ.Type
		name      string
		inputs    []string
	}{
		{boundedNumber, "top-level number", []string{`-1`, `0`, `50`, `100`, `101`, `"50"`}},
		{boundedString, "top-level string", []string{`"a"`, `"ab"`, `"AB"`, `"ab1"`, `null`}},
		{typ.NewRecord().Field("age", boundedNumber).Build(), "record field", []string{`{"age":-1}`, `{"age":50}`, `{"age":101}`}},
		{typ.NewArray(boundedNumber), "array element", []string{`[-1]`, `[0,50,100]`, `[50,101]`}},
		{typ.NewMap(typ.String, boundedString), "map value", []string{`{"id":"a"}`, `{"id":"ab"}`, `{"id":"AB"}`}},
		{typ.NewMap(boundedString, typ.String), "map key", []string{`{"ab":"value"}`, `{"AB":"value"}`}},
		{typ.NewOptional(boundedNumber), "optional", []string{`null`, `-1`, `50`, `101`}},
		{typ.NewUnion(boundedNumber, typ.Boolean), "union member", []string{`true`, `false`, `-1`, `50`, `101`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, raw := range tc.inputs {
				checkTypedDifferential(t, lua.NewLType(tc.typeValue), raw)
			}
			if compilePlan(tc.typeValue, 0) != nil {
				t.Fatal("annotated types must use the runtime validator")
			}
		})
	}
}

func largeFallbackRecord() typ.Type {
	b := typ.NewRecord()
	for i := 0; i < 65; i++ {
		b.OptField(strconv.Itoa(i), typ.String)
	}
	return b.Build()
}

func FuzzTypedDecodeDifferential(f *testing.F) {
	for i := range differentialTypes {
		for _, raw := range differentialJSON {
			f.Add(uint8(i), raw)
		}
	}
	f.Fuzz(func(t *testing.T, typeIndex uint8, raw string) {
		if len(raw) > 4096 {
			return
		}
		checkTypedDifferential(t, differentialTypes[int(typeIndex)%len(differentialTypes)], raw)
	})
}
