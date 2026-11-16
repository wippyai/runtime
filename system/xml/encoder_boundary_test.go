// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"math"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const unlimited = math.MaxInt32

type boundaryUnit struct {
	setup  func(*Encoder) error
	unit   func(*Encoder) error
	name   string
	indent string
	size   int
}

func noSetup(*Encoder) error { return nil }

func start(name string, attrs map[string]string) func(*Encoder) error {
	return func(w *Encoder) error { return w.StartElement(name, attrs, "") }
}

func steps(fs ...func(*Encoder) error) func(*Encoder) error {
	return func(w *Encoder) error {
		for _, f := range fs {
			if err := f(w); err != nil {
				return err
			}
		}
		return nil
	}
}

func TestEncoderTagBytesAtEveryUnit(t *testing.T) {
	units := []boundaryUnit{
		{name: "element name", setup: noSetup, unit: start("abcd", nil), size: len(`<abcd>`)},
		{name: "attribute", setup: noSetup, unit: start("a", map[string]string{"x": ""}), size: len(`<a x="">`)},
		{name: "qualified attribute", setup: noSetup, unit: start("a", map[string]string{"{u}x": "1"}), size: len(`<a xmlns:ns1="u" ns1:x="1">`)},
		{name: "declaration", setup: noSetup, size: len(`<a xmlns:p="u">`),
			unit: func(w *Encoder) error { return w.StartNode("a", "", nil, []Binding{{"p", "u"}}, false) }},
		{name: "default declaration", setup: noSetup, size: len(`<a xmlns="u">`),
			unit: func(w *Encoder) error { return w.StartNode("a", "u", nil, nil, false) }},
		{name: "self-close", setup: start("abcd", nil), unit: (*Encoder).EndElement, size: len(`<abcd/>`)},
		{name: "comment", setup: noSetup, unit: func(w *Encoder) error { return w.Comment("abc") }, size: len(`<!--abc-->`)},
		{name: "empty instruction", setup: noSetup, unit: func(w *Encoder) error { return w.instruction("t", nil) }, size: len(`<?t?>`)},
		{name: "instruction", setup: noSetup, unit: func(w *Encoder) error { return w.instruction("t", []byte("d")) }, size: len(`<?t d?>`)},
	}
	for _, u := range units {
		for _, limit := range []int{u.size - 1, u.size, u.size + 1} {
			w, err := NewEncoder(EncodeOptions{MaxTagBytes: limit, MaxNodeBytes: unlimited})
			require.NoError(t, err)
			require.NoError(t, u.setup(w), "%s setup at %d", u.name, limit)
			err = u.unit(w)
			if limit < u.size {
				assert.Equal(t, "max_tag_bytes", limitName(t, err), "%s at %d", u.name, limit)
				assert.LessOrEqual(t, len(w.Buffered()), limit, "%s at %d: bytes written", u.name, limit)
			} else {
				assert.NoError(t, err, "%s at %d", u.name, limit)
			}
		}
	}
}

func TestEncoderNodeBytesAtEveryUnit(t *testing.T) {
	units := []boundaryUnit{
		{name: "start tag", setup: noSetup, unit: start("abcd", nil)},
		{name: "attribute", setup: noSetup, unit: start("a", map[string]string{"x": "12345"})},
		{name: "declaration", setup: noSetup, unit: func(w *Encoder) error { return w.StartNode("a", "", nil, []Binding{{"p", "u"}}, false) }},
		{name: "text", setup: start("a", nil), unit: func(w *Encoder) error { return w.Text("x<y") }},
		{name: "comment", setup: start("a", nil), unit: func(w *Encoder) error { return w.Comment("c") }},
		{name: "instruction", setup: start("a", nil), unit: func(w *Encoder) error { return w.instruction("t", []byte("d")) }},
		{name: "end tag", setup: steps(start("ab", nil), func(w *Encoder) error { return w.Text("x") }), unit: (*Encoder).EndElement},
		{name: "self-close", setup: start("ab", nil), unit: (*Encoder).EndElement},
		{name: "indentation", indent: "    ", setup: start("a", nil), unit: start("b", nil)},
		{name: "closing indentation", indent: "    ", setup: steps(start("a", nil), start("b", nil), (*Encoder).EndElement), unit: (*Encoder).EndElement},
	}
	for _, u := range units {
		w, err := NewEncoder(EncodeOptions{Indent: u.indent, MaxNodeBytes: unlimited, MaxTagBytes: unlimited})
		require.NoError(t, err)
		w.BeginTree()
		require.NoError(t, u.setup(w))
		before := len(w.Buffered())
		require.NoError(t, u.unit(w))
		size := len(w.Buffered())
		require.Greater(t, size, before, u.name)
		for _, limit := range []int{size - 1, size, size + 1} {
			w, err := NewEncoder(EncodeOptions{Indent: u.indent, MaxNodeBytes: limit, MaxTagBytes: unlimited})
			require.NoError(t, err)
			w.BeginTree()
			require.NoError(t, u.setup(w), "%s setup at %d", u.name, limit)
			err = u.unit(w)
			if limit < size {
				assert.Equal(t, "max_node_bytes", limitName(t, err), "%s at %d", u.name, limit)
			} else {
				assert.NoError(t, err, "%s at %d", u.name, limit)
			}
			assert.LessOrEqual(t, len(w.Buffered()), limit, "%s at %d: bytes written", u.name, limit)
		}
	}
}

