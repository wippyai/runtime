// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"bytes"
	"context"
	stdxml "encoding/xml"
	"errors"
	"io"
	"reflect"
	"sort"
	"testing"
)

var seeds = []string{"<a A=\"\t\"></a>", `<a/>`, `<a x="1">a&amp;b<![CDATA[c]]></a>`, `<a xmlns="u" xmlns:p="v"><p:b p:x="z"/></a>`, `<?xml version="1.0"?><a>𐐀</a>`, "<a>\r\n</a>", `<a><!--x--><?p x?></a>`, `<a>]]&gt;</a>`}

func FuzzDecoder(f *testing.F) {
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		d, err := NewDecoder(context.Background(), bytes.NewReader(b), Options{MaxBytes: 1 << 20, MaxNodeBytes: 1 << 18, MaxTagBytes: 1 << 14, MaxTokenBytes: 4096, MaxDepth: 32, MaxAttrs: 32})
		if err != nil {
			t.Fatal(err)
		}
		for n := 0; ; n++ {
			_, err = d.Next()
			if err != nil {
				break
			}
			if n > 2*len(b)+1 {
				t.Fatal("unbounded event count")
			}
		}
	})
}

type oracleEvent struct {
	kind  string
	name  string
	text  string
	attrs []string
}

func mergeText(es []oracleEvent, s string) []oracleEvent {
	if s == "" {
		return es
	}
	if len(es) > 0 && es[len(es)-1].kind == "text" {
		es[len(es)-1].text += s
		return es
	}
	return append(es, oracleEvent{kind: "text", text: s})
}

// Strict encoding/xml also permits constructs this contract rejects: DOCTYPE,
// non-1.0 declarations, undeclared prefixes, duplicate attributes, multiple roots,
// reserved namespace misuse, and characters outside the Fifth Edition name grammar.
func isASCII(p []byte) bool {
	for _, c := range p {
		if c >= 0x80 {
			return false
		}
	}
	return true
}

func FuzzDifferential(f *testing.F) {
	f.Add([]byte("<\u052c/>"))
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 1<<18 {
			return
		}
		d, err := NewDecoder(context.Background(), bytes.NewReader(b), Options{MaxBytes: 1 << 18, MaxTagBytes: 1 << 14, MaxDepth: 32})
		if err != nil {
			t.Fatal(err)
		}
		var got []oracleEvent
		for {
			ev, err := d.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return
			}
			switch ev.Kind {
			case StartEvent:
				v := oracleEvent{kind: "start", name: AttrKey(ev.NS, ev.Name)}
				for _, a := range ev.Attrs {
					v.attrs = append(v.attrs, AttrKey(a.NS, a.Name)+"="+string(a.Value))
				}
				sort.Strings(v.attrs)
				got = append(got, v)
			case EndEvent:
				got = append(got, oracleEvent{kind: "end", name: AttrKey(ev.NS, ev.Name)})
			case TextEvent:
				got = mergeText(got, string(ev.Text))
			}
		}
		// The oracle receives UTF-8 from the same strict transport transcoder.
		r, _, err := sniff(bytes.NewReader(b), 1<<14)
		if err != nil {
			t.Fatal(err)
		}
		raw, readErr := io.ReadAll(r)
		if readErr != nil {
			t.Fatal(readErr)
		}
		oracle := stdxml.NewDecoder(bytes.NewReader(oracleAttributes(raw)))
		oracle.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
		var want []oracleEvent
		depth := 0
		for {
			token, err := oracle.Token()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				// encoding/xml carries name tables older than the Fifth Edition, so it
				// has no authority over documents that hold non-ASCII characters.
				if !isASCII(raw) {
					return
				}
				t.Fatalf("accepted %q; oracle: %v", b, err)
			}
			switch v := token.(type) {
			case stdxml.StartElement:
				depth++
				ev := oracleEvent{kind: "start", name: AttrKey(v.Name.Space, v.Name.Local)}
				for _, a := range v.Attr {
					if a.Name.Space == "xmlns" || a.Name.Space == "" && a.Name.Local == "xmlns" {
						continue
					}
					ev.attrs = append(ev.attrs, AttrKey(a.Name.Space, a.Name.Local)+"="+a.Value)
				}
				sort.Strings(ev.attrs)
				want = append(want, ev)
			case stdxml.EndElement:
				depth--
				want = append(want, oracleEvent{kind: "end", name: AttrKey(v.Name.Space, v.Name.Local)})
			case stdxml.CharData:
				if depth > 0 {
					want = mergeText(want, string(v))
				}
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("input %q\ngot %#v\nwant %#v", b, got, want)
		}
		{
			e, err := Decode(b, Options{MaxNodeBytes: 1 << 18})
			if err == nil {
				out, err := Encode(e, EncodeOptions{})
				if err != nil {
					t.Fatal(err)
				}
				again, err := Decode(out, Options{})
				if err != nil || !reflect.DeepEqual(e, again) {
					t.Fatalf("round trip: %v", err)
				}
			}
		}
	})
}

// encoding/xml preserves literal attribute tabs and line breaks. XML 1.0
// normalizes them before entity expansion, so the oracle input does that step.
func oracleAttributes(p []byte) []byte {
	out := make([]byte, 0, len(p))
	tag := false
	quote := byte(0)
	for i := 0; i < len(p); i++ {
		c := p[i]
		if !tag && c == '<' {
			end := ""
			switch {
			case bytes.HasPrefix(p[i:], []byte("<!--")):
				end = "-->"
			case bytes.HasPrefix(p[i:], []byte("<![CDATA[")):
				end = "]]>"
			case bytes.HasPrefix(p[i:], []byte("<?")):
				end = "?>"
			}
			if end != "" {
				j := bytes.Index(p[i:], []byte(end))
				if j >= 0 {
					j += len(end)
					out = append(out, p[i:i+j]...)
					i += j - 1
					continue
				}
			}
		}
		if !tag && c == '<' && i+1 < len(p) && p[i+1] != '!' && p[i+1] != '?' {
			tag = true
		}
		if tag {
			if quote != 0 {
				switch c {
				case quote:
					quote = 0
				case '\r':
					c = ' '
					if i+1 < len(p) && p[i+1] == '\n' {
						i++
					}
				case '\t', '\n':
					c = ' '
				}
			} else if c == '\'' || c == '"' {
				quote = c
			} else if c == '>' {
				tag = false
			}
		}
		out = append(out, c)
	}
	return out
}
