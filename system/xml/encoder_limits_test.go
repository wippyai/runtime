// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
)

func limitName(t *testing.T, err error) string {
	t.Helper()
	require.Error(t, err)
	var e apierror.Error
	require.ErrorAs(t, err, &e)
	require.Equal(t, "limit", detail(e, "code"))
	return fmt.Sprint(detail(e, "limit"))
}

func TestEncodeSharedSubtreesStopAtNodeBytes(t *testing.T) {
	e := &Element{Name: "x"}
	for range 40 {
		e = &Element{Name: "x", Children: []any{e, e}}
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	started := time.Now()
	_, err := Encode(e, EncodeOptions{})
	elapsed := time.Since(started)
	runtime.ReadMemStats(&after)
	assert.Equal(t, "max_node_bytes", limitName(t, err))
	assert.Less(t, elapsed, raceSlowdown*500*time.Millisecond)
	assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(64<<20), "work stops at the limit")
}

func TestEncodeNodeBytesBoundary(t *testing.T) {
	tree := &Element{Name: "a", Children: []any{&Element{Name: "b", Text: "0123456789"}, "tail"}}
	out, err := Encode(tree, EncodeOptions{})
	require.NoError(t, err)
	size := len(out)
	_, err = Encode(tree, EncodeOptions{MaxNodeBytes: size})
	require.NoError(t, err, "a tree of exactly max_node_bytes fits")
	_, err = Encode(tree, EncodeOptions{MaxNodeBytes: size - 1})
	assert.Equal(t, "max_node_bytes", limitName(t, err))

	w, err := NewEncoder(EncodeOptions{MaxNodeBytes: 32})
	require.NoError(t, err)
	require.NoError(t, w.StartElement("root", nil, ""))
	for range 10 {
		w.BeginTree()
		require.NoError(t, w.Element(&Element{Name: "item", Text: "0123456789"}))
		w.EndTree()
	}
	assert.Greater(t, len(w.Buffered()), 32, "the limit applies to each tree, not the document")
	assert.Equal(t, "max_node_bytes", limitName(t, w.Element(&Element{Name: "item", Text: strings.Repeat("x", 40)})))

	big := &Element{Name: "doc", Children: []any{&Element{Name: "v", Text: strings.Repeat("y", 1<<20)}}}
	out, err = Encode(big, EncodeOptions{MaxNodeBytes: 2 << 20})
	require.NoError(t, err)
	back, err := Decode(out, Options{MaxNodeBytes: 2 << 20})
	require.NoError(t, err)
	assert.Equal(t, dump(big), dump(back), "a tree under the limit round-trips")
}

func TestEncoderStructuralLimits(t *testing.T) {
	nested := func(depth int) *Element {
		e := &Element{Name: "leaf"}
		for range depth - 1 {
			e = &Element{Name: "n", Children: []any{e}}
		}
		return e
	}
	_, err := Encode(nested(4), EncodeOptions{MaxDepth: 4})
	require.NoError(t, err)
	_, err = Encode(nested(5), EncodeOptions{MaxDepth: 4})
	assert.Equal(t, "max_depth", limitName(t, err))

	attrs := &Element{Name: "a", Attrs: map[string]string{"x": "1", "y": "2", "z": "3"}, XMLNS: map[string]string{"p": "u"}}
	_, err = Encode(attrs, EncodeOptions{MaxAttrs: 4})
	require.NoError(t, err)
	_, err = Encode(attrs, EncodeOptions{MaxAttrs: 3})
	assert.Equal(t, "max_attrs", limitName(t, err))
	generated := &Element{Name: "a", Attrs: map[string]string{"{u}k": "1"}}
	_, err = Encode(generated, EncodeOptions{MaxAttrs: 2})
	require.NoError(t, err)
	_, err = Encode(generated, EncodeOptions{MaxAttrs: 1})
	assert.Equal(t, "max_attrs", limitName(t, err), "generated declarations count")

	scoped := &Element{Name: "a", XMLNS: map[string]string{"p": "1", "q": "2"},
		Children: []any{&Element{Name: "b", XMLNS: map[string]string{"r": "3"}, Children: []any{&Element{Name: "c", XMLNS: map[string]string{"p": "4"}}}}}}
	_, err = Encode(scoped, EncodeOptions{MaxNamespaces: 4})
	require.NoError(t, err, "shadowed declarations count and four fit")
	_, err = Encode(scoped, EncodeOptions{MaxNamespaces: 3})
	assert.Equal(t, "max_namespaces", limitName(t, err))
	_, err = Encode(&Element{Name: "a"}, EncodeOptions{MaxNamespaces: 1, NS: map[string]string{"s": "1", "t": "2"}})
	assert.Equal(t, "max_namespaces", limitName(t, err), "preferred bindings count")

	w, err := NewEncoder(EncodeOptions{MaxAttrs: 1})
	require.NoError(t, err)
	assert.Equal(t, "max_attrs", limitName(t, w.StartElement("a", map[string]string{"x": "1", "y": "2"}, "")), "incremental writes share the limits")

	d, err := NewDecoder(context.Background(), strings.NewReader(`<a x="1" y="2" z="3"/>`), Options{})
	require.NoError(t, err)
	_, err = d.NextEvent()
	require.NoError(t, err)
	w, err = NewEncoder(EncodeOptions{MaxAttrs: 2})
	require.NoError(t, err)
	assert.Equal(t, "max_attrs", limitName(t, d.Copy(w)), "copies share the structural limits")

	for _, o := range []EncodeOptions{{MaxNodeBytes: -1}, {MaxAttrs: -1}, {MaxNamespaces: -1}, {MaxDepth: -1}} {
		_, err := NewEncoder(o)
		codeIs(t, err, "argument")
	}
}

