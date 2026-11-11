// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
)

// dump renders an element in a fixed notation that is independent of the encoder:
// name{ns}[attrs](xmlns)"text" or name{ns}[attrs](xmlns)<child,child>.
func dump(e *Element) string {
	var b strings.Builder
	b.WriteString(e.Name)
	if e.NS != "" {
		b.WriteString("{" + e.NS + "}")
	}
	writeMap := func(open, shut string, m map[string]string) {
		if len(m) == 0 {
			return
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteString(open)
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(k + "=" + strconv.Quote(m[k]))
		}
		b.WriteString(shut)
	}
	writeMap("[", "]", e.Attrs)
	writeMap("(", ")", e.XMLNS)
	if e.Text != "" {
		b.WriteString(strconv.Quote(e.Text))
	}
	if len(e.Children) > 0 {
		b.WriteByte('<')
		for i, c := range e.Children {
			if i > 0 {
				b.WriteByte(',')
			}
			switch v := c.(type) {
			case string:
				b.WriteString(strconv.Quote(v))
			case *Element:
				b.WriteString(dump(v))
			}
		}
		b.WriteByte('>')
	}
	return b.String()
}

type treeCase struct {
	name string
	src  string
	want string
	opts Options
}

func treeOf(name, src string, opts Options, want string) treeCase {
	return treeCase{name: name, src: src, want: want, opts: opts}
}

type encodeCase struct {
	in   *Element
	name string
	want string
	opts EncodeOptions
}

func encodeOf(name string, in *Element, opts EncodeOptions, want string) encodeCase {
	return encodeCase{in: in, name: name, want: want, opts: opts}
}

type elementCase struct {
	in   *Element
	name string
}

func elementOf(name string, in *Element) elementCase {
	return elementCase{in: in, name: name}
}

