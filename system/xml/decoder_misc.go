// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"bytes"
	"strings"
)

func (d *Decoder) until(end string, start int) (int, error) {
	if len(end) == 1 && d.hi-d.lo > start {
		if i := bytes.IndexByte(d.buf[d.lo+start:d.hi], end[0]); i >= 0 {
			n := start + i + 1
			if n > d.opts.MaxTagBytes {
				return 0, d.limit("max_tag_bytes")
			}
			return n, nil
		}
	}

	for i := start; ; i++ {
		if i+len(end) > d.opts.MaxTagBytes {
			return 0, d.limit("max_tag_bytes")
		}
		if i+len(end) > d.hi-d.lo && !d.ensure(i+len(end)) {
			if d.err != nil {
				return 0, d.err
			}
			return 0, d.fail("syntax", "unterminated markup", "")
		}
		if string(d.buf[d.lo+i:d.lo+i+len(end)]) == end {
			return i + len(end), nil
		}
	}
}

func (d *Decoder) declaration(p []byte) error {
	s := strings.Trim(string(p), " \t\r\n")
	names := []string{"version", "encoding", "standalone"}
	last := -1
	for len(s) > 0 {
		i := strings.IndexByte(s, '=')
		if i < 0 {
			return d.fail("syntax", "invalid XML declaration", "")
		}
		key := strings.Trim(s[:i], " \t\r\n")
		idx := -1
		for j, n := range names {
			if n == key {
				idx = j
			}
		}
		if idx < 0 || idx <= last || last < 0 && idx != 0 {
			return d.fail("syntax", "invalid declaration order", "")
		}
		s = strings.TrimLeft(s[i+1:], " \t\r\n")
		if len(s) == 0 || s[0] != '\'' && s[0] != '"' {
			return d.fail("syntax", "invalid declaration value", "")
		}
		j := strings.IndexByte(s[1:], s[0])
		if j < 0 {
			return d.fail("syntax", "unterminated declaration value", "")
		}
		v := s[1 : 1+j]
		s = s[j+2:]
		if len(s) > 0 && !space(s[0]) {
			return d.fail("syntax", "missing declaration whitespace", "")
		}
		s = strings.TrimLeft(s, " \t\r\n")
		switch idx {
		case 0:
			if v != "1.0" {
				return d.fail("syntax", "XML version must be 1.0", "")
			}
		case 1:
			v = strings.ToLower(v)
			if v != d.encoding && (v != "utf-16" || !strings.HasPrefix(d.encoding, "utf-16")) {
				return d.fail("encoding", "encoding declaration contradicts input", "")
			}
		case 2:
			if v != "yes" && v != "no" {
				return d.fail("syntax", "invalid standalone value", "")
			}
		}
		last = idx
	}
	if last < 0 {
		return d.fail("syntax", "missing XML version", "")
	}
	return nil
}

func (d *Decoder) skipPI() error {
	if err := d.consume(2); err != nil {
		return err
	}
	first := true
	for {
		r, n, err := d.inputRune()
		if err != nil {
			return err
		}
		if r == '?' || r < 128 && space(byte(r)) {
			if first {
				return d.fail("syntax", "missing PI target", "")
			}
			if r == '?' && (!d.ensure(2) || d.buf[d.lo+1] != '>') {
				return d.fail("syntax", "missing PI whitespace", "")
			}
			break
		}
		if r == ':' {
			return d.fail("namespace", "colon in PI target", "")
		}
		if first && !nameStart(r) || !first && !nameChar(r) {
			return d.fail("syntax", "invalid PI target", "")
		}
		if err = d.consume(n); err != nil {
			return err
		}
		first = false
	}
	return d.skipMisc(false)
}

func (d *Decoder) skipMisc(comment bool) error {
	for {
		if !d.ensure(2) {
			if d.err != nil {
				return d.err
			}
			return d.fail("syntax", "unterminated markup", "")
		}
		if comment && d.buf[d.lo] == '-' && d.buf[d.lo+1] == '-' {
			if !d.ensure(3) {
				if d.err != nil {
					return d.err
				}
				return d.fail("syntax", "unterminated comment", "")
			}
			if d.buf[d.lo+2] != '>' {
				return d.fail("syntax", "invalid comment", "")
			}
			return d.consume(3)
		}
		if !comment && d.buf[d.lo] == '?' && d.buf[d.lo+1] == '>' {
			return d.consume(2)
		}
		_, n, err := d.inputRune()
		if err != nil {
			return err
		}
		if err = d.consume(n); err != nil {
			return err
		}
	}
}