func TestEncoderFinishIsSticky(t *testing.T) {
	w, err := NewEncoder(EncodeOptions{})
	require.NoError(t, err)
	require.NoError(t, w.Element(&Element{Name: "a"}))
	require.NoError(t, w.Finish())
	again := w.Finish()
	codeIs(t, again, "state")
	assert.Same(t, again, w.Finish())
	assert.Same(t, again, w.Err())

	w, err = NewEncoder(EncodeOptions{})
	require.NoError(t, err)
	require.NoError(t, w.Element(&Element{Name: "a"}))
	require.NoError(t, w.Finish())
	first := w.Flush(&chunkWriter{limit: 1, fail: context.Canceled})
	require.Error(t, first)
	assert.Same(t, first, w.Finish(), "finish after a failed write returns that failure")
	assert.Same(t, first, w.Err())
}

// encodeWide writes one element with n qualified attributes in n namespaces under
// n declarations in scope, and returns the time it takes.
func encodeWide(t *testing.T, n int) time.Duration {
	t.Helper()
	inner := &Element{Name: "leaf", Attrs: map[string]string{}}
	for i := range n {
		inner.Attrs[fmt.Sprintf("{urn:a:%d}k", i)] = "v"
	}
	e := inner
	for i := range n / 4 {
		e = &Element{Name: "n", XMLNS: map[string]string{
			fmt.Sprintf("a%d", i): fmt.Sprintf("urn:d:%d:a", i), fmt.Sprintf("b%d", i): fmt.Sprintf("urn:d:%d:b", i),
			fmt.Sprintf("c%d", i): fmt.Sprintf("urn:d:%d:c", i), fmt.Sprintf("d%d", i): fmt.Sprintf("urn:d:%d:d", i),
		}, Children: []any{e}}
	}
	best := time.Duration(1 << 62)
	for range 3 {
		started := time.Now()
		_, err := Encode(e, EncodeOptions{MaxAttrs: 2 * n, MaxNamespaces: 3 * n, MaxDepth: n})
		require.NoError(t, err)
		best = min(best, time.Since(started))
	}
	return best
}

func TestEncoderNamespacesScaleLinearly(t *testing.T) {
	small := encodeWide(t, 1024)
	large := encodeWide(t, 8192)
	t.Logf("n=1024: %v, n=8192: %v", small, large)
	assert.Less(t, large, small*30, "eight times the bindings take far less than sixty-four times the work")
}

// peak returns the bytes held and the capacity of the encoder buffer after a failed write;
// within one Encode the buffer only grows, so these are the peaks.
func peak(w *Encoder) (int, int) { return len(w.Buffered()), cap(w.Buffered()) }

