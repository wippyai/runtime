// SPDX-License-Identifier: MPL-2.0

package json

import (
	"reflect"
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
	wantOK := decodeErr == nil && target.Validate(l, want)
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
