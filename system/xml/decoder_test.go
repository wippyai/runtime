// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"context"
	"errors"
	"io"
	"math/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
)

type shortReader struct {
	r      io.Reader
	rng    *rand.Rand
	random bool
}

func (r *shortReader) Read(p []byte) (int, error) {
	n := 1
	if r.random {
		n = 1 + r.rng.Intn(7)
	}
	if len(p) > n {
		p = p[:n]
	}
	return r.r.Read(p)
}
func code(t *testing.T, err error, want string) {
	t.Helper()
	require.Error(t, err)
	var e apierror.Error
	require.ErrorAs(t, err, &e)
	assert.Equal(t, want, detail(e, "code"))
	if want != "argument" {
		assert.NotNil(t, detail(e, "line"))
		assert.NotNil(t, detail(e, "column"))
		assert.NotNil(t, detail(e, "offset"))
		assert.NotNil(t, detail(e, "path"))
	}
}
func TestDocuments(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"empty", "", "syntax"}, {"root", "<a/>", ""}, {"declaration", `<?xml version="1.0"?><a/>`, ""},
		{"version", `<?xml version="1.1"?><a/>`, "syntax"}, {"doctype", "<!DOCTYPE a><a/>", "syntax"},
		{"entity", "<a>&other;</a>", "syntax"}, {"entities", "<a>&lt;&gt;&amp;&apos;&quot;&#65;&#x1F600;</a>", ""},
		{"duplicate", `<a x="1" x="2"/>`, "syntax"}, {"expanded duplicate", `<a xmlns:p="u" xmlns:q="u" p:x="1" q:x="2"/>`, "namespace"},
		{"unbound", "<p:a/>", "namespace"}, {"xml binding", `<a xmlns:xml="bad"/>`, "namespace"}, {"xmlns binding", `<a xmlns:xmlns="u"/>`, "namespace"},
		{"reserved URI", `<a xmlns:p="http://www.w3.org/XML/1998/namespace"/>`, "namespace"},
		{"prefixed undeclare", `<a xmlns:p=""/>`, "namespace"}, {"default undeclare", `<a xmlns="u"><b xmlns=""/></a>`, ""},
		{"literal char", "<a>\x01</a>", "syntax"}, {"reference char", "<a>&#0;</a>", "syntax"}, {"surrogate", "<a>&#xD800;</a>", "syntax"},
		{"text close", "<a>]]></a>", "syntax"}, {"cdata", "<a><![CDATA[x]]></a>", ""},
		{"bad name", "<1a/>", "syntax"}, {"unicode name", "<é:根 xmlns:é='u'/>", ""}, {"multiple colon", "<a:b:c/>", "namespace"},
		{"mismatch", "<a></b>", "syntax"}, {"unclosed", "<a>", "syntax"}, {"two roots", "<a/><b/>", "syntax"},
		{"trailing", "<a/>x", "syntax"}, {"trailing misc", "<a/> <!--x--><?p data?>", ""},
		{"comment dash", "<a><!--a--b--></a>", "syntax"}, {"comment ending", "<a><!--a---></a>", "syntax"},
		{"pi colon", "<?p:a?><a/>", "namespace"}, {"pi xml", "<a><?XML x?></a>", "syntax"}, {"attribute less", `<a x="<"/>`, "syntax"},
		{"attribute spacing", `<a x="1"y="2"/>`, "syntax"}, {"end spacing", "<a></ a>", "syntax"},
		{"utf8", "<a>\xff</a>", "encoding"}, {"prolog text", "x<a/>", "syntax"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, mode := range []string{"normal", "single", "random"} {
				t.Run(mode, func(t *testing.T) {
					var r io.Reader = strings.NewReader(tc.src)
					if mode != "normal" {
						r = &shortReader{r: r, random: mode == "random", rng: rand.New(rand.NewSource(7))}
					}
					d, err := NewDecoder(context.Background(), r, Options{})
					require.NoError(t, err)
					for err == nil {
						_, err = d.Next()
					}
					if tc.want == "" {
						require.ErrorIs(t, err, io.EOF)
					} else {
						code(t, err, tc.want)
					}
				})
			}
		})
	}
}
func TestTree(t *testing.T) {
	for _, tc := range []struct {
		want *Element
		src  string
		opts Options
	}{
		{src: `<a/>`, opts: Options{}, want: &Element{Name: "a"}},
		{src: `<a>x<![CDATA[y]]>&amp;<!--c-->z</a>`, opts: Options{}, want: &Element{Name: "a", Text: "xy&z"}},
		{src: `<a>x<b/>y<c/>z</a>`, opts: Options{}, want: &Element{Name: "a", Children: []any{"x", &Element{Name: "b"}, "y", &Element{Name: "c"}, "z"}}},
		{src: "<a x='a\t\r\nb&#10;c'>a\r\nb\rc</a>", opts: Options{}, want: &Element{Name: "a", Attrs: map[string]string{"x": "a  b\nc"}, Text: "a\nb\nc"}},
		{src: "<a> \n<b/> \t</a>", opts: Options{Whitespace: "strip"}, want: &Element{Name: "a", Children: []any{&Element{Name: "b"}}}},
	} {
		t.Run(tc.src, func(t *testing.T) {
			for _, mode := range []string{"normal", "single", "random"} {
				t.Run(mode, func(t *testing.T) {
					got, err := decodeTestReader(testReader(tc.src, mode), tc.opts)
					require.NoError(t, err)
					assert.Equal(t, tc.want, got)
				})
			}
		})
	}
}
func TestLimitsAndPosition(t *testing.T) {
	for _, tc := range []struct {
		src, limit string
		opts       Options
	}{
		{"<a><b/></a>", "max_depth", Options{MaxDepth: 1}}, {`<a x="1" y="2"/>`, "max_attrs", Options{MaxAttrs: 1}},
		{"<longname/>", "max_tag_bytes", Options{MaxTagBytes: 8}}, {"<a>12345</a>", "max_node_bytes", Options{MaxNodeBytes: 10}}, {"<a/>", "max_bytes", Options{MaxBytes: 3}},
	} {
		t.Run(tc.limit, func(t *testing.T) {
			_, err := Decode([]byte(tc.src), tc.opts)
			code(t, err, "limit")
			var e apierror.Error
			require.ErrorAs(t, err, &e)
			assert.Equal(t, tc.limit, detail(e, "limit"))
		})
	}
	d, err := NewDecoder(context.Background(), strings.NewReader("<a>é\n<b/></a>"), Options{MaxTokenBytes: 2})
	require.NoError(t, err)
	var texts []string
	for {
		ev, err := d.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		if ev.Kind == TextEvent {
			assert.LessOrEqual(t, len(ev.Text), 2)
			texts = append(texts, string(ev.Text))
		}
		if ev.Kind == StartEvent && ev.Name == "b" {
			assert.Equal(t, Position{Line: 2, Column: 1, Offset: 6}, ev.Pos)
		}
	}
	assert.Equal(t, "é\n", strings.Join(texts, ""))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d, err = NewDecoder(ctx, strings.NewReader("<a/>"), Options{})
	require.NoError(t, err)
	_, err = d.Next()
	code(t, err, "canceled")
}

