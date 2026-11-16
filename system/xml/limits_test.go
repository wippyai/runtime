// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
)

// textStream produces "<a>" followed by n bytes of text and "</a>" without holding the document.
type textStream struct {
	head, tail string
	n          int
	pos        int
}

func (s *textStream) Read(p []byte) (int, error) {
	total := len(s.head) + s.n + len(s.tail)
	if s.pos >= total {
		return 0, io.EOF
	}
	k := 0
	for k < len(p) && s.pos < total {
		switch {
		case s.pos < len(s.head):
			p[k] = s.head[s.pos]
		case s.pos < len(s.head)+s.n:
			p[k] = 'x'
		default:
			p[k] = s.tail[s.pos-len(s.head)-s.n]
		}
		k++
		s.pos++
	}
	return k, nil
}

func TestAppendEventsByteBudget(t *testing.T) {
	const budget = 64 << 10
	src := &textStream{head: "<a>", tail: "</a>", n: 24 << 20}
	d, err := NewDecoder(context.Background(), src, Options{MaxTokenBytes: 256 << 10})
	require.NoError(t, err)
	var b Batch
	events := 0
	text := 0
	for {
		b.Reset()
		n, err := d.AppendEvents(&b, 1<<30, budget)
		if n > 0 {
			assert.LessOrEqual(t, b.Size(), budget+(256<<10)+64, "a batch holds the budget plus at most one event")
		}
		events += n
		_ = b.Replay(func(ev Event) error {
			if ev.Kind == TextEvent {
				text += len(ev.Text)
			}
			return nil
		})
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		require.Positive(t, n, "a fill delivers at least one event")
	}
	assert.Equal(t, 24<<20, text)
	assert.Greater(t, events, 96)

	d, err = NewDecoder(context.Background(), strings.NewReader("<a>"+strings.Repeat("<b/>", 100)+"</a>"), Options{})
	require.NoError(t, err)
	b.Reset()
	n, err := d.AppendEvents(&b, 1000, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "a budget smaller than one event still delivers one event")
	_, err = d.AppendEvents(&b, 1000, 0)
	code(t, err, "argument")
}

// codeIs checks the error code of errors that carry no document position.
func codeIs(t *testing.T, err error, want string) {
	t.Helper()
	require.Error(t, err)
	var e apierror.Error
	require.ErrorAs(t, err, &e)
	assert.Equal(t, want, detail(e, "code"))
}

func nestedDeclarations(depth, perElement int) string {
	var s strings.Builder
	for i := range depth {
		s.WriteString("<e")
		for j := range perElement {
			fmt.Fprintf(&s, ` xmlns:p%d="urn:%d:%d"`, j, i, j)
		}
		s.WriteString(">")
	}
	for range depth {
		s.WriteString("</e>")
	}
	return s.String()
}

func TestMaxNamespaces(t *testing.T) {
	_, err := Decode([]byte(nestedDeclarations(4, 3)), Options{MaxNamespaces: 12})
	require.NoError(t, err, "shadowed declarations count and twelve fit exactly")
	_, err = Decode([]byte(nestedDeclarations(4, 3)+""), Options{MaxNamespaces: 11})
	code(t, err, "limit")
	var e apierror.Error
	require.ErrorAs(t, err, &e)
	assert.Equal(t, "max_namespaces", detail(e, "limit"))

	_, err = Decode([]byte(nestedDeclarations(2, 600)), Options{MaxAttrs: 1000})
	code(t, err, "limit")
	require.ErrorAs(t, err, &e)
	assert.Equal(t, "max_namespaces", detail(e, "limit"), "the default allows 1024 bindings")

	_, err = Decode([]byte(nestedDeclarations(2, 512)), Options{MaxAttrs: 1000})
	require.NoError(t, err)

	src := `<a xmlns:p="1"><b/><b/><b/></a>`
	_, err = Decode([]byte(src), Options{MaxNamespaces: 1})
	require.NoError(t, err, "declarations leave scope with their element")
	_, err = NewDecoder(context.Background(), strings.NewReader(src), Options{MaxNamespaces: -1})
	code(t, err, "argument")
}