func TestConformanceTrees(t *testing.T) {
	strip := Options{Whitespace: "strip"}
	cases := []treeCase{
		treeOf("empty forms are equal", `<a></a>`, Options{}, `a`),
		treeOf("text only", `<a>x</a>`, Options{}, `a"x"`),
		treeOf("whitespace preserved", "<a> <b/> </a>", Options{}, `a<" ",b," ">`),
		treeOf("whitespace stripped", "<a> <b/> </a>", strip, `a<b>`),
		treeOf("strip keeps leaf text", "<a> x </a>", strip, `a" x "`),
		treeOf("strip empties whitespace leaf", "<a>  </a>", strip, `a`),
		treeOf("strip keeps CDATA whitespace", "<a><![CDATA[  ]]></a>", strip, `a"  "`),
		treeOf("strip keeps referenced whitespace", "<a>&#x20;</a>", strip, `a" "`),
		treeOf("strip keeps whitespace joined to a reference", "<a> &#x20; </a>", strip, `a"   "`),
		treeOf("strip keeps xml:space preserve scope", `<a xml:space="preserve"> <b/> </a>`, strip,
			`a[{http://www.w3.org/XML/1998/namespace}space="preserve"]<" ",b," ">`),
		treeOf("strip resumes under xml:space default", `<a xml:space="preserve"><b xml:space="default"> <c/> </b></a>`, strip,
			`a[{http://www.w3.org/XML/1998/namespace}space="preserve"]<b[{http://www.w3.org/XML/1998/namespace}space="default"]<c>>`),
		treeOf("CDATA joins text", "<a>x<![CDATA[<y>]]>z</a>", Options{}, `a"x<y>z"`),
		treeOf("comment does not split text", "<a>x<!--c-->y</a>", Options{}, `a"xy"`),
		treeOf("instruction does not split text", "<a>x<?p d?>y</a>", Options{}, `a"xy"`),
		treeOf("mixed content order", "<p>a<b>B</b>c<i/>d</p>", Options{}, `p<"a",b"B","c",i,"d">`),
		treeOf("entities and references", "<a>&lt;&gt;&amp;&apos;&quot;&#65;&#x42;</a>", Options{}, `a"<>&'\"AB"`),
		treeOf("line endings", "<a>1\r\n2\r3\n4</a>", Options{}, `a"1\n2\n3\n4"`),
		treeOf("referenced carriage return survives", "<a>1&#13;2</a>", Options{}, `a"1\r2"`),
		treeOf("attribute whitespace normalised", "<a x=\"1\t2\n3\r\n4\r5\"/>", Options{}, `a[x="1 2 3 4 5"]`),
		treeOf("attribute references kept", `<a x="1&#9;2&#10;3&#13;4"/>`, Options{}, `a[x="1\t2\n3\r4"]`),
		treeOf("attribute quotes", `<a x='"' y="'"/>`, Options{}, `a[x="\"" y="'"]`),
		treeOf("default namespace", `<a xmlns="u"><b/></a>`, Options{}, `a{u}(="u")<b{u}>`),
		treeOf("attributes skip default namespace", `<a xmlns="u" x="1"/>`, Options{}, `a{u}[x="1"](="u")`),
		treeOf("qualified attribute", `<a xmlns:p="v" p:x="1" y="2"/>`, Options{}, `a[y="2" {v}x="1"](p="v")`),
		treeOf("prefixed element", `<p:a xmlns:p="v"><p:b/><c/></p:a>`, Options{}, `a{v}(p="v")<b{v},c>`),
		treeOf("undeclared default", `<a xmlns="u"><b xmlns=""><c/></b></a>`, Options{}, `a{u}(="u")<b(="")<c>>`),
		treeOf("rebinding in descendant", `<a xmlns:p="u"><b xmlns:p="v"><p:c/></b><p:d/></a>`, Options{},
			`a(p="u")<b(p="v")<c{v}>,d{u}>`),
		treeOf("xml prefix needs no declaration", `<a xml:lang="en"/>`, Options{}, `a[{http://www.w3.org/XML/1998/namespace}lang="en"]`),
		treeOf("declaration with encoding and standalone", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><a/>`, Options{}, `a`),
		treeOf("misc around root", "<!--x-->\n<?p d?>\n<a/>\n<!--y-->\n", Options{}, `a`),
		treeOf("greater-than in text", "<a>a>b</a>", Options{}, `a"a>b"`),
		treeOf("bracket run that is not a CDATA end", "<a>]]&gt;]] ></a>", Options{}, `a"]]>]] >"`),
		treeOf("supplementary plane", "<a>\U0001F600&#x1F600;</a>", Options{}, "a\"\U0001F600\U0001F600\""),
		treeOf("fifth edition name character", "<\u052c/>", Options{}, "\u052c"),
		treeOf("unicode names", "<\u00e9l\u00e9ment attribut\u00e9=\"1\"/>", Options{}, "\u00e9l\u00e9ment[attribut\u00e9=\"1\"]"),
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, err := Decode([]byte(tc.src), tc.opts)
			require.NoError(t, err)
			assert.Equal(t, tc.want, dump(e))
		})
	}
}

func TestConformanceRejects(t *testing.T) {
	cases := []struct{ name, src, code string }{
		{"less-than in attribute", `<a x="<"/>`, "syntax"},
		{"attributes need separating space", `<a x="1"y="2"/>`, "syntax"},
		{"unquoted attribute", `<a x=1/>`, "syntax"},
		{"attribute without value", `<a x/>`, "syntax"},
		{"space inside empty tag close", `<a/ >`, "syntax"},
		{"space before name", `< a/>`, "syntax"},
		{"end tag with attribute", `<a></a x="1">`, "syntax"},
		{"lone end tag", `</a>`, "syntax"},
		{"reference above range", `<a>&#x110000;</a>`, "syntax"},
		{"reference to non-character", `<a>&#xFFFE;</a>`, "syntax"},
		{"literal non-character", "<a>\uFFFE</a>", "syntax"},
		{"unterminated reference", `<a>&amp</a>`, "syntax"},
		{"empty reference", `<a>&#;</a>`, "syntax"},
		{"hex marker is lower case", `<a>&#X41;</a>`, "syntax"},
		{"bare ampersand", `<a>a & b</a>`, "syntax"},
		{"reference in attribute", `<a x="&other;"/>`, "syntax"},
		{"declaration after whitespace", ` <?xml version="1.0"?><a/>`, "syntax"},
		{"declaration inside root", `<a><?xml version="1.0"?></a>`, "syntax"},
		{"reserved instruction target", `<a><?XmL d?></a>`, "syntax"},
		{"declaration without version", `<?xml encoding="UTF-8"?><a/>`, "syntax"},
		{"declaration bad standalone", `<?xml version="1.0" standalone="maybe"?><a/>`, "syntax"},
		{"declaration wrong order", `<?xml encoding="UTF-8" version="1.0"?><a/>`, "syntax"},
		{"double hyphen in comment", `<a><!-- a -- b --></a>`, "syntax"},
		{"comment ending in hyphen", `<a><!-- a ---></a>`, "syntax"},
		{"unterminated comment", `<a><!-- a`, "syntax"},
		{"unterminated CDATA", `<a><![CDATA[x`, "syntax"},
		{"CDATA outside root", `<![CDATA[x]]><a/>`, "syntax"},
		{"malformed UTF-8", "<a>\xff</a>", "encoding"},
		{"overlong UTF-8", "<a>\xc0\xaf</a>", "encoding"},
		{"truncated UTF-8 at end", "<a>\xe2\x82", "encoding"},
		{"colon in instruction target", `<a><?p:i d?></a>`, "namespace"},
		{"prefix bound to xmlns namespace", `<a xmlns:p="http://www.w3.org/2000/xmlns/"/>`, "namespace"},
		{"default bound to xml namespace", `<a xmlns="http://www.w3.org/XML/1998/namespace"/>`, "namespace"},
		{"xmlns as element prefix", `<xmlns:a xmlns:b="u"/>`, "namespace"},
		{"unbound attribute prefix", `<a p:x="1"/>`, "namespace"},
		{"empty prefix part", `<:a/>`, "namespace"},
		{"empty local part", `<a xmlns:p="u"><p:/></a>`, "namespace"},
		{"duplicate declaration", `<a xmlns:p="u" xmlns:p="v"/>`, "syntax"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, reader := range []string{"whole", "byte"} {
				var r io.Reader = strings.NewReader(tc.src)
				if reader == "byte" {
					r = &shortReader{r: r}
				}
				d, err := NewDecoder(context.Background(), r, Options{})
				require.NoError(t, err)
				for err == nil {
					_, err = d.NextEvent()
				}
				require.False(t, errors.Is(err, io.EOF), "%s reader accepts the document", reader)
				code(t, err, tc.code)
			}
		})
	}
}

