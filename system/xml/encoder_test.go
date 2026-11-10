// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncoder(t *testing.T) {
	for _, tc := range []struct {
		e    *Element
		name string
		want string
		o    EncodeOptions
	}{
		{name: "empty", e: &Element{Name: "a"}, o: EncodeOptions{}, want: `<a/>`},
		{name: "escaping", e: &Element{Name: "a", Attrs: map[string]string{"x": "\t\n\r\"<&"}, Text: "<&>\r"}, o: EncodeOptions{}, want: "<a x=\"&#9;&#10;&#13;&quot;&lt;&amp;\">&lt;&amp;&gt;&#13;</a>"},
		{name: "default", e: &Element{Name: "a", NS: "u", Children: []any{&Element{Name: "b"}}}, o: EncodeOptions{}, want: `<a xmlns="u"><b xmlns=""/></a>`},
		{name: "smallest prefix", e: &Element{Name: "a", NS: "u", Attrs: map[string]string{"{u}x": "1"}}, o: EncodeOptions{NS: map[string]string{"z": "u", "p": "u"}}, want: `<p:a xmlns:p="u" xmlns:z="u" p:x="1"/>`},
		{name: "generated", e: &Element{Name: "a", Attrs: map[string]string{"{v}z": "2", "{u}x": "1", "id": "0"}}, o: EncodeOptions{}, want: `<a xmlns:ns1="u" xmlns:ns2="v" id="0" ns1:x="1" ns2:z="2"/>`},
		{name: "indent", e: &Element{Name: "a", Children: []any{&Element{Name: "b"}, &Element{Name: "c"}}}, o: EncodeOptions{Indent: "  "}, want: "<a>\n  <b/>\n  <c/>\n</a>"},
		{name: "mixed", e: &Element{Name: "a", Children: []any{"x", &Element{Name: "b"}, "y"}}, o: EncodeOptions{Indent: "  "}, want: "<a>x<b/>y</a>"},
		{name: "declaration", e: &Element{Name: "a"}, o: EncodeOptions{Declaration: true}, want: `<?xml version="1.0" encoding="UTF-8"?><a/>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Encode(tc.e, tc.o)
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(out))
		})
	}
}
func TestRoundTrip(t *testing.T) {
	for _, s := range []string{`<a/>`, `<a xmlns="u" xmlns:p="v" p:x="1"><b xmlns="">x</b></a>`, `<a>x<b/>y<![CDATA[z]]></a>`, `<a xml:space="preserve"> &#13; </a>`} {
		t.Run(s, func(t *testing.T) {
			a, err := Decode([]byte(s), Options{})
			require.NoError(t, err)
			b, err := Encode(a, EncodeOptions{})
			require.NoError(t, err)
			c, err := Decode(b, Options{})
			require.NoError(t, err)
			assert.Equal(t, a, c)
		})
	}
}
func TestInvalidElement(t *testing.T) {
	for _, e := range []*Element{nil, {Name: ""}, {Name: "a:b"}, {Name: "a", Text: "x", Children: []any{&Element{Name: "b"}}}, {Name: "a", Children: []any{"a", "b"}}, {Name: "a", Children: []any{42}}, {Name: "a", Children: []any{""}}, {Name: "a", Text: "\x00"}} {
		_, err := Encode(e, EncodeOptions{})
		require.Error(t, err)
	}
}
func TestCopy(t *testing.T) {
	d, err := NewDecoder(context.Background(), strings.NewReader(`<r xmlns:p="u" xmlns:q="v"><p:a q:t="q:T">x<b/></p:a></r>`), Options{})
	require.NoError(t, err)
	_, err = d.Next()
	require.NoError(t, err)
	_, err = d.Next()
	require.NoError(t, err)
	w, err := NewEncoder(EncodeOptions{})
	require.NoError(t, err)
	require.NoError(t, d.Copy(w))
	require.NoError(t, w.Finish())
	e, err := Decode(w.Drain(), Options{})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"p": "u", "q": "v"}, e.XMLNS)
	assert.Equal(t, "u", e.NS)
	assert.Equal(t, "x", Text(e))
}
func TestWriterState(t *testing.T) {
	w, err := NewEncoder(EncodeOptions{})
	require.NoError(t, err)
	require.Error(t, w.Finish())
	require.Error(t, w.StartElement("a", nil, ""))
	w, err = NewEncoder(EncodeOptions{})
	require.NoError(t, err)
	require.NoError(t, w.StartElement("a", nil, ""))
	require.NoError(t, w.Text("<&"))
	require.NoError(t, w.Comment("ok"))
	require.NoError(t, w.EndElement())
	require.NoError(t, w.Finish())
	assert.Equal(t, "<a>&lt;&amp;<!--ok--></a>", string(w.Drain()))
}

func TestCopyDrain(t *testing.T) {
	d, err := NewDecoder(context.Background(), strings.NewReader(`<a>`+strings.Repeat(`<b>text</b>`, 10000)+`</a>`), Options{})
	require.NoError(t, err)
	_, err = d.Next()
	require.NoError(t, err)
	w, err := NewEncoder(EncodeOptions{})
	require.NoError(t, err)
	total := 0
	require.NoError(t, d.CopyTo(w, func(w *Encoder) error { p := w.Drain(); assert.Less(t, len(p), 64); total += len(p); return nil }))
	assert.Equal(t, 110007, total)
	require.NoError(t, w.Finish())
}

func TestPrefixBranches(t *testing.T) {
	for _, tc := range []struct {
		want string
		e    *Element
		o    EncodeOptions
	}{
		{want: `<a xmlns="u" xmlns:p="u" p:x="1"/>`, e: &Element{Name: "a", NS: "u", Attrs: map[string]string{"{u}x": "1"}}, o: EncodeOptions{NS: map[string]string{"": "u", "p": "u"}}},
		{want: `<a xmlns:ns1="v" xmlns:ns2="u" ns2:x="1"/>`, e: &Element{Name: "a", Attrs: map[string]string{"{u}x": "1"}}, o: EncodeOptions{NS: map[string]string{"ns1": "v"}}},
		{want: `<a xml:lang="en"/>`, e: &Element{Name: "a", Attrs: map[string]string{"{http://www.w3.org/XML/1998/namespace}lang": "en"}}},
	} {
		t.Run(tc.want, func(t *testing.T) {
			out, err := Encode(tc.e, tc.o)
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(out))
		})
	}
}
func TestWriterFailures(t *testing.T) {
	w, err := NewEncoder(EncodeOptions{MaxDepth: 1})
	require.NoError(t, err)
	require.NoError(t, w.StartElement("a", nil, ""))
	first := w.StartElement("b", nil, "")
	require.Error(t, first)
	assert.Same(t, first, w.EndElement())
	w, err = NewEncoder(EncodeOptions{})
	require.NoError(t, err)
	first = w.Comment("bad--comment")
	require.Error(t, first)
	assert.Same(t, first, w.Finish())
	w, err = NewEncoder(EncodeOptions{})
	require.NoError(t, err)
	first = w.Abort(context.Canceled)
	require.ErrorIs(t, first, context.Canceled)
	assert.Same(t, first, w.Text("x"))
}

func TestEmptyContainers(t *testing.T) {
	for _, e := range []*Element{{Name: "a", Attrs: map[string]string{}}, {Name: "a", XMLNS: map[string]string{}}, {Name: "a", Children: []any{}}, {Name: "a", XMLNS: map[string]string{"xml": XMLNamespace}}} {
		_, err := Encode(e, EncodeOptions{})
		require.Error(t, err)
	}
}