func TestScopeShadowing(t *testing.T) {
	src := `<a xmlns:p="1" xmlns:q="2"><b xmlns:p="3" xmlns=""><c xmlns="4"/></b><d xmlns:q="5"/></a>`
	d, err := NewDecoder(context.Background(), strings.NewReader(src), Options{})
	require.NoError(t, err)
	got := map[string][]Binding{}
	for {
		ev, err := d.NextEvent()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		if ev.Kind == StartEvent {
			got[ev.Name] = d.Scope()
		}
	}
	assert.Equal(t, []Binding{{"p", "1"}, {"q", "2"}}, got["a"])
	assert.Equal(t, []Binding{{"q", "2"}, {"p", "3"}, {"", ""}}, got["b"])
	assert.Equal(t, []Binding{{"q", "2"}, {"p", "3"}, {"", "4"}}, got["c"])
	assert.Equal(t, []Binding{{"p", "1"}, {"q", "5"}}, got["d"])
}

func TestSelectorScopeCountsTowardBudget(t *testing.T) {
	var s strings.Builder
	s.WriteString("<root")
	for i := range 100 {
		fmt.Fprintf(&s, ` xmlns:p%d="urn:%s:%d"`, i, strings.Repeat("u", 90), i)
	}
	s.WriteString(">")
	for range 1000 {
		s.WriteString("<r/>")
	}
	s.WriteString("</root>")
	const nodeBytes = 64 << 10
	d, err := NewDecoder(context.Background(), strings.NewReader(s.String()), Options{MaxNodeBytes: nodeBytes})
	require.NoError(t, err)
	p, err := CompilePath("root/r", nil)
	require.NoError(t, err)
	sel, err := NewSelector(d, []Path{p})
	require.NoError(t, err)
	var b Batch
	total := 0
	for {
		b.Reset()
		n, err := sel.Read(&b, 1000)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		scope := 0
		trees, err := b.Elements()
		require.NoError(t, err)
		for _, tr := range trees {
			for k, v := range tr.XMLNS {
				scope += len(k) + len(v)
			}
		}
		assert.Less(t, scope, nodeBytes+10<<10, "carried scope stays within the batch budget plus one record")
		assert.Less(t, n, 1000)
		total += n
	}
	assert.Equal(t, 1000, total)
}

type cancelReader struct {
	cancel context.CancelFunc
	r      io.Reader
	after  int
}

func (r *cancelReader) Read(p []byte) (int, error) {
	if r.after <= 0 {
		r.cancel()
	}
	r.after--
	if len(p) > 4 {
		p = p[:4]
	}
	return r.r.Read(p)
}

func TestDecoderSetContext(t *testing.T) {
	d, err := NewDecoder(context.Background(), strings.NewReader("<a><b/></a>"), Options{})
	require.NoError(t, err)
	_, err = d.NextEvent()
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, d.SetContext(ctx))
	_, err = d.NextEvent()
	require.Error(t, err)
	var e apierror.Error
	require.ErrorAs(t, err, &e)
	assert.Equal(t, apierror.Canceled, e.Kind())
	assert.Equal(t, "canceled", detail(e, "code"))
	code(t, d.SetContext(nil), "argument")

	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	d, err = NewDecoder(context.Background(), &cancelReader{cancel: cancel, r: strings.NewReader("<a>" + strings.Repeat("<b/>", 100) + "</a>"), after: 3}, Options{})
	require.NoError(t, err)
	require.NoError(t, d.SetContext(ctx))
	for err == nil {
		_, err = d.NextEvent()
	}
	require.ErrorAs(t, err, &e)
	assert.Equal(t, apierror.Canceled, e.Kind(), "the bound context is checked before each read")
}