func detail(e apierror.Error, key string) any { v, _ := e.Details().Get(key); return v }

func TestNamespaces(t *testing.T) {
	e, err := Decode([]byte(`<r xmlns="u" xmlns:p="v" id="1" p:id="2"><p:a xmlns:p="w"><b xmlns=""/></p:a></r>`), Options{})
	require.NoError(t, err)
	assert.Equal(t, "u", e.NS)
	assert.Equal(t, map[string]string{"id": "1", "{v}id": "2"}, e.Attrs)
	a := Child(e, "a", nil)
	require.NotNil(t, a)
	assert.Equal(t, "w", a.NS)
	assert.Equal(t, map[string]string{"p": "w"}, a.XMLNS)
	assert.Empty(t, Child(a, "b", nil).NS)
}
func TestMalformedDeclarations(t *testing.T) {
	for _, s := range []string{`<?xml?><a/>`, `<?xml encoding="UTF-8" version="1.0"?><a/>`, `<?xml version="1.0"version="1.0"?><a/>`, `<?xml version="1.0" standalone="maybe"?><a/>`, "<?xml version\u00a0='1.0'?><a/>", ` <?xml version="1.0"?><a/>`} {
		t.Run(s, func(t *testing.T) { _, err := Decode([]byte(s), Options{}); code(t, err, "syntax") })
	}
}
func TestShortReadTrees(t *testing.T) {
	for _, s := range []string{`<a>  text  <b/>  more  </a>`, `<a><![CDATA[ x ]]>&#10;</a>`, "<a x='\r\n\t'>é\r\n𐐀</a>", `<r xmlns:p="u"><p:a p:x="&amp;"/></r>`} {
		for _, mode := range []string{"preserve", "strip"} {
			t.Run(mode+s, func(t *testing.T) {
				want, err := Decode([]byte(s), Options{Whitespace: mode})
				require.NoError(t, err)
				for _, random := range []bool{false, true} {
					d, err := NewDecoder(context.Background(), &shortReader{r: strings.NewReader(s), rng: rand.New(rand.NewSource(3)), random: random}, Options{Whitespace: mode})
					require.NoError(t, err)
					ev, err := d.Next()
					require.NoError(t, err)
					require.Equal(t, StartEvent, ev.Kind)
					got, err := d.Node()
					require.NoError(t, err)
					assert.Equal(t, want, got)
				}
			})
		}
	}
}
func TestExactErrorPosition(t *testing.T) {
	_, err := Decode([]byte("<a>é\n\x01</a>"), Options{})
	code(t, err, "syntax")
	var e apierror.Error
	require.ErrorAs(t, err, &e)
	assert.Equal(t, 2, e.Details().GetInt("line", 0))
	assert.Equal(t, 1, e.Details().GetInt("column", 0))
	assert.Equal(t, int64(6), detail(e, "offset"))
	assert.Equal(t, []string{"a"}, e.Details().GetSlice("path"))
}
func TestSkippedValidation(t *testing.T) {
	for _, s := range []string{`<a><b></c></a>`, `<a><!--bad--comment--></a>`, `<a><?XML x?></a>`} {
		d, err := NewDecoder(context.Background(), strings.NewReader(s), Options{})
		require.NoError(t, err)
		_, err = d.Next()
		require.NoError(t, err)
		require.Error(t, d.Skip())
	}
}
func TestMiscEvents(t *testing.T) {
	d, err := NewDecoder(context.Background(), strings.NewReader("<?p x\r\ny?><a><!--c\r\nd--></a>"), Options{Comments: true, Instructions: true})
	require.NoError(t, err)
	var kinds []Kind
	var texts []string
	for {
		ev, err := d.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		kinds = append(kinds, ev.Kind)
		if ev.Kind == PIEvent || ev.Kind == CommentEvent {
			texts = append(texts, string(ev.Text))
		}
	}
	assert.Equal(t, []Kind{PIEvent, StartEvent, CommentEvent, EndEvent}, kinds)
	assert.Equal(t, []string{"x\ny", "c\nd"}, texts)
}

