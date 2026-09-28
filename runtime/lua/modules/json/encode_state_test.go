// SPDX-License-Identifier: MPL-2.0

package json

import (
	"strconv"
	"sync"
	"testing"

	lua "github.com/wippyai/go-lua"
)

func newTestEncodeState(entriesCap int) *encodeState {
	return &encodeState{
		visited: make(map[*lua.LTable]bool),
		entries: make([]objectEntry, 0, entriesCap),
	}
}

// requireStateReleased fails when the state still references any table or
// value anywhere in its backing storage.
func requireStateReleased(t *testing.T, state *encodeState) {
	t.Helper()
	if len(state.visited) != 0 {
		t.Fatalf("visited retains %d tables", len(state.visited))
	}
	if len(state.entries) != 0 {
		t.Fatalf("entries stack not empty: len %d", len(state.entries))
	}
	for i, entry := range state.entries[:cap(state.entries)] {
		if entry.key != "" || entry.value != nil || entry.rank != 0 {
			t.Fatalf("entries[%d] retains key %q value %v", i, entry.key, entry.value)
		}
	}
}

// buildObject returns an object of the given width whose first key holds a
// nested object of the same shape, depth levels deep.
func buildObject(width, depth int) *lua.LTable {
	table := lua.CreateTable(0, width)
	for i := width - 1; i >= 0; i-- {
		table.RawSetString("k"+strconv.Itoa(i), lua.LString("v"+strconv.Itoa(i)))
	}
	if depth > 0 {
		table.RawSetString("k0", buildObject(width, depth-1))
	}
	return table
}

func TestEncodeStateReleasesEverythingAfterNestedEncode(t *testing.T) {
	// A small initial capacity forces the stack to grow while parent
	// segments are live, covering reallocation under nesting.
	state := newTestEncodeState(2)
	value := buildObject(9, 6)

	first, err := encodeWithState(value, &DefaultEncodeOptions, state)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	requireStateReleased(t, state)

	second, err := encodeWithState(value, &DefaultEncodeOptions, state)
	if err != nil {
		t.Fatalf("encode with reused state: %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("reused state changed output:\n%s\n%s", first, second)
	}
	requireStateReleased(t, state)
}

func TestEncodeStateReleasesEverythingAfterFailedEncode(t *testing.T) {
	state := newTestEncodeState(4)

	root := buildObject(5, 2)
	inner := root.RawGetString("k0").(*lua.LTable)
	inner.RawSetString("loop", root)

	if _, err := encodeWithState(root, &DefaultEncodeOptions, state); err == nil {
		t.Fatal("expected cyclic table to fail")
	}
	if len(state.entries) == 0 {
		t.Fatal("expected the failed encode to leave parent segments on the stack")
	}

	if !resetEncodeState(state) {
		t.Fatal("small state must be poolable")
	}
	requireStateReleased(t, state)
}

func TestEncodeAfterFailedEncodeIsUnaffected(t *testing.T) {
	cyclic := buildObject(6, 3)
	cyclic.RawGetString("k0").(*lua.LTable).RawSetString("loop", cyclic)
	valid := buildObject(6, 3)

	want, err := Encode(valid)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	for i := 0; i < 200; i++ {
		if _, err := Encode(cyclic); err == nil {
			t.Fatal("expected cyclic table to fail")
		}
		got, err := Encode(valid)
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if string(got) != string(want) {
			t.Fatalf("run %d: output changed after a failed encode:\n got %s\nwant %s", i, got, want)
		}
	}
}

func TestEncodeStateWithOversizedStackIsNotPooled(t *testing.T) {
	state := newTestEncodeState(4)
	if _, err := encodeWithState(buildObject(maxPooledEntries+1, 0), &DefaultEncodeOptions, state); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if resetEncodeState(state) {
		t.Fatalf("state with entries capacity %d must not be pooled", cap(state.entries))
	}
	requireStateReleased(t, state)
}

func TestEncodeConcurrentIsCanonical(t *testing.T) {
	input := []byte(`{"zeta":1,"alpha":{"yankee":true,"bravo":[{"delta":"d","charlie":"c"}]},"mike":"m","echo":2.5}`)
	want := `{"alpha":{"bravo":[{"charlie":"c","delta":"d"}],"yankee":true},"echo":2.5,"mike":"m","zeta":1}`

	var wg sync.WaitGroup
	errs := make(chan string, 16)
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, err := Decode(input)
			if err != nil {
				errs <- err.Error()
				return
			}
			for i := 0; i < 500; i++ {
				got, err := Encode(value)
				if err != nil {
					errs <- err.Error()
					return
				}
				if string(got) != want {
					errs <- "got " + string(got)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for msg := range errs {
		t.Fatal(msg)
	}
}