func TestAppendDocument(t *testing.T) {
	d, err := NewDecoder(context.Background(), strings.NewReader("<!--x--><a><b>t</b></a><?p?>\n"), Options{Comments: true, Instructions: true})
	require.NoError(t, err)
	var b Batch
	require.NoError(t, d.AppendDocument(&b))
	trees, err := b.Elements()
	require.NoError(t, err)
	assert.Equal(t, `a<b"t">`, dump(trees[0]))
	d, err = NewDecoder(context.Background(), strings.NewReader("<a/><b/>"), Options{})
	require.NoError(t, err)
	b.Reset()
	code(t, d.AppendDocument(&b), "syntax")
}

func TestPathMatch(t *testing.T) {
	p, err := CompilePath("/s:a/*:b/*/s:*", map[string]string{"s": "u"})
	require.NoError(t, err)
	assert.Equal(t, 4, p.Len())
	assert.True(t, p.Match(0, "a", "u"))
	assert.False(t, p.Match(0, "a", ""))
	assert.True(t, p.Match(1, "b", "anything"))
	assert.False(t, p.Match(1, "c", ""))
	assert.True(t, p.Match(2, "z", ""))
	assert.True(t, p.Match(3, "q", "u"))
	assert.False(t, p.Match(3, "q", "v"))
	assert.False(t, p.Match(4, "a", "u"), "steps past the end match nothing")
	assert.False(t, p.Match(-1, "a", "u"))
}

func TestResolveQName(t *testing.T) {
	scope := map[string]string{"s": "urn:s", "": "urn:d"}
	lookup := func(p string) (string, bool) { v, ok := scope[p]; return v, ok }
	for _, tc := range []struct{ in, local, ns, code string }{
		{"s:Order", "Order", "urn:s", ""},
		{"Order", "Order", "urn:d", ""},
		{"xml:lang", "lang", XMLNamespace, ""},
		{"q:Order", "", "", "namespace"},
		{"1x", "", "", "syntax"},
		{"a:b:c", "", "", "namespace"},
		{"a\xff", "", "", "encoding"},
	} {
		local, ns, err := ResolveQName(tc.in, lookup)
		if tc.code != "" {
			require.Error(t, err, tc.in)
			var e apierror.Error
			require.ErrorAs(t, err, &e)
			assert.Equal(t, tc.code, detail(e, "code"), tc.in)
			assert.NotContains(t, e.Error(), "bufferLookahead")
			continue
		}
		require.NoError(t, err, tc.in)
		assert.Equal(t, tc.local, local)
		assert.Equal(t, tc.ns, ns)
	}
	local, ns, err := ResolveQName("Order", func(string) (string, bool) { return "", false })
	require.NoError(t, err)
	assert.Equal(t, "Order", local)
	assert.Empty(t, ns, "an unprefixed name without a default binding has no namespace")
}

func TestEncoderStartNode(t *testing.T) {
	w, err := NewEncoder(EncodeOptions{Indent: "  "})
	require.NoError(t, err)
	require.NoError(t, w.StartNode("a", "u", []AttrValue{{Key: "z", Value: "2"}, {Key: "{v}k", Value: "1"}, {Key: "b", Value: "<"}}, []Binding{{"q", "v"}, {"p", "v"}}, false))
	require.NoError(t, w.StartNode("m", "", nil, nil, true))
	require.NoError(t, w.Text("x"))
	require.NoError(t, w.StartNode("i", "", nil, nil, false))
	require.NoError(t, w.EndElement())
	require.NoError(t, w.EndElement())
	require.NoError(t, w.StartNode("e", "u", nil, nil, false))
	require.NoError(t, w.EndElement())
	require.NoError(t, w.EndElement())
	require.NoError(t, w.Finish())
	assert.Equal(t, "<a xmlns=\"u\" xmlns:p=\"v\" xmlns:q=\"v\" b=\"&lt;\" z=\"2\" p:k=\"1\">\n  <m xmlns=\"\">x<i/></m>\n  <e/>\n</a>", string(w.Drain()))

	w, err = NewEncoder(EncodeOptions{})
	require.NoError(t, err)
	err = w.StartNode("a", "", []AttrValue{{Key: "{u", Value: "1"}}, nil, false)
	codeIs(t, err, "element")
	assert.Same(t, err, w.EndElement())
	w, err = NewEncoder(EncodeOptions{})
	require.NoError(t, err)
	codeIs(t, w.StartNode("a", "", nil, []Binding{{"p", ""}}, false), "namespace")
}

