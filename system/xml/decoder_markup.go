// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"bytes"
	"io"
	"strings"
	"unicode/utf8"
)

// Next returns an event whose slices borrow decoder storage.
func (d *Decoder) Next() (Event, error) {
	ev, err := d.NextEvent()
	if err != nil {
		return Event{}, err
	}
	return *ev, nil
}

// NextEvent returns the reusable event, or io.EOF after the document.
func (d *Decoder) NextEvent() (*Event, error) {
	if d.selected && !d.selectionReading {
		return nil, problem("state", "selection owns the decoder")
	}

	if d.err != nil {
		return nil, d.err
	}
	if d.closed {
		return nil, d.fail("state", "decoder is closed", "")
	}
	// Buffered input is decoded without reads, so cancellation is also checked per interval.
	if d.events++; d.events%cancelCheckInterval == 0 {
		if err := d.ctx.Err(); err != nil {
			return nil, d.wrap(err)
		}
	}
	if !d.initialized {
		d.initialized = true
		r, enc, err := sniff(checkedReader{d: d, r: d.source}, d.opts.MaxTagBytes)
		if err != nil {
			return nil, d.wrap(err)
		}
		d.reader = r
		d.encoding = enc
		d.buf = make([]byte, min(bufferBytes, max(d.opts.MaxTagBytes, d.opts.MaxTokenBytes)+bufferLookahead))
	}
	if d.pop {
		f := d.stack[len(d.stack)-1]
		d.ns.truncate(f.scope)
		d.stack[len(d.stack)-1] = frame{}
		d.stack = d.stack[:len(d.stack)-1]
		d.pop = false
	}
	d.scratch = d.scratch[:0]
	d.attributes = d.attributes[:0]
	if len(d.declarations) > 0 {
		clear(d.declarations)
		d.declarations = d.declarations[:0]
	}
	if d.empty {
		d.empty = false
		f := d.stack[len(d.stack)-1]
		d.pop = true
		d.current = Event{Kind: EndEvent, Name: f.name, NS: f.ns, Depth: len(d.stack) - 1, Pos: d.pos, EndOffset: d.pos.Offset}
		return &d.current, nil
	}
	for {
		if d.lo == d.hi && !d.ensure(1) {
			if d.err != nil {
				return nil, d.err
			}
			if len(d.stack) > 0 || !d.root {
				return nil, d.fail("syntax", "incomplete document", "")
			}
			d.current = Event{}
			return nil, io.EOF
		}
		if len(d.stack) == 0 && d.buf[d.lo] != '<' {
			if !space(d.buf[d.lo]) {
				if d.buf[d.lo] >= 128 {
					n := 1
					for !utf8.FullRune(d.buf[d.lo : d.lo+n]) {
						n++
						if !d.ensure(n) {
							return nil, d.fail("encoding", "truncated UTF-8", "")
						}
					}
					r, k := utf8.DecodeRune(d.buf[d.lo : d.lo+n])
					if r == utf8.RuneError && k == 1 {
						return nil, d.fail("encoding", "malformed UTF-8", "")
					}
				}
				return nil, d.fail("syntax", "text outside root", "")
			}
			n := 0
			for d.lo+n < d.hi && space(d.buf[d.lo+n]) {
				n++
			}
			if err := d.consume(n); err != nil {
				return nil, err
			}
			continue
		}
		pos := d.pos
		if d.cdata || d.buf[d.lo] != '<' {
			ev, err := d.text()
			if err != nil {
				return nil, err
			}
			if len(ev.Text) == 0 {
				continue
			}
			if len(d.stack) == 0 {
				if !white(ev.Text) {
					return nil, d.fail("syntax", "text outside root", "")
				}
				continue
			}

			d.current = ev
			return &d.current, nil
		}
		d.runContent = false
		if d.hi-d.lo < 2 && !d.ensure(2) {
			return nil, d.fail("syntax", "incomplete markup", "")
		}
		switch d.buf[d.lo+1] {
		case '!':
			if d.ensure(4) && bytes.Equal(d.buf[d.lo:d.lo+4], []byte("<!--")) {
				if !d.opts.Comments {
					if err := d.consume(4); err != nil {
						return nil, err
					}
					if err := d.skipMisc(true); err != nil {
						return nil, err
					}
					continue
				}

				n, err := d.until("-->", 4)
				if err != nil {
					return nil, err
				}
				p := d.buf[d.lo+4 : d.lo+n-3]
				if bytes.Contains(p, []byte("--")) || len(p) > 0 && p[len(p)-1] == '-' {
					return nil, d.fail("syntax", "invalid comment", "")
				}
				v, err := d.normalize(p, false, false, 4)
				if err != nil {
					return nil, err
				}
				if err = d.consume(n); err != nil {
					return nil, err
				}
				if d.opts.Comments {
					d.current = Event{Kind: CommentEvent, Text: v, Pos: pos, Depth: len(d.stack), EndOffset: d.pos.Offset}
					return &d.current, nil
				}
				d.scratch = d.scratch[:0]
				continue
			}
			if d.ensure(9) && bytes.Equal(d.buf[d.lo:d.lo+9], []byte("<![CDATA[")) {
				if len(d.stack) == 0 {
					return nil, d.fail("syntax", "CDATA outside root", "")
				}
				if err := d.consume(9); err != nil {
					return nil, err
				}
				d.cdata = true
				continue
			}
			return nil, d.fail("syntax", "unsupported declaration", "")
		case '?':
			if !d.opts.Instructions {
				_ = d.ensure(6)
				declaration := d.hi-d.lo >= 5 && strings.EqualFold(string(d.buf[d.lo+2:d.lo+5]), "xml") && (d.hi-d.lo == 5 || space(d.buf[d.lo+5]) || d.buf[d.lo+5] == '?')
				if !declaration {
					if err := d.skipPI(); err != nil {
						return nil, err
					}
					continue
				}
			}
			n, err := d.until("?>", 2)
			if err != nil {
				return nil, err
			}
			p := d.buf[d.lo+2 : d.lo+n-2]
			i := 0
			for i < len(p) && !space(p[i]) {
				i++
			}
			name := d.intern(p[:i])
			if !utf8.ValidString(name) {
				return nil, d.failAt("encoding", "malformed UTF-8 target", 2)
			}
			if strings.ContainsRune(name, ':') {
				return nil, d.fail("namespace", "colon in PI target", "")
			}
			if !validName(name) {
				return nil, d.fail("syntax", "invalid PI target", "")
			}
			if strings.EqualFold(name, "xml") {
				if name != "xml" || pos.Offset != 0 || d.root {
					return nil, d.fail("syntax", "misplaced XML declaration", "")
				}
				if err = d.declaration(p[i:]); err != nil {
					return nil, err
				}
				if err = d.consume(n); err != nil {
					return nil, err
				}
				continue
			}
			for i < len(p) && space(p[i]) {
				i++
			}
			v, err := d.normalize(p[i:], false, false, 2+i)
			if err != nil {
				return nil, err
			}
			if err = d.consume(n); err != nil {
				return nil, err
			}
			if d.opts.Instructions {
				d.current = Event{Kind: PIEvent, Name: name, Text: v, Depth: len(d.stack), Pos: pos, EndOffset: d.pos.Offset}
				return &d.current, nil
			}
			d.scratch = d.scratch[:0]
			continue
		case '/':
			if len(d.stack) == 0 {
				return nil, d.fail("syntax", "unexpected end tag", "")
			}
			f := d.stack[len(d.stack)-1]
			n := 2 + len(f.qname)
			if !d.ensure(n + 1) {
				if d.err != nil {
					return nil, d.err
				}
				return nil, d.fail("syntax", "unterminated end tag", "")
			}
			if string(d.buf[d.lo+2:d.lo+n]) != f.qname {
				if r, k := utf8.DecodeRune(d.buf[d.lo+2 : d.hi]); utf8.FullRune(d.buf[d.lo+2:d.hi]) && r == utf8.RuneError && k == 1 {
					return nil, d.failAt("encoding", "malformed UTF-8 end tag", 2)
				}
				return nil, d.fail("syntax", "mismatched end tag", "")
			}
			if d.buf[d.lo+n] == '>' {
				if err := d.consume(n + 1); err != nil {
					return nil, err
				}
			} else {
				if !space(d.buf[d.lo+n]) {
					return nil, d.fail("syntax", "invalid end tag", "")
				}
				if err := d.consume(n); err != nil {
					return nil, err
				}
				for {
					if !d.ensure(1) {
						if d.err != nil {
							return nil, d.err
						}
						return nil, d.fail("syntax", "unterminated end tag", "")
					}
					if d.buf[d.lo] == '>' {
						if err := d.consume(1); err != nil {
							return nil, err
						}
						break
					}
					n = 0
					for d.lo+n < d.hi && space(d.buf[d.lo+n]) {
						n++
					}
					if n == 0 {
						return nil, d.fail("syntax", "invalid end tag whitespace", "")
					}
					if err := d.consume(n); err != nil {
						return nil, err
					}
				}
			}
			d.pop = true
			d.current = Event{Kind: EndEvent, Name: f.name, NS: f.ns, Depth: len(d.stack) - 1, Pos: pos, EndOffset: d.pos.Offset}
			return &d.current, nil

		default:
			if err := d.start(pos); err != nil {
				return nil, err
			}
			return &d.current, nil
		}
	}
}