func TestConformanceErrorPosition(t *testing.T) {
	_, err := Decode([]byte("<a>\n  <b></c>\n</a>"), Options{})
	require.Error(t, err)
	var e apierror.Error
	require.ErrorAs(t, err, &e)
	assert.EqualValues(t, 2, detail(e, "line"))
	assert.Equal(t, []string{"a", "b"}, detail(e, "path"))
}

func TestConformanceStripDecidesPerRun(t *testing.T) {
	opts := Options{Whitespace: "strip", MaxTokenBytes: 8}
	run := strings.Repeat(" ", 20)

	e, err := Decode([]byte("<a>"+run+"x</a>"), opts)
	require.NoError(t, err)
	assert.Equal(t, run+"x", e.Text, "whitespace inside a text run is content")

	e, err = Decode([]byte("<a>x"+run+"</a>"), opts)
	require.NoError(t, err)
	assert.Equal(t, "x"+run, e.Text)

	e, err = Decode([]byte("<a>x"+run+"y</a>"), opts)
	require.NoError(t, err)
	assert.Equal(t, "x"+run+"y", e.Text)
}

func TestConformanceTextChunks(t *testing.T) {
	text := strings.Repeat("a\u00e9\u20ac\U0001F600", 9)
	for _, limit := range []int{4, 5, 6, 7, 16} {
		t.Run(strconv.Itoa(limit), func(t *testing.T) {
			d, err := NewDecoder(context.Background(), &shortReader{r: strings.NewReader("<a>" + text + "</a>")}, Options{MaxTokenBytes: limit})
			require.NoError(t, err)
			var got bytes.Buffer
			chunks := 0
			for {
				ev, err := d.NextEvent()
				if errors.Is(err, io.EOF) {
					break
				}
				require.NoError(t, err)
				if ev.Kind != TextEvent {
					continue
				}
				chunks++
				assert.LessOrEqual(t, len(ev.Text), limit)
				assert.True(t, utf8.Valid(ev.Text), "chunk splits a character")
				got.Write(ev.Text)
			}
			assert.Equal(t, text, got.String())
			assert.Greater(t, chunks, 1)
		})
	}
}