type chunkWriter struct {
	fail  error
	buf   bytes.Buffer
	limit int
	stall bool
}

func (w *chunkWriter) Write(p []byte) (int, error) {
	if w.fail != nil {
		return 0, w.fail
	}
	if w.stall {
		return 0, nil
	}
	if len(p) > w.limit {
		p = p[:w.limit]
	}
	return w.buf.Write(p)
}

func TestEncoderFlush(t *testing.T) {
	w, err := NewEncoder(EncodeOptions{})
	require.NoError(t, err)
	require.NoError(t, w.StartElement("a", map[string]string{"x": "1"}, ""))
	require.NoError(t, w.Text(strings.Repeat("t", 1000)))
	dst := &chunkWriter{limit: 3}
	require.NoError(t, w.Flush(dst))
	assert.Empty(t, w.Buffered())
	require.NoError(t, w.EndElement())
	require.NoError(t, w.Finish())
	require.NoError(t, w.Flush(dst))
	assert.Equal(t, `<a x="1">`+strings.Repeat("t", 1000)+`</a>`, dst.buf.String())

	for name, dst := range map[string]*chunkWriter{
		"failure": {limit: 1, fail: errors.New("disk full")},
		"stall":   {limit: 1, stall: true},
	} {
		t.Run(name, func(t *testing.T) {
			w, err := NewEncoder(EncodeOptions{})
			require.NoError(t, err)
			require.NoError(t, w.StartElement("a", nil, ""))
			first := w.Flush(dst)
			require.Error(t, first)
			var e apierror.Error
			require.ErrorAs(t, first, &e)
			assert.Equal(t, apierror.Internal, e.Kind())
			assert.Equal(t, "io", detail(e, "code"))
			assert.Same(t, first, w.EndElement(), "a failed write is terminal")
			assert.Same(t, first, w.Flush(&chunkWriter{limit: 10}))
		})
	}
}

func TestEncoderFlushCanceled(t *testing.T) {
	w, err := NewEncoder(EncodeOptions{})
	require.NoError(t, err)
	require.NoError(t, w.StartElement("a", nil, ""))
	err = w.Flush(&chunkWriter{limit: 1, fail: context.Canceled})
	var e apierror.Error
	require.ErrorAs(t, err, &e)
	assert.Equal(t, apierror.Canceled, e.Kind())
}

type classifiedFailure struct{ err error }

func (r classifiedFailure) Read([]byte) (int, error) { return 0, r.err }

func TestClassifiedSourceErrorIsIO(t *testing.T) {
	cause := apierror.New(apierror.Internal, "read failed").WithRetryable(apierror.False)
	d, err := NewDecoder(context.Background(), io.MultiReader(strings.NewReader("<a>"), classifiedFailure{err: cause}), Options{})
	require.NoError(t, err)
	for err == nil {
		_, err = d.NextEvent()
	}
	var e apierror.Error
	require.ErrorAs(t, err, &e)
	assert.Equal(t, apierror.Internal, e.Kind())
	assert.Equal(t, "io", detail(e, "code"), "a source failure is an I/O error whatever its own classification")
	assert.ErrorIs(t, err, cause)
}

func TestEncoderFail(t *testing.T) {
	w, err := NewEncoder(EncodeOptions{})
	require.NoError(t, err)
	require.NoError(t, w.StartElement("a", nil, ""))
	first := w.Fail("element", "invalid child")
	codeIs(t, first, "element")
	assert.Same(t, first, w.EndElement(), "a recorded failure is terminal")
	assert.Same(t, first, w.Fail("state", "later failure"))
}

