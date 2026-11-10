// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"bytes"
	"unicode/utf8"
)

func (d *Decoder) normalize(p []byte, attribute, entities bool, base int) ([]byte, error) {
	if attribute {
		plain := true
		ascii := true
		for _, c := range p {
			if c < asciiSpace || c == '&' || c == '<' {
				plain = false
				break
			}
			if c >= utf8.RuneSelf {
				ascii = false
			}
		}
		if plain && (ascii || utf8.Valid(p)) {
			return p, nil
		}
	}
	begin := len(d.scratch)
	for i := 0; i < len(p); {
		b := p[i]
		if b == '&' && entities {
			j := bytes.IndexByte(p[i:], ';')
			if j < 0 {
				return nil, d.failAt("syntax", "unterminated reference", base+i)
			}
			r, ok := referenceRune(p[i+1 : i+j])
			if !ok {
				return nil, d.failAt("syntax", referenceProblem(p[i+1:i+j]), base+i)
			}
			d.scratch = utf8.AppendRune(d.scratch, r)
			i += j + 1
			continue
		}
		if b == '<' && attribute {
			return nil, d.failAt("syntax", "less-than in attribute", base+i)
		}
		r, n := utf8.DecodeRune(p[i:])
		if r == utf8.RuneError && n == 1 {
			return nil, d.failAt("encoding", "malformed UTF-8", base+i)
		}
		if !validChar(r) {
			return nil, d.failAt("syntax", "invalid XML character", base+i)
		}
		if r == '\r' {
			r = '\n'
			if i+n < len(p) && p[i+n] == '\n' {
				n++
			}
		}
		if attribute && (r == '\n' || r == '\t') {
			r = ' '
		}
		d.scratch = utf8.AppendRune(d.scratch, r)
		i += n
	}
	return d.scratch[begin:], nil
}

func (d *Decoder) text() (Event, error) {
	pos := d.pos
	if d.opts.strip && !d.cdata && !d.runContent && !d.stack[len(d.stack)-1].preserve {
		n := 0
		for n <= d.opts.MaxTokenBytes && d.ensure(n+1) && space(d.buf[d.lo+n]) {
			n++
		}
		if n <= d.opts.MaxTokenBytes && d.ensure(n+1) && d.buf[d.lo+n] == '<' {
			if err := d.consume(n); err != nil {
				return Event{}, err
			}
			return Event{Kind: TextEvent, Pos: pos}, nil
		}
		d.runContent = true
	}
	if !d.cdata {
		available := d.buf[d.lo:d.hi]
		n := min(len(available), d.opts.MaxTokenBytes)
		i := 0
		for i < n {
			c := available[i]
			if c == '<' {
				break
			}
			if c == '&' || c == '\r' || c == ']' || c >= 128 || c < asciiSpace && c != '\n' && c != '\t' {
				i = 0
				break
			}
			i++
		}
		if i == len(available) && i < d.opts.MaxTokenBytes {
			// The run continues past the buffered input; the slow path reads on.
			i = 0
		}
		if i > 0 {
			p := available[:i]
			if err := d.consume(i); err != nil {
				return Event{}, err
			}
			return Event{Kind: TextEvent, Text: p, Pos: pos, Depth: len(d.stack), EndOffset: d.pos.Offset}, nil
		}
	}
	start := len(d.scratch)
	consumed := 0
	pending := 0
	for {
		if !d.ensure(pending + 1) {
			if d.err != nil {
				return Event{}, d.err
			}
			if d.cdata {
				return Event{}, d.fail("syntax", "unterminated CDATA", "")
			}
			break
		}
		room := d.opts.MaxTokenBytes - (len(d.scratch) - start)
		seg := d.buf[d.lo+pending : d.lo+pending+min(d.hi-d.lo-pending, room, bufferBytes-consumed)]
		j := 0
		for j < len(seg) {
			c := seg[j]
			if plainText[c] {
				j++
				continue
			}
			if c < utf8.RuneSelf {
				break
			}
			r, k := utf8.DecodeRune(seg[j:])
			if r == utf8.RuneError && k == 1 || !validChar(r) {
				break
			}
			j += k
		}
		if j > 0 {
			d.scratch = append(d.scratch, seg[:j]...)
			pending += j
			consumed += j
			if len(d.scratch)-start == d.opts.MaxTokenBytes || consumed >= bufferBytes {
				break
			}
			continue
		}
		// Special units are handled at the cursor, so the plain span before them is consumed.
		if pending > 0 {
			if err := d.consume(pending); err != nil {
				return Event{}, err
			}
			pending = 0
		}
		b := d.buf[d.lo]
		if b == ']' && d.ensure(3) && string(d.buf[d.lo:d.lo+3]) == "]]>" {
			if !d.cdata {
				return Event{}, d.fail("syntax", "CDATA terminator in text", "")
			}
			if len(d.scratch) == start {
				if err := d.consume(3); err != nil {
					return Event{}, err
				}
				d.cdata = false
				return Event{Kind: TextEvent, Pos: pos}, nil
			}
			break
		}
		if b == '<' && !d.cdata {
			break
		}
		if b == '&' && !d.cdata {
			r, n, err := d.textReference(room, len(d.scratch) == start)
			if err != nil {
				return Event{}, err
			}
			if n == 0 {
				break
			}
			d.scratch = utf8.AppendRune(d.scratch, r)
			consumed += n
		} else {
			n := 1
			if b >= 128 {
				for n < utf8.UTFMax && !utf8.FullRune(d.buf[d.lo:d.lo+n]) {
					n++
					if !d.ensure(n) {
						return Event{}, d.fail("encoding", "truncated UTF-8", "")
					}
				}
				_, n = utf8.DecodeRune(d.buf[d.lo : d.lo+n])
			} else if b == '\r' && d.ensure(2) && d.buf[d.lo+1] == '\n' {
				n = 2
			}
			before := len(d.scratch)
			if _, err := d.normalize(d.buf[d.lo:d.lo+n], false, false, 0); err != nil {
				return Event{}, err
			}
			if len(d.scratch)-start > d.opts.MaxTokenBytes {
				d.scratch = d.scratch[:before]
				if before == start {
					return Event{}, d.limit("max_token_bytes")
				}
				break
			}
			if err := d.consume(n); err != nil {
				return Event{}, err
			}
			consumed += n
		}
		if len(d.scratch)-start == d.opts.MaxTokenBytes || consumed >= bufferBytes {
			break
		}
	}
	if pending > 0 {
		if err := d.consume(pending); err != nil {
			return Event{}, err
		}
	}
	return Event{Kind: TextEvent, Text: d.scratch[start:], Pos: pos, Depth: len(d.stack), EndOffset: d.pos.Offset}, nil
}