func TestEncoderRejectsBeforeAppending(t *testing.T) {
	const limit = 1024
	const slack = 256
	value := strings.Repeat("v", 1<<20)
	wide := &Element{Name: "a", Attrs: map[string]string{}}
	for i := range 256 {
		wide.Attrs[fmt.Sprintf("k%03d", i)] = value
	}
	escaped := &Element{Name: "a", Text: strings.Repeat("<", 1<<20)}
	comment := strings.Repeat("c", 1<<20)
	generated := &Element{Name: "a", Attrs: map[string]string{}}
	for i := range 200 {
		generated.Attrs[fmt.Sprintf("{urn:%s:%d}k", strings.Repeat("u", 5000), i)] = "v"
	}
	cases := []struct {
		write func(*Encoder) error
		name  string
		limit string
		opts  EncodeOptions
	}{
		{name: "shared attribute value, node bound", limit: "max_node_bytes", opts: EncodeOptions{MaxNodeBytes: limit, MaxTagBytes: 1 << 30},
			write: func(w *Encoder) error { return w.Element(wide) }},
		{name: "shared attribute value, tag bound", limit: "max_tag_bytes", opts: EncodeOptions{MaxTagBytes: limit},
			write: func(w *Encoder) error { return w.Element(wide) }},
		{name: "incremental start tag", limit: "max_tag_bytes", opts: EncodeOptions{MaxTagBytes: limit},
			write: func(w *Encoder) error { return w.StartElement("a", wide.Attrs, "") }},
		{name: "escaped text", limit: "max_node_bytes", opts: EncodeOptions{MaxNodeBytes: limit},
			write: func(w *Encoder) error { return w.Element(escaped) }},
		{name: "comment in a tree", limit: "max_node_bytes", opts: EncodeOptions{MaxNodeBytes: limit, MaxTagBytes: 1 << 30},
			write: func(w *Encoder) error {
				w.BeginTree()
				if err := w.StartElement("a", nil, ""); err != nil {
					return err
				}
				return w.Comment(comment)
			}},
		{name: "comment outside a tree", limit: "max_tag_bytes", opts: EncodeOptions{MaxTagBytes: limit},
			write: func(w *Encoder) error { return w.Comment(comment) }},
		{name: "generated declarations", limit: "max_node_bytes", opts: EncodeOptions{MaxNodeBytes: limit, MaxTagBytes: 1 << 30, MaxAttrs: 1000},
			write: func(w *Encoder) error { return w.Element(generated) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, err := NewEncoder(tc.opts)
			require.NoError(t, err)
			assert.Equal(t, tc.limit, limitName(t, tc.write(w)))
			n, c := peak(w)
			assert.LessOrEqual(t, n, limit+slack, "bytes held")
			assert.LessOrEqual(t, c, 2*(limit+slack), "buffer capacity")
		})
	}
}

func TestEncoderTagBytesBoundary(t *testing.T) {
	tag := &Element{Name: "a", Attrs: map[string]string{"k": "0123456789"}}
	out, err := Encode(tag, EncodeOptions{})
	require.NoError(t, err)
	size := strings.Index(string(out), ">") + 1
	_, err = Encode(tag, EncodeOptions{MaxTagBytes: size})
	require.NoError(t, err, "a start tag of exactly max_tag_bytes fits")
	_, err = Encode(tag, EncodeOptions{MaxTagBytes: size - 1})
	assert.Equal(t, "max_tag_bytes", limitName(t, err))

	d, err := NewDecoder(context.Background(), strings.NewReader(`<a k="`+strings.Repeat("x", 100)+`"/>`), Options{})
	require.NoError(t, err)
	_, err = d.NextEvent()
	require.NoError(t, err)
	w, err := NewEncoder(EncodeOptions{MaxTagBytes: 64})
	require.NoError(t, err)
	assert.Equal(t, "max_tag_bytes", limitName(t, d.Copy(w)), "copies are bounded by the writer's tag limit")
}

// encodeSharedURI writes one element declaring n prefixes for one URI and n attributes in it.
func encodeSharedURI(t *testing.T, n int) time.Duration {
	t.Helper()
	e := &Element{Name: "a", XMLNS: map[string]string{}, Attrs: map[string]string{}}
	for i := range n {
		e.XMLNS[fmt.Sprintf("p%05d", i)] = "urn:same"
		e.Attrs[fmt.Sprintf("{urn:same}k%05d", i)] = "v"
	}
	best := time.Duration(1 << 62)
	for range 3 {
		started := time.Now()
		out, err := Encode(e, EncodeOptions{MaxAttrs: 2 * n, MaxNamespaces: 2 * n})
		require.NoError(t, err)
		require.Contains(t, string(out), ` p00000:k00000="v"`, "the smallest prefix is chosen")
		best = min(best, time.Since(started))
	}
	return best
}

func TestEncoderSharedURIPrefixesScaleLinearly(t *testing.T) {
	small := encodeSharedURI(t, 1024)
	large := encodeSharedURI(t, 8192)
	t.Logf("n=1024: %v, n=8192: %v", small, large)
	assert.Less(t, large, small*30, "eight times the prefixes take far less than sixty-four times the work")
}
