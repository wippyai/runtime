// SPDX-License-Identifier: MPL-2.0

package json

import (
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