// plainText marks the bytes a text run copies unchanged: printable ASCII other than
// markup and CDATA delimiters, tab and line feed.
var plainText = func() (t [256]bool) {
	for c := asciiSpace; c < utf8.RuneSelf; c++ {
		t[c] = c != '<' && c != '&' && c != ']'
	}
	t['\t'] = true
	t['\n'] = true
	return t
}()

// textReference decodes the reference at the cursor and consumes it when its character
// fits in room bytes; n is zero when it does not fit and an empty token can take it later.
// A reference within the peek window is decoded before anything is consumed. Only leading
// zeros make a valid reference longer; those are consumed as they are read, so nothing
// is buffered beyond the window.
func (d *Decoder) textReference(room int, empty bool) (rune, int, error) {
	pos := d.pos
	window := min(referencePeek, max(d.opts.MaxTagBytes, d.opts.MaxTokenBytes)+bufferLookahead)
	d.ensure(window)
	if d.err != nil {
		return 0, 0, d.err
	}
	avail := d.buf[d.lo:min(d.hi, d.lo+window)]
	if i := bytes.IndexByte(avail, ';'); i >= 0 {
		r, ok := referenceRune(avail[1:i])
		if !ok {
			return 0, 0, d.failAtPosition("syntax", referenceProblem(avail[1:i]), pos)
		}
		if utf8.RuneLen(r) > room {
			if empty {
				return 0, 0, d.limit("max_token_bytes")
			}
			return 0, 0, nil
		}
		if err := d.consume(i + 1); err != nil {
			return 0, 0, err
		}
		return r, i + 1, nil
	}
	if len(avail) < 2 || avail[1] != '#' {
		return 0, 0, d.failAtPosition("syntax", "invalid reference", pos)
	}
	if room < utf8.UTFMax && !empty {
		return 0, 0, nil
	}
	var body [referencePeek]byte
	k := copy(body[:], "#")
	n := 2
	if len(avail) > 2 && avail[2] == 'x' {
		body[k] = 'x'
		k++
		n = 3
	}
	if err := d.consume(n); err != nil {
		return 0, 0, err
	}
	digits := 0
	for {
		if !d.ensure(1) {
			if d.err != nil {
				return 0, 0, d.err
			}
			return 0, 0, d.failAtPosition("syntax", "unterminated reference", pos)
		}
		c := d.buf[d.lo]
		if err := d.consume(1); err != nil {
			return 0, 0, err
		}
		n++
		if c == ';' {
			break
		}
		if c == '0' && digits == 0 {
			continue
		}
		if k == len(body) {
			return 0, 0, d.failAtPosition("syntax", "invalid character reference", pos)
		}
		body[k] = c
		k++
		digits++
	}
	if digits == 0 {
		body[k] = '0'
		k++
	}
	r, ok := referenceRune(body[:k])
	if !ok {
		return 0, 0, d.failAtPosition("syntax", "invalid character reference", pos)
	}
	if utf8.RuneLen(r) > room {
		return 0, 0, d.limit("max_token_bytes")
	}
	return r, n, nil
}

func referenceProblem(body []byte) string {
	if len(body) > 0 && body[0] == '#' {
		return "invalid character reference"
	}
	return "unknown entity"
}

// referenceRune decodes the body of an entity or character reference, between & and ;.
func referenceRune(body []byte) (rune, bool) {
	switch string(body) {
	case "lt":
		return '<', true
	case "gt":
		return '>', true
	case "amp":
		return '&', true
	case "apos":
		return '\'', true
	case "quot":
		return '"', true
	}
	if len(body) < 2 || body[0] != '#' {
		return 0, false
	}
	digits := body[1:]
	base := rune(10)
	if digits[0] == 'x' {
		base = 16
		digits = digits[1:]
	}
	if len(digits) == 0 {
		return 0, false
	}
	var v rune
	for _, c := range digits {
		var x rune
		switch {
		case c >= '0' && c <= '9':
			x = rune(c - '0')
		case base == 16 && c >= 'a' && c <= 'f':
			x = rune(c-'a') + 10
		case base == 16 && c >= 'A' && c <= 'F':
			x = rune(c-'A') + 10
		default:
			return 0, false
		}
		v = v*base + x
		if v > utf8.MaxRune {
			return 0, false
		}
	}
	return v, validChar(v)
}