func TestConformanceDetachedRecordScope(t *testing.T) {
	src := `<s:catalog xmlns:s="urn:shop" xmlns:xsi="urn:xsi" xmlns="urn:default">` +
		`<s:item xsi:type="s:Book" xmlns:x="urn:own"><title>T</title></s:item></s:catalog>`
	path, err := CompilePath("s:catalog/s:item", map[string]string{"s": "urn:shop"})
	require.NoError(t, err)
	d, err := NewDecoder(context.Background(), strings.NewReader(src), Options{})
	require.NoError(t, err)
	sel, err := NewSelector(d, []Path{path})
	require.NoError(t, err)
	var b Batch
	n, err := sel.Read(&b, 10)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	records, err := b.Elements()
	require.NoError(t, err)
	item := records[0]
	assert.Equal(t, map[string]string{"s": "urn:shop", "xsi": "urn:xsi", "": "urn:default", "x": "urn:own"}, item.XMLNS)
	title := item.Children[0].(*Element)
	assert.Equal(t, "urn:default", title.NS)
	assert.Nil(t, title.XMLNS, "descendants carry only their own declarations")
	name, ns, err := Resolve(item, item.Attrs["{urn:xsi}type"])
	require.NoError(t, err)
	assert.Equal(t, "Book", name)
	assert.Equal(t, "urn:shop", ns)
}

func TestConformancePaths(t *testing.T) {
	src := `<catalog xmlns="urn:shop"><item id="1"><item id="nested"/></item><other/><item id="2"/></catalog>`
	ids := func(paths []string, bindings map[string]string) []string {
		compiled := make([]Path, 0, len(paths))
		for _, p := range paths {
			c, err := CompilePath(p, bindings)
			require.NoError(t, err)
			compiled = append(compiled, c)
		}
		d, err := NewDecoder(context.Background(), strings.NewReader(src), Options{})
		require.NoError(t, err)
		sel, err := NewSelector(d, compiled)
		require.NoError(t, err)
		var out []string
		for {
			var b Batch
			_, err := sel.Read(&b, 1)
			if errors.Is(err, io.EOF) {
				return out
			}
			require.NoError(t, err)
			records, err := b.Elements()
			require.NoError(t, err)
			for _, r := range records {
				out = append(out, r.Name+":"+r.Attrs["id"])
			}
		}
	}
	assert.Empty(t, ids([]string{"catalog/item"}, nil), "bare names match no namespace")
	assert.Equal(t, []string{"item:1", "item:2"}, ids([]string{"*:catalog/*:item"}, nil), "matches do not nest")
	assert.Equal(t, []string{"item:1", "item:2"}, ids([]string{"s:catalog/s:item"}, map[string]string{"s": "urn:shop"}))
	assert.Equal(t, []string{"item:1", "other:", "item:2"}, ids([]string{"*/*:item", "*/*:other"}, nil), "document order across paths")
	assert.Equal(t, []string{"item:1", "other:", "item:2"}, ids([]string{"/*/*"}, nil))
	_, err := CompilePath("q:catalog", nil)
	require.Error(t, err)
	var e apierror.Error
	require.ErrorAs(t, err, &e)
	assert.Equal(t, "namespace", detail(e, "code"))
}