func TestEncoderIndentOption(t *testing.T) {
	_, err := NewEncoder(EncodeOptions{Indent: strings.Repeat(" ", 256)})
	require.NoError(t, err)
	_, err = NewEncoder(EncodeOptions{Indent: strings.Repeat("\t", 257)})
	codeIs(t, err, "argument")
	_, err = NewEncoder(EncodeOptions{Indent: " x"})
	codeIs(t, err, "argument")
}

func TestEncoderAdmitsNamesBeforeReserving(t *testing.T) {
	w, err := NewEncoder(EncodeOptions{MaxTagBytes: 1024})
	require.NoError(t, err)
	name := "a" + strings.Repeat("b", 16<<20)
	assert.Equal(t, "max_tag_bytes", limitName(t, w.StartElement(name, nil, "")))
	assert.Less(t, cap(w.Buffered()), 64<<10, "a rejected name reserves no storage")
}

func TestAdmissionArithmeticCannotOverflow(t *testing.T) {
	const maxInt = int64(math.MaxInt)
	for _, tc := range []struct {
		room, fixed, n int64
		fits           bool
	}{
		{100, 10, 15, true},
		{100, 10, 16, false},
		{100, 101, 0, false},
		{math.MaxInt64, 10, math.MaxInt32, true},
		{math.MaxInt64, 10, math.MaxInt64, false},
		{math.MaxInt64, 10, maxInt, maxInt <= (math.MaxInt64-10)/maxEscaped},
		{math.MaxInt64, 0, math.MaxInt64 / maxEscaped, true},
		{math.MaxInt64, 0, math.MaxInt64/maxEscaped + 1, false},
		{math.MaxInt32, 10, math.MaxInt32, false},
		{-5, 0, 0, false},
	} {
		assert.Equal(t, tc.fits, worstFits(tc.room, tc.fixed, tc.n), "room %d, fixed %d, n %d", tc.room, tc.fixed, tc.n)
	}
	tag, node := int64(100), int64(math.MaxInt64)
	assert.True(t, reserveWorst(&tag, &node, 4, 10))
	assert.Equal(t, int64(36), tag)
	assert.False(t, reserveWorst(&tag, &node, 4, maxInt), "a value of the largest length never fits")

	w, err := NewEncoder(EncodeOptions{MaxNodeBytes: 100, MaxTagBytes: math.MaxInt32})
	require.NoError(t, err)
	require.NoError(t, w.StartElement("a", nil, ""))
	assert.False(t, w.exceeds(maxInt), "outside a tree nothing exceeds the node bound")
	assert.Nil(t, w.admitMarkup(7, math.MaxInt32-7), "a markup unit of exactly the tag limit fits outside a tree")
	w.BeginTree()
	for _, n := range []int64{101, math.MaxInt32, math.MaxInt32 + 1, maxInt - 1, maxInt} {
		assert.True(t, w.exceeds(n), "%d bytes exceed the tree", n)
	}
	assert.Equal(t, "max_tag_bytes", limitName(t, w.admitMarkup(7, maxInt)))
	w, err = NewEncoder(EncodeOptions{MaxNodeBytes: 100})
	require.NoError(t, err)
	w.BeginTree()
	assert.Equal(t, "max_node_bytes", limitName(t, w.admitMarkup(7, 94)))
	w, err = NewEncoder(EncodeOptions{MaxNodeBytes: 100})
	require.NoError(t, err)
	w.BeginTree()
	assert.Nil(t, w.admitMarkup(7, 93))
}

func TestLongNamesAreAdmittedBeforeAnyCopy(t *testing.T) {
	long := strings.Repeat("a", 8<<20+64)
	attrs := map[string]string{}
	for i := range 64 {
		attrs[long[:8<<20+i]] = "v"
	}
	w, err := NewEncoder(EncodeOptions{})
	require.NoError(t, err)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	err = w.StartElement("e", attrs, "")
	runtime.ReadMemStats(&after)
	assert.Equal(t, "max_tag_bytes", limitName(t, err))
	assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(1<<20), "validating long names copies nothing")

	w, err = NewEncoder(EncodeOptions{MaxTagBytes: 64 << 20, MaxNodeBytes: 64 << 20})
	require.NoError(t, err)
	name := long[:4096]
	require.NoError(t, w.StartElement(name, map[string]string{name: "v"}, ""))
	require.NoError(t, w.EndElement())
	assert.Equal(t, "<"+name+" "+name+`="v"/>`, string(w.Buffered()))
	for _, slot := range w.valid {
		assert.LessOrEqual(t, len(slot), maxInternBytes, "names above the cutoff are not cached")
	}
}

func TestTreeElementNameIsAdmittedBeforeValidation(t *testing.T) {
	name := strings.Repeat("a", 2<<20) + "!"
	_, err := Encode(&Element{Name: name}, EncodeOptions{})
	assert.Equal(t, "max_tag_bytes", limitName(t, err), "an oversized name is rejected by length, not scanned")
}
