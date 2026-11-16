// SPDX-License-Identifier: MPL-2.0

package json

import (
	"errors"
	"strings"
	"testing"

	lua "github.com/wippyai/go-lua"
)

func TestSparseStoragePreservesEmptyJSONShape(t *testing.T) {
	for _, hashOrigin := range []bool{false, true} {
		name, want := "numeric array", "[]"
		if hashOrigin {
			name, want = "hash object", "{}"
		}
		t.Run(name, func(t *testing.T) {
			table := lua.CreateTable(0, 0)
			if hashOrigin {
				table.RawSetH(lua.LTrue, lua.LTrue)
				table.RawSetH(lua.LTrue, lua.LNil)
			}
			table.RawSetInt(100000, lua.LFalse)
			if cap(table.Array) > 32 || table.RawGetInt(100000) != lua.LFalse {
				t.Fatal("large numeric key did not use bounded sparse storage")
			}
			table.RawSetInt(100000, lua.LNil)
			got, err := Encode(table)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != want {
				t.Fatalf("emptied table encoded as %s, want %s", got, want)
			}
		})
	}
}

func TestSparsePromotionPreservesDenseJSON(t *testing.T) {
	table := lua.CreateTable(0, 0)
	table.RawSetInt(40, lua.LTrue)
	for i := 1; i < 40; i++ {
		table.RawSetInt(i, lua.LTrue)
	}
	if table.Dict != nil || table.Len() != 40 {
		t.Fatal("gap filling did not restore dense array storage")
	}
	for i := 40; i > 0; i-- {
		table.RawSetInt(i, lua.LNil)
	}
	got, err := Encode(table)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "[]" {
		t.Fatalf("emptied promoted array encoded as %s, want []", got)
	}
}

func TestEncodeRoundTripsDecodedNulls(t *testing.T) {
	long := "[1" + strings.Repeat(",null", 40) + ",2]"
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"inner null", `[1,null,3]`, `[1,null,3]`},
		{"leading null", `[null,"a"]`, `[null,"a"]`},
		{"long null run", long, long},
		{"nested rows", `{"rows":[["a",null,"c"],[null,2]]}`, `{"rows":[["a",null,"c"],[null,2]]}`},
		{"trailing null", `["a",null]`, `["a"]`},
		{"only nulls", `[null,null]`, `[]`},
		{"null object value", `{"a":null,"b":1}`, `{"b":1}`},
		{"only null object value", `{"a":null}`, `{}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, err := Decode([]byte(tt.input))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			got, err := Encode(value)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			if string(got) != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestEncodeLuaArrayHolesAsNull(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindJSON(l)

	err := l.DoString(`
		local literal, err = json.encode({1, nil, 3})
		assert(err == nil, tostring(err))
		assert(literal == "[1,null,3]", literal)

		assert(json.encode({[1] = "a", [3] = "c"}) == '["a",null,"c"]')

		local cleared = {1, 2, 3}
		cleared[2] = nil
		assert(json.encode(cleared) == "[1,null,3]")

		local popped = {1, 2, 3}
		popped[3] = nil
		assert(json.encode(popped) == "[1,2]")
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestEncodeArrayGapFollowsLuaStorage(t *testing.T) {
	nulls := func(n int) string {
		return strings.Repeat("null,", n)
	}
	tests := []struct {
		name  string
		build string
		want  string
	}{
		{"gap of 32 slots", `{[1] = "a", [34] = "c"}`, `["a",` + nulls(32) + `"c"]`},
		{"gap of 33 slots", `{[1] = "a", [35] = "c"}`, ""},
		{"ascending writes", `{[1] = "a", [20] = "b", [40] = "c"}`, `["a",` + nulls(18) + `"b",` + nulls(19) + `"c"]`},
		{"distant write first", `{[1] = "a", [40] = "c", [20] = "b"}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()
			if err := l.DoString("value = " + tt.build); err != nil {
				t.Fatal(err)
			}
			got, err := Encode(l.GetGlobal("value"))
			if tt.want == "" {
				if !errors.Is(err, errSparseArray) {
					t.Fatalf("got %s, %v; want %v", got, err, errSparseArray)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestEncodeGoBuiltHashKeys(t *testing.T) {
	contiguous := lua.CreateTable(0, 0)
	for i := 1; i <= 3; i++ {
		contiguous.RawSetH(lua.LNumber(i), lua.LNumber(i))
	}
	decoded, err := Decode([]byte(`["a",null,"c"]`))
	if err != nil {
		t.Fatal(err)
	}
	filled := decoded.(*lua.LTable)
	filled.RawSetH(lua.LNumber(2), lua.LString("b"))

	tests := []struct {
		name  string
		table *lua.LTable
		want  string
	}{
		{"contiguous keys", contiguous, "[1,2,3]"},
		{"key fills array hole", filled, `["a","b","c"]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Encode(tt.table)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestEncodeRejectsSparseHashKeys(t *testing.T) {
	distant := lua.CreateTable(0, 0)
	distant.RawSetInt(1, lua.LString("a"))
	distant.RawSetInt(1000, lua.LString("c"))

	unrepresentable := lua.CreateTable(0, 0)
	unrepresentable.RawSetInt(1, lua.LString("a"))
	unrepresentable.RawSetH(lua.LNumber(1<<63), lua.LString("b"))

	tests := []struct {
		table *lua.LTable
		name  string
	}{
		{table: distant, name: "distant key"},
		{table: unrepresentable, name: "unrepresentable key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.table.Dict) == 0 {
				t.Fatal("key did not use hash storage")
			}
			if _, err := Encode(tt.table); !errors.Is(err, errSparseArray) {
				t.Fatalf("got %v, want %v", err, errSparseArray)
			}
		})
	}
}