func TestConformanceEncode(t *testing.T) {
	cases := []encodeCase{
		encodeOf("empty", &Element{Name: "a"}, EncodeOptions{}, `<a/>`),
		encodeOf("text escaping", &Element{Name: "a", Text: "a<b>&\"c'\r\n"}, EncodeOptions{}, "<a>a&lt;b&gt;&amp;\"c'&#13;\n</a>"),
		encodeOf("attribute escaping", &Element{Name: "a", Attrs: map[string]string{"x": "\"<&>\t\n\r'"}}, EncodeOptions{},
			`<a x="&quot;&lt;&amp;&gt;&#9;&#10;&#13;'"/>`),
		encodeOf("attribute order", &Element{Name: "a", Attrs: map[string]string{"b": "2", "a": "1", "{v}z": "3", "{u}y": "4"}}, EncodeOptions{},
			`<a xmlns:ns1="u" xmlns:ns2="v" a="1" b="2" ns1:y="4" ns2:z="3"/>`),
		encodeOf("element namespace becomes default", &Element{Name: "a", NS: "u", Children: []any{&Element{Name: "b", NS: "u"}}}, EncodeOptions{},
			`<a xmlns="u"><b/></a>`),
		encodeOf("no namespace under default", &Element{Name: "a", NS: "u", Children: []any{&Element{Name: "b"}}}, EncodeOptions{},
			`<a xmlns="u"><b xmlns=""/></a>`),
		encodeOf("declared prefix is used", &Element{Name: "a", NS: "u", XMLNS: map[string]string{"p": "u"}}, EncodeOptions{},
			`<p:a xmlns:p="u"/>`),
		encodeOf("default wins over prefix", &Element{Name: "a", NS: "u", XMLNS: map[string]string{"p": "u", "": "u"}}, EncodeOptions{},
			`<a xmlns="u" xmlns:p="u"/>`),
		encodeOf("smallest prefix wins", &Element{Name: "a", NS: "u", XMLNS: map[string]string{"q": "u", "p": "u"}}, EncodeOptions{},
			`<p:a xmlns:p="u" xmlns:q="u"/>`),
		encodeOf("preferred bindings on root", &Element{Name: "a", NS: "u", Children: []any{&Element{Name: "b", NS: "v"}}},
			EncodeOptions{NS: map[string]string{"s": "u", "t": "v"}}, `<s:a xmlns:s="u" xmlns:t="v"><t:b/></s:a>`),
		encodeOf("generated prefix avoids taken names", &Element{Name: "a", XMLNS: map[string]string{"ns1": "w"}, Attrs: map[string]string{"{u}x": "1"}},
			EncodeOptions{}, `<a xmlns:ns1="w" xmlns:ns2="u" ns2:x="1"/>`),
		encodeOf("xml namespace attribute", &Element{Name: "a", Attrs: map[string]string{"{" + XMLNamespace + "}lang": "en"}}, EncodeOptions{},
			`<a xml:lang="en"/>`),
		encodeOf("declaration", &Element{Name: "a"}, EncodeOptions{Declaration: true}, `<?xml version="1.0" encoding="UTF-8"?><a/>`),
		encodeOf("declaration on its own line when indenting", &Element{Name: "a"}, EncodeOptions{Declaration: true, Indent: " "},
			`<?xml version="1.0" encoding="UTF-8"?>`+"\n<a/>"),
		encodeOf("indent element-only content", &Element{Name: "a", Children: []any{&Element{Name: "b", Text: "x"}, &Element{Name: "c", Children: []any{&Element{Name: "d"}}}}},
			EncodeOptions{Indent: "  "}, "<a>\n  <b>x</b>\n  <c>\n    <d/>\n  </c>\n</a>"),
		encodeOf("indent leaves mixed content alone", &Element{Name: "a", Children: []any{&Element{Name: "p", Children: []any{"x", &Element{Name: "b"}}}}},
			EncodeOptions{Indent: "  "}, "<a>\n  <p>x<b/></p>\n</a>"),
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Encode(tc.in, tc.opts)
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(out))
			back, err := Decode(out, Options{})
			require.NoError(t, err, "encoder output is well formed")
			again, err := Encode(back, tc.opts)
			require.NoError(t, err)
			if tc.opts.Indent == "" {
				assert.Equal(t, string(out), string(again), "encoding is stable across a round trip")
			}
		})
	}
}

