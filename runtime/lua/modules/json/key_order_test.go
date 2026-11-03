// SPDX-License-Identifier: MPL-2.0

package json

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

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

func TestEncodeMixedKeysWriteDistantIntegerKey(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	if err := l.DoString(`value = {"one", nil, "three", alpha = 1}; value[2^53] = "far"; value[2] = "two"`); err != nil {
		t.Fatal(err)
	}

	options := DefaultEncodeOptions
	options.TreatMixedKeysAsObjects = true

	done := make(chan struct{})
	var got []byte
	var err error
	go func() {
		defer close(done)
		got, err = EncodeWithOptions(l.GetGlobal("value"), &options)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("encoding a mixed table with one distant integer key did not finish")
	}

	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	want := `{"1":"one","2":"two","3":"three","9007199254740992":"far","alpha":1}`
	if string(got) != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
}

// go-lua stores integer keys outside [1, MaxArrayIndex) as LInteger; the
// encoder classifies them by value, like every other numeric key.
func TestEncodeIntegerKeysOutsideArrayIndexRange(t *testing.T) {
	tests := []struct {
		err   error
		name  string
		build string
		want  string
	}{
		{name: "zero key", build: `{[0] = "z", a = 1}`, want: `{"0":"z","a":1}`},
		{name: "negative key", build: `{[-1] = "m", a = 1}`, want: `{"-1":"m","a":1}`},
		{name: "only zero key", build: `{[0] = "z"}`, want: `{"0":"z"}`},
		{name: "large key with string keys", build: `{[1730000000] = "ts", a = 1}`, err: errInvalidKeys},
		{name: "only large key", build: `{[1730000000] = "ts"}`, err: errSparseArray},
		{name: "array then large key", build: `{"a", [1730000000] = "ts"}`, err: errSparseArray},
		{name: "fractional key", build: `{[1.5] = "x"}`, want: `{"1.5":"x"}`},
		{name: "min integer key", build: `{[math.mininteger] = "x"}`, want: `{"-9223372036854775808":"x"}`},
		{name: "max integer key", build: `{[math.maxinteger] = "x"}`, err: errSparseArray},
		{name: "float key beyond int64", build: `{[2^63] = "x"}`, err: errSparseArray},
		{name: "float key beyond int64 with string keys", build: `{[2^63] = "x", a = 1}`, err: errInvalidKeys},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()
			if err := l.DoString("value = " + tt.build); err != nil {
				t.Fatal(err)
			}
			got, err := Encode(l.GetGlobal("value"))
			if tt.err != nil {
				if !errors.Is(err, tt.err) {
					t.Fatalf("got %s, %v; want %v", got, err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			if string(got) != tt.want {
				t.Fatalf("got %s\nwant %s", got, tt.want)
			}
		})
	}
}

func TestEncodeMixedKeysWriteLargeIntegerKeysInOrder(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	if err := l.DoString(`value = {"a", b = 2}; value[1730000000] = "ts"; value[0] = "zero"; value[-5] = "neg"`); err != nil {
		t.Fatal(err)
	}
	options := DefaultEncodeOptions
	options.TreatMixedKeysAsObjects = true
	got, err := EncodeWithOptions(l.GetGlobal("value"), &options)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	want := `{"1":"a","1730000000":"ts","-5":"neg","0":"zero","b":2}`
	if string(got) != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
}

func TestEncodeSparseArrayErrorNamesTheKey(t *testing.T) {
	tests := []struct {
		build string
		want  string
	}{
		{build: `{[1730000000] = "ts"}`, want: "integer key 1730000000 past an array of 0 elements"},
		{build: `{"a", "b", [40] = "c"}`, want: "integer key 40 past an array of 2 elements"},
		{build: `{[2^63] = "x"}`, want: "integer key 9223372036854775808 past an array of 0 elements"},
	}
	for _, tt := range tests {
		t.Run(tt.build, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()
			if err := l.DoString("value = " + tt.build); err != nil {
				t.Fatal(err)
			}
			_, err := Encode(l.GetGlobal("value"))
			if !errors.Is(err, errSparseArray) {
				t.Fatalf("got %v, want %v", err, errSparseArray)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q does not name the key: want %q", err, tt.want)
			}
		})
	}
}

// Array positions at or above go-lua's MaxArrayIndex are LInteger hash keys;
// a contiguous array that crosses it still encodes in order.
func TestEncodeArrayCrossingMaxArrayIndex(t *testing.T) {
	saved := lua.MaxArrayIndex
	lua.MaxArrayIndex = 4
	t.Cleanup(func() { lua.MaxArrayIndex = saved })

	l := lua.NewState()
	defer l.Close()
	if err := l.DoString(`value = {"a", "b", "c"}; value[4] = "d"; value[5] = "e"; value[6] = "f"`); err != nil {
		t.Fatal(err)
	}
	got, err := Encode(l.GetGlobal("value"))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if want := `["a","b","c","d","e","f"]`; string(got) != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
}
