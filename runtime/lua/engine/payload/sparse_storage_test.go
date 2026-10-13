// SPDX-License-Identifier: MPL-2.0

package payload

import (
	"testing"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/runtime/lua/modules/json"
)

func TestExportSparseStorageOrigin(t *testing.T) {
	for _, hashOrigin := range []bool{false, true} {
		name, wantJSON := "numeric array", "[]"
		if hashOrigin {
			name, wantJSON = "hash object", "{}"
		}
		t.Run(name, func(t *testing.T) {
			original := lua.CreateTable(0, 0)
			if hashOrigin {
				original.RawSetH(lua.LTrue, lua.LTrue)
				original.RawSetH(lua.LTrue, lua.LNil)
			}
			original.RawSetInt(100000, lua.LTrue)
			original.Next(lua.LNil) // Copy initialized iteration metadata too.
			copied := ExportPayload(original).Data().(*lua.LTable)
			if copied == original || cap(copied.Array) > 32 {
				t.Fatal("export did not preserve independent, bounded sparse storage")
			}
			copied.RawSetInt(100000, lua.LNil)
			if original.RawGetInt(100000) != lua.LTrue {
				t.Fatal("changing exported storage changed the source")
			}
			encoded, err := json.Encode(copied)
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != wantJSON {
				t.Fatalf("emptied exported table = %s, want %s", encoded, wantJSON)
			}
			key, value := original.Next(lua.LNil)
			if key == lua.LNil || value != lua.LTrue {
				t.Fatal("changing exported iteration metadata changed the source")
			}
		})
	}
}

func TestExportSparseNestedStorageIsIndependent(t *testing.T) {
	original := lua.CreateTable(0, 0)
	nested := lua.CreateTable(0, 0)
	nested.RawSetString("value", lua.LString("original"))
	original.RawSetInt(1, nested)
	original.RawSetInt(100000, nested)
	original.RawSetString("nested", nested)
	original.RawSetString("self", original)
	original.Next(lua.LNil)
	metatable := lua.CreateTable(0, 0)
	metatable.RawSetString("label", lua.LString("original"))
	original.Metatable = metatable
	original.Immutable = true

	copied := ExportPayload(original).Data().(*lua.LTable)
	child := copied.RawGetInt(100000).(*lua.LTable)
	if copied.Immutable || copied == original || child == nested {
		t.Fatal("export retained the original table or its immutability")
	}
	if copied.RawGetInt(1) != child || copied.RawGetString("nested") != child || copied.RawGetString("self") != copied {
		t.Fatal("export lost shared-value or cyclic-table identity")
	}
	child.RawSetString("value", lua.LString("copy"))
	copied.RawSetInt(1, lua.LNil)
	copied.RawSetInt(100000, lua.LNil)
	copied.RawSetString("nested", lua.LNil)
	copied.Metatable.(*lua.LTable).RawSetString("label", lua.LString("copy"))
	if nested.RawGetString("value") != lua.LString("original") || original.RawGetInt(1) != nested || original.RawGetInt(100000) != nested || original.RawGetString("nested") != nested || metatable.RawGetString("label") != lua.LString("original") {
		t.Fatal("export shared mutable backing storage with the source")
	}
	if cap(copied.Array) > 32 {
		t.Fatal("export expanded sparse storage")
	}
}