func TestConformanceEncodeRejects(t *testing.T) {
	cases := []elementCase{
		elementOf("text and children", &Element{Name: "a", Text: "x", Children: []any{&Element{Name: "b"}}}),
		elementOf("children without element", &Element{Name: "a", Children: []any{"x"}}),
		elementOf("empty string child", &Element{Name: "a", Children: []any{"", &Element{Name: "b"}}}),
		elementOf("adjacent strings", &Element{Name: "a", Children: []any{"x", "y", &Element{Name: "b"}}}),
		elementOf("nil child", &Element{Name: "a", Children: []any{(*Element)(nil)}}),
		elementOf("foreign child type", &Element{Name: "a", Children: []any{1}}),
		elementOf("empty name", &Element{}),
		elementOf("colon in name", &Element{Name: "p:a"}),
		elementOf("invalid name", &Element{Name: "1a"}),
		elementOf("invalid character in text", &Element{Name: "a", Text: "\x00"}),
		elementOf("invalid character in attribute", &Element{Name: "a", Attrs: map[string]string{"x": "\x01"}}),
		elementOf("malformed UTF-8", &Element{Name: "a", Text: "\xff"}),
		elementOf("invalid attribute key", &Element{Name: "a", Attrs: map[string]string{"{u": "1"}}),
		elementOf("attribute named xmlns", &Element{Name: "a", Attrs: map[string]string{"xmlns": "u"}}),
		elementOf("empty namespace in attribute key", &Element{Name: "a", Attrs: map[string]string{"{}x": "1"}}),
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Encode(tc.in, EncodeOptions{})
			require.Error(t, err)
			var e apierror.Error
			require.ErrorAs(t, err, &e)
			assert.Equal(t, "element", detail(e, "code"))
		})
	}
}

func TestConformanceEncodeRejectsDeclarations(t *testing.T) {
	cases := []elementCase{
		elementOf("reserved prefix declaration", &Element{Name: "a", XMLNS: map[string]string{"xml": "u"}}),
		elementOf("prefix undeclaration", &Element{Name: "a", XMLNS: map[string]string{"p": ""}}),
		elementOf("invalid prefix", &Element{Name: "a", XMLNS: map[string]string{"1p": "u"}}),
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Encode(tc.in, EncodeOptions{})
			require.Error(t, err)
			var e apierror.Error
			require.ErrorAs(t, err, &e)
			assert.Equal(t, "namespace", detail(e, "code"))
		})
	}
}

func TestConformanceReferenceErrorPosition(t *testing.T) {
	_, err := Decode([]byte("<a>\n<b x=\"12345678901234567890&#xD800;\"/></a>"), Options{})
	require.Error(t, err)
	var e apierror.Error
	require.ErrorAs(t, err, &e)
	assert.EqualValues(t, 2, detail(e, "line"))
	assert.EqualValues(t, 27, detail(e, "column"), "position points at the reference")
}

func TestConformanceEncoderIncrementalIndent(t *testing.T) {
	w, err := NewEncoder(EncodeOptions{Indent: "  "})
	require.NoError(t, err)
	require.NoError(t, w.StartElement("a", nil, ""))
	require.NoError(t, w.StartElement("b", nil, ""))
	require.NoError(t, w.Text("x"))
	require.NoError(t, w.EndElement())
	require.NoError(t, w.StartElement("c", nil, ""))
	require.NoError(t, w.StartElement("d", nil, ""))
	require.NoError(t, w.EndElement())
	require.NoError(t, w.EndElement())
	require.NoError(t, w.EndElement())
	require.NoError(t, w.Finish())
	assert.Equal(t, "<a>\n  <b>x</b>\n  <c>\n    <d/>\n  </c>\n</a>", string(w.Drain()))
}

func TestConformanceEncoderStopsIndentingAfterText(t *testing.T) {
	w, err := NewEncoder(EncodeOptions{Indent: "  "})
	require.NoError(t, err)
	require.NoError(t, w.StartElement("p", nil, ""))
	require.NoError(t, w.Text("x"))
	require.NoError(t, w.StartElement("b", nil, ""))
	require.NoError(t, w.EndElement())
	require.NoError(t, w.EndElement())
	require.NoError(t, w.Finish())
	assert.Equal(t, "<p>x<b/></p>", string(w.Drain()))
}
