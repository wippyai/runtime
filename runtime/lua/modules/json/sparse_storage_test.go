// SPDX-License-Identifier: MPL-2.0

package json

import (
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
