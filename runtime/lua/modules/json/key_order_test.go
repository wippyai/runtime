// SPDX-License-Identifier: MPL-2.0

package json

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	lua "github.com/wippyai/go-lua"
)

// Equal values encode to identical bytes: object keys are written in
// ascending byte order regardless of how the table was built.

func TestEncodeDecodedObjectKeysAreCanonical(t *testing.T) {
	input := []byte(`{"zeta":1,"alpha":{"yankee":true,"bravo":[{"delta":"d","charlie":"c"}],"xray":null},"mike":"m","echo":2.5}`)
	want := `{"alpha":{"bravo":[{"charlie":"c","delta":"d"}],"yankee":true},"echo":2.5,"mike":"m","zeta":1}`

	for i := 0; i < 64; i++ {
		value, err := Decode(input)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		got, err := Encode(value)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		if string(got) != want {
			t.Fatalf("run %d:\n got %s\nwant %s", i, got, want)
		}
	}
}

func TestEncodeLuaBuiltObjectKeysAreCanonical(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindJSON(l)

	err := l.DoString(`
		local want = '{"content":"c","draft_id":"d1","style":"corporate","title":"t","total":3}'
		for i = 1, 64 do
			local got = json.encode({
				title = "t", draft_id = "d1", total = 3, style = "corporate", content = "c",
			})
			if got ~= want then
				error("run " .. i .. ": got " .. got .. " want " .. want)
			end
		end
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestEncodeNonStringHashKeysAreCanonical(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindJSON(l)

	err := l.DoString(`
		local want = '{"1.5":"half","b":"bee","true":"yes"}'
		for i = 1, 64 do
			local got = json.encode({ [true] = "yes", b = "bee", [1.5] = "half" })
			if got ~= want then
				error("run " .. i .. ": got " .. got .. " want " .. want)
			end
		end
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestEncodeMixedKeysWriteNumericKeysFirstThenCanonical(t *testing.T) {
	table := lua.CreateTable(2, 4)
	table.Append(lua.LString("one"))
	table.Append(lua.LString("two"))
	table.RawSetString("zulu", lua.LNumber(26))
	table.RawSetString("alpha", lua.LNumber(1))
	table.RawSetString("mike", lua.LNumber(13))

	want := `{"1":"one","2":"two","alpha":1,"mike":13,"zulu":26}`
	options := DefaultEncodeOptions
	options.TreatMixedKeysAsObjects = true

	for i := 0; i < 64; i++ {
		got, err := EncodeWithOptions(table, &options)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		if string(got) != want {
			t.Fatalf("run %d:\n got %s\nwant %s", i, got, want)
		}
	}
}

func TestEncodeWideObjectKeysAreCanonical(t *testing.T) {
	for _, width := range []int{maxInsertionSortEntries, maxInsertionSortEntries + 1, 200} {
		table := lua.CreateTable(0, width)
		keys := make([]string, 0, width)
		for i := width - 1; i >= 0; i-- {
			key := fmt.Sprintf("key%04d", i)
			keys = append(keys, key)
			table.RawSetString(key, lua.LNumber(i))
		}
		slices.Sort(keys)

		var want strings.Builder
		want.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				want.WriteByte(',')
			}
			n, _ := strconv.Atoi(strings.TrimPrefix(key, "key"))
			fmt.Fprintf(&want, "%q:%d", key, n)
		}
		want.WriteByte('}')

		for run := 0; run < 16; run++ {
			got, err := Encode(table)
			if err != nil {
				t.Fatalf("width %d: %v", width, err)
			}
			if string(got) != want.String() {
				t.Fatalf("width %d run %d:\n got %s\nwant %s", width, run, got, want.String())
			}
		}
	}
}

func TestEncodeEqualWrittenKeysKeepFixedOrder(t *testing.T) {
	// A string key and a boolean key both write "true"; the string key comes
	// first. Padding keys push the same object onto the wide sort path.
	for _, padding := range []int{0, maxInsertionSortEntries + 4} {
		table := lua.CreateTable(0, padding+2)
		for i := 0; i < padding; i++ {
			table.RawSetString(fmt.Sprintf("p%02d", i), lua.LNumber(i))
		}
		table.RawSetString("true", lua.LString("string"))
		table.RawSetH(lua.LTrue, lua.LString("bool"))

		first, err := Encode(table)
		if err != nil {
			t.Fatalf("padding %d: %v", padding, err)
		}
		if !strings.HasSuffix(string(first), `"true":"string","true":"bool"}`) {
			t.Fatalf("padding %d: got %s", padding, first)
		}
		for run := 0; run < 32; run++ {
			got, err := Encode(table)
			if err != nil {
				t.Fatalf("padding %d: %v", padding, err)
			}
			if string(got) != string(first) {
				t.Fatalf("padding %d run %d:\n got %s\nwant %s", padding, run, got, first)
			}
		}
	}
}
