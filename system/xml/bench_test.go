// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"bytes"
	"context"
	stdxml "encoding/xml"
	"errors"
	"io"
	"strings"
	"testing"
)

func benchmarkDocuments() map[string][]byte {
	soapRecord := `<m:record m:id="123">` + strings.Repeat(`<m:node>`, 24) + `payload` + strings.Repeat(`</m:node>`, 24) + `</m:record>`
	return map[string][]byte{
		"feed": []byte("<feed>" + strings.Repeat(`<item id="123" status="active"><title>Example record</title><value>12345</value></item>`, 125000) + "</feed>"),
		"soap": []byte(`<s:Envelope xmlns:s="urn:soap" xmlns:m="urn:message"><s:Body>` + strings.Repeat(soapRecord, 24000) + `</s:Body></s:Envelope>`),
		"text": []byte("<text>" + strings.Repeat("Text with UTF-8 café and entity &amp; more text. ", 220000) + "</text>"),
	}
}
func BenchmarkXML(b *testing.B) {
	documents := benchmarkDocuments()
	for _, name := range []string{"feed", "soap", "text"} {
		data := documents[name]
		b.Run(name, func(b *testing.B) {
			opts := Options{MaxNodeBytes: len(data) + 1}
			for _, op := range []string{"Cursor", "Skip", "Select", "Decode", "Encode", "Copy"} {
				b.Run(op, func(b *testing.B) {
					b.SetBytes(int64(len(data)))
					b.ReportAllocs()
					var tree *Element
					var encoded []byte
					if op == "Encode" {
						var err error
						tree, err = Decode(data, opts)
						if err != nil {
							b.Fatal(err)
						}
						encoded, err = Encode(tree, EncodeOptions{MaxNodeBytes: 64 << 20})
						if err != nil {
							b.Fatal(err)
						}
					}
					b.ResetTimer()
					for range b.N {
						if op == "Encode" {
							out, err := Encode(tree, EncodeOptions{MaxNodeBytes: 64 << 20})
							if err != nil || len(out) != len(encoded) {
								b.Fatal(err)
							}
							continue
						}
						d, err := NewDecoder(context.Background(), bytes.NewReader(data), opts)
						if err != nil {
							b.Fatal(err)
						}
						switch op {
						case "Decode":
							if _, err = Decode(data, opts); err != nil {
								b.Fatal(err)
							}
						case "Select":
							path := "feed/item"
							ns := map[string]string{"s": "urn:soap", "m": "urn:message"}
							if name == "soap" {
								path = "s:Envelope/s:Body/m:record"
							}
							if name == "text" {
								path = "text"
							}
							p, _ := CompilePath(path, ns)
							s, _ := NewSelector(d, []Path{p})
							var batch Batch
							for {
								batch.Reset()
								_, err = s.Read(&batch, 64)
								if errors.Is(err, io.EOF) {
									break
								}
								if err != nil {
									b.Fatal(err)
								}
								if _, err = batch.Elements(); err != nil {
									b.Fatal(err)
								}
							}
						case "Copy":
							w, _ := NewEncoder(EncodeOptions{})
							for {
								ev, err := d.NextEvent()
								if errors.Is(err, io.EOF) {
									break
								}
								if err != nil {
									b.Fatal(err)
								}
								if ev.Kind == StartEvent {
									if err = d.Copy(w); err != nil {
										b.Fatal(err)
									}
								}
							}
							if err = w.Finish(); err != nil {
								b.Fatal(err)
							}
						default:
							for {
								ev, err := d.NextEvent()
								if errors.Is(err, io.EOF) {
									break
								}
								if err != nil {
									b.Fatal(err)
								}
								if op == "Skip" && ev.Kind == StartEvent {
									if err = d.Skip(); err != nil {
										b.Fatal(err)
									}
								}
							}
						}
					}
				})
			}
			for _, op := range []string{"Cursor", "Skip", "Select", "Decode", "Encode", "Copy"} {
				b.Run("Std"+op, func(b *testing.B) {
					var tree stdTree
					if op == "Encode" {
						if err := stdxml.Unmarshal(data, &tree); err != nil {
							b.Fatal(err)
						}
					}
					b.ResetTimer()
					b.SetBytes(int64(len(data)))
					b.ReportAllocs()
					for range b.N {
						if op == "Encode" {
							if _, err := stdxml.Marshal(tree); err != nil {
								b.Fatal(err)
							}
							continue
						}
						d := stdxml.NewDecoder(bytes.NewReader(data))
						var out bytes.Buffer
						w := stdxml.NewEncoder(&out)
						for {
							v, err := d.Token()
							if errors.Is(err, io.EOF) {
								break
							}
							if err != nil {
								b.Fatal(err)
							}
							switch op {
							case "Skip":
								if _, ok := v.(stdxml.StartElement); ok {
									if err = d.Skip(); err != nil {
										b.Fatal(err)
									}
								}
							case "Decode", "Select":
								if start, ok := v.(stdxml.StartElement); ok {
									if op == "Select" && start.Name.Local != "item" && start.Name.Local != "record" && start.Name.Local != "text" {
										continue
									}
									var e stdTree
									if err = d.DecodeElement(&e, &start); err != nil {
										b.Fatal(err)
									}
								}
							case "Copy":
								if err = w.EncodeToken(v); err != nil {
									b.Fatal(err)
								}
							}
						}
						if op == "Copy" {
							if err := w.Flush(); err != nil {
								b.Fatal(err)
							}
						}
					}
				})
			}
		})
	}
}

type stdTree struct {
	XMLName  stdxml.Name
	Attrs    []stdxml.Attr `xml:",any,attr"`
	Text     string        `xml:",chardata"`
	Children []stdTree     `xml:",any"`
}

func BenchmarkCursorSteady(b *testing.B) {
	data := []byte("<r>" + strings.Repeat(`<i a="1">text</i>`, 10000) + "</r>")
	d, _ := NewDecoder(context.Background(), bytes.NewReader(data), Options{})
	for range 32 {
		_, _ = d.NextEvent()
	}
	b.ReportAllocs()
	b.SetBytes(5)
	b.ResetTimer()
	for range b.N {
		if _, err := d.NextEvent(); errors.Is(err, io.EOF) {
			b.StopTimer()
			d, _ = NewDecoder(context.Background(), bytes.NewReader(data), Options{})
			for range 32 {
				_, _ = d.NextEvent()
			}
			b.StartTimer()
		} else if err != nil {
			b.Fatal(err)
		}
	}
}
