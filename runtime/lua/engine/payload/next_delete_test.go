// SPDX-License-Identifier: MPL-2.0

package payload

import (
	"testing"

	lua "github.com/wippyai/go-lua"
)

func TestExportTableWithDeletedCursors(t *testing.T) {
	state := lua.NewState()
	defer state.Close()
	if err := state.DoString(`
local object = {}
local t = {1, 2, a = 3, b = 4, [false] = 5, [object] = 6, [100000] = 7}
for k in pairs(t) do
  if k == 'a' or k == false or k == object then t[k] = nil end
end
return t
`); err != nil {
		t.Fatal(err)
	}
	original := state.Get(-1).(*lua.LTable)
	originalB := original.RawGetString("b")
	copied := ExportPayload(original).Data().(*lua.LTable)
	state.SetGlobal("copied", copied)
	if err := state.DoString(`
local expected = {[1] = 1, [2] = 2, b = 4, [100000] = 7}
local count = 0
for k, v in pairs(copied) do
  count = count + 1
  assert(count <= 4 and expected[k] == v, 'payload traversal returned an unexpected key')
  expected[k] = nil
  copied[k] = nil
end
assert(count == 4, 'payload traversal skipped a live key after deletion')
assert(next(copied) == nil)
`); err != nil {
		t.Fatal(err)
	}
	count := 0
	original.ForEach(func(_, _ lua.LValue) { count++ })
	if count != 4 || original.RawGetString("b") != originalB {
		t.Fatalf("mutating exported table changed the source: count=%d, b=%v", count, original.RawGetString("b"))
	}
}

func TestExportDeletedSparseKeyThenArrayGrowth(t *testing.T) {
	original := lua.CreateTable(0, 0)
	original.RawSetInt(64, lua.LTrue)
	original.Next(lua.LNil)
	original.RawSetInt(64, lua.LNil)
	for i := 1; i <= 65; i++ {
		original.Append(lua.LNumber(i))
	}
	copied := ExportPayload(original).Data().(*lua.LTable)
	for _, tbl := range []*lua.LTable{original, copied} {
		count := 0
		for key, value := tbl.Next(lua.LNil); key != lua.LNil; key, value = tbl.Next(key) {
			count++
			if count > 65 || key != lua.LNumber(count) || value != key {
				t.Fatalf("invalid traversal at %v, %v", key, value)
			}
			tbl.RawSet(key, lua.LNil)
		}
		if count != 65 {
			t.Fatalf("visited %d of 65 array entries", count)
		}
	}
}