func TestTextEventsIndependentOfReads(t *testing.T) {
	docs := []string{
		cursorDocument,
		"<a>" + strings.Repeat("plain text ", 20000) + "</a>",
		"<a>" + strings.Repeat("café &amp; more\r\n", 3000) + "<b/>" + strings.Repeat("y", 70000) + "</a>",
	}
	for i, src := range docs {
		for _, limit := range []int{0, 5, 1 << 17} {
			opts := Options{Comments: true, Instructions: true, MaxTokenBytes: limit}
			want := decoderEvents(t, strings.NewReader(src), opts)
			got := decoderEvents(t, &shortReader{r: strings.NewReader(src)}, opts)
			assert.Equal(t, want, got, "document %d, max_token_bytes %d: byte reads", i, limit)
			got = decoderEvents(t, &shortReader{r: strings.NewReader(src), random: true, rng: rand.New(rand.NewSource(int64(i)))}, opts)
			assert.Equal(t, want, got, "document %d, max_token_bytes %d: random reads", i, limit)
		}
	}
}

func TestVisitDocumentMatchesAppendDocument(t *testing.T) {
	src := `<!--x--><s:a xmlns:s="urn:s" k="v"><s:b>t<![CDATA[u]]></s:b><c xmlns="urn:c"/></s:a><?p?>`
	d, err := NewDecoder(context.Background(), strings.NewReader(src), Options{Comments: true})
	require.NoError(t, err)
	var b Batch
	require.NoError(t, d.AppendDocument(&b))
	var want []string
	require.NoError(t, b.Replay(func(ev Event) error { want = append(want, signature(&ev, nil)); return nil }))

	d, err = NewDecoder(context.Background(), strings.NewReader(src), Options{Comments: true})
	require.NoError(t, err)
	var got []string
	require.NoError(t, d.VisitDocument(func(ev *Event) error { got = append(got, signature(ev, nil)); return nil }))
	assert.Equal(t, want, got)
	assert.Contains(t, got[0], "xmlns:s=urn:s", "the root carries its full scope")

	d, err = NewDecoder(context.Background(), strings.NewReader(`<a>`+strings.Repeat("x", 100)+`</a>`), Options{MaxNodeBytes: 50})
	require.NoError(t, err)
	code(t, d.VisitDocument(func(*Event) error { return nil }), "limit")

	stop := errors.New("stop")
	d, err = NewDecoder(context.Background(), strings.NewReader(`<a><b/></a>`), Options{})
	require.NoError(t, err)
	require.ErrorIs(t, d.VisitDocument(func(*Event) error { return stop }), stop)
}

func TestEntityDenseTextWithTinyLimits(t *testing.T) {
	src := `<a>&amp;&amp;&amp;&amp;</a>`
	for name, reader := range cursorReaders(src) {
		t.Run(name, func(t *testing.T) {
			d, err := NewDecoder(context.Background(), reader(), Options{MaxTagBytes: 4, MaxTokenBytes: 4})
			require.NoError(t, err)
			var text strings.Builder
			for {
				ev, err := d.NextEvent()
				if errors.Is(err, io.EOF) {
					break
				}
				require.NoError(t, err)
				if ev.Kind == TextEvent {
					assert.LessOrEqual(t, len(ev.Text), 4)
					text.Write(ev.Text)
				}
			}
			assert.Equal(t, "&&&&", text.String())
		})
	}
	e, err := Decode([]byte(src), Options{MaxTagBytes: 4, MaxTokenBytes: 4, MaxNodeBytes: 64})
	require.NoError(t, err)
	assert.Equal(t, "&&&&", e.Text)
}