func TestOutsideReferences(t *testing.T) {
	for _, s := range []string{`&#32;<a/>`, `<a/>&#32;`, `<a/> &amp;`, `<a/> <![CDATA[ ]]>`} {
		_, err := Decode([]byte(s), Options{})
		code(t, err, "syntax")
	}
}
func TestSteadyCursorAllocations(t *testing.T) {
	d, err := NewDecoder(context.Background(), strings.NewReader(`<r>`+strings.Repeat(`<a x="1">text</a>`, 40000)+`</r>`), Options{})
	require.NoError(t, err)
	for range 32 {
		_, err = d.NextEvent()
		require.NoError(t, err)
	}
	allocs := testing.AllocsPerRun(100, func() {
		for range 1000 {
			if _, err := d.NextEvent(); err != nil {
				panic(err)
			}
		}
	})
	assert.Zero(t, allocs)
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }
func TestIOFailure(t *testing.T) {
	cause := errors.New("read failed")
	d, err := NewDecoder(context.Background(), failingReader{cause}, Options{})
	require.NoError(t, err)
	_, err = d.Next()
	code(t, err, "io")
	require.ErrorIs(t, err, cause)
	_, again := d.Next()
	assert.Same(t, err, again)
}

func testReader(s, mode string) io.Reader {
	var r io.Reader = strings.NewReader(s)
	if mode != "normal" {
		r = &shortReader{r: r, random: mode == "random", rng: rand.New(rand.NewSource(7))}
	}
	return r
}
func decodeTestReader(r io.Reader, opts Options) (*Element, error) {
	d, err := NewDecoder(context.Background(), r, opts)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	var root *Element
	for {
		ev, err := d.Next()
		if errors.Is(err, io.EOF) {
			return root, nil
		}
		if err != nil {
			return nil, err
		}
		if ev.Kind == StartEvent {
			root, err = d.Node()
			if err != nil {
				return nil, err
			}
		}
	}
}

func TestEndTagWhitespace(t *testing.T) {
	e, err := Decode([]byte(`<a></a`+strings.Repeat(" ", 100000)+`>`), Options{MaxTagBytes: 4})
	require.NoError(t, err)
	assert.Equal(t, "a", e.Name)
}

func TestLargeIgnoredMisc(t *testing.T) {
	for _, s := range []string{`<a><!--` + strings.Repeat("abc𐐀", 10000) + `--></a>`, `<?p ` + strings.Repeat("abc𐐀", 10000) + `?><a/>`} {
		e, err := Decode([]byte(s), Options{MaxTagBytes: 4, MaxTokenBytes: 4})
		require.NoError(t, err)
		assert.Equal(t, "a", e.Name)
	}
}

func TestDefaultDepthBoundary(t *testing.T) {
	for _, depth := range []int{256, 257} {
		s := strings.Repeat("<a>", depth) + strings.Repeat("</a>", depth)
		_, err := Decode([]byte(s), Options{})
		if depth == 256 {
			require.NoError(t, err)
		} else {
			code(t, err, "limit")
		}
	}
}