func TestZeroPaddedCharacterReferences(t *testing.T) {
	ref := "&#" + strings.Repeat("0", 40) + "65;"
	hex := "&#x" + strings.Repeat("0", 40) + "42;"
	src := `<a v="` + ref + hex + `">` + ref + hex + `</a>`
	for name, reader := range cursorReaders(src) {
		for _, limit := range []int{0, 4} {
			t.Run(name+"/"+strconv.Itoa(limit), func(t *testing.T) {
				opts := Options{MaxTokenBytes: limit}
				d, err := NewDecoder(context.Background(), reader(), opts)
				require.NoError(t, err)
				var b Batch
				_, err = d.NextEvent()
				require.NoError(t, err)
				require.NoError(t, d.AppendNode(&b))
				trees, err := b.Elements()
				require.NoError(t, err)
				assert.Equal(t, "AB", trees[0].Attrs["v"])
				assert.Equal(t, "AB", trees[0].Text)
			})
		}
	}
	for _, bad := range []string{
		"&#" + strings.Repeat("0", 40) + ";",
		"&#" + strings.Repeat("0", 40) + "1114112;",
		"&#" + strings.Repeat("0", 40) + "65",
		"&#" + strings.Repeat("0", 40) + "6x;",
		"&#x" + strings.Repeat("0", 40) + "110000;",
	} {
		_, err := Decode([]byte("<a>"+bad+"</a>"), Options{})
		code(t, err, "syntax")
		var e apierror.Error
		require.ErrorAs(t, err, &e)
		assert.EqualValues(t, 4, detail(e, "column"), "the error points at the reference: %q", bad)
	}
}

func TestCancellationBetweenBufferedEvents(t *testing.T) {
	src := "<r>" + strings.Repeat("<b/>", 2000) + "</r>"
	ops := map[string]func(*Decoder) error{
		"next": func(d *Decoder) error {
			for {
				if _, err := d.NextEvent(); err != nil {
					return err
				}
			}
		},
		"node": func(d *Decoder) error { var b Batch; return d.AppendThrough(&b, 0, 0) },
		"skip": func(d *Decoder) error { return d.SkipThrough(0) },
		"copy": func(d *Decoder) error {
			w, err := NewEncoder(EncodeOptions{})
			require.NoError(t, err)
			require.NoError(t, w.StartElement("r", nil, ""))
			return d.CopyThrough(w, 0, nil)
		},
	}
	for name, op := range ops {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			d, err := NewDecoder(ctx, strings.NewReader(src), Options{})
			require.NoError(t, err)
			for range 3 {
				_, err := d.NextEvent()
				require.NoError(t, err)
			}
			require.Greater(t, d.hi-d.lo, 1000, "the document is buffered")
			cancel()
			before := d.pos.Offset
			err = op(d)
			var e apierror.Error
			require.ErrorAs(t, err, &e)
			assert.Equal(t, apierror.Canceled, e.Kind())
			assert.Less(t, d.pos.Offset-before, int64(4*(cancelCheckInterval+1)), "canceled within a bounded number of events")
			_, again := d.NextEvent()
			assert.Same(t, err, again, "the canceled error is sticky")
		})
	}
}

func TestRecentNameCacheCollisions(t *testing.T) {
	var s strings.Builder
	s.WriteString(`<root xmlns:p="urn:p">`)
	for i := range 300 {
		n := fmt.Sprintf("a%03dz", i)
		fmt.Fprintf(&s, `<%s p:%s="%d" %s="x"/><p:%s/>`, n, n, i, n, n)
	}
	s.WriteString(`</root>`)
	e, err := Decode([]byte(s.String()), Options{})
	require.NoError(t, err)
	require.Len(t, e.Children, 600)
	for i := range 300 {
		n := fmt.Sprintf("a%03dz", i)
		c := e.Children[2*i].(*Element)
		assert.Equal(t, n, c.Name)
		assert.Equal(t, strconv.Itoa(i), c.Attrs["{urn:p}"+n])
		assert.Equal(t, "x", c.Attrs[n])
		q := e.Children[2*i+1].(*Element)
		assert.Equal(t, n, q.Name)
		assert.Equal(t, "urn:p", q.NS)
	}
}
