// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// sourceError marks a failure of the underlying reader, as opposed to a decoding problem.
type sourceError struct{ err error }

func (e sourceError) Error() string { return e.err.Error() }
func (e sourceError) Unwrap() error { return e.err }

type checkedReader struct {
	d *Decoder
	r io.Reader
}

// Read checks the decoder's current context and bounds retries on empty reads.
func (r checkedReader) Read(p []byte) (int, error) {
	for range 100 {
		if err := r.d.ctx.Err(); err != nil {
			return 0, err
		}
		n, err := r.r.Read(p)
		if err != nil && !errors.Is(err, io.EOF) {
			err = sourceError{err: err}
		}
		if n != 0 || err != nil {
			return n, err
		}
	}
	return 0, io.ErrNoProgress
}

// sniff reads only the declaration prefix before selecting the byte decoder.
func sniff(r io.Reader, maxTag int) (io.Reader, string, error) {
	var head []byte
	var one [1]byte
	for len(head) < 4 {
		n, err := r.Read(one[:])
		if n > 0 {
			head = append(head, one[0])
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return nil, "", err
			}
			break
		}
		if n == 0 {
			return nil, "", io.ErrNoProgress
		}
	}
	enc := "utf-8"
	bom := false
	switch {
	case bytes.Equal(head, []byte{0xFF, 0xFE, 0, 0}) || bytes.Equal(head, []byte{0, 0, 0xFE, 0xFF}):
		return nil, "", problem("encoding", "unsupported UTF-32 encoding")
	case bytes.HasPrefix(head, []byte{0xEF, 0xBB, 0xBF}):
		head = head[3:]
		bom = true
	case bytes.HasPrefix(head, []byte{0xFF, 0xFE}):
		head = head[2:]
		enc = "utf-16le"
		bom = true
	case bytes.HasPrefix(head, []byte{0xFE, 0xFF}):
		head = head[2:]
		enc = "utf-16be"
		bom = true
	case bytes.Equal(head, []byte{0, '<', 0, '?'}):
		enc = "utf-16be"
	case bytes.Equal(head, []byte{'<', 0, '?', 0}):
		enc = "utf-16le"
	}
	if strings.HasPrefix(enc, "utf-16") {
		return &transcoder{r: io.MultiReader(bytes.NewReader(head), r), encoding: enc}, enc, nil
	}
	for len(head) < 6 {
		n, err := r.Read(one[:])
		if n > 0 {
			head = append(head, one[0])
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return nil, "", err
			}
			break
		}
		if n == 0 {
			return nil, "", io.ErrNoProgress
		}
	}
	if len(head) >= 6 && string(head[:5]) == "<?xml" && space(head[5]) {
		for !bytes.HasSuffix(head, []byte("?>")) {
			if len(head) >= maxTag {
				return nil, "", problem("limit", "max_tag_bytes exceeded")
			}
			n, err := r.Read(one[:])
			if n > 0 {
				head = append(head, one[0])
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					return nil, "", err
				}
				break
			}
			if n == 0 {
				return nil, "", io.ErrNoProgress
			}
		}
		s := string(head)
		i := strings.Index(s, "encoding")
		if i >= 0 {
			v := strings.TrimLeft(s[i+len("encoding"):], " \t\r\n=")
			if len(v) > 0 && (v[0] == '\'' || v[0] == '"') {
				if j := strings.IndexByte(v[1:], v[0]); j >= 0 {
					decl := strings.ToLower(v[1 : 1+j])
					if bom && decl != "utf-8" {
						return nil, "", problem("encoding", "BOM contradicts declaration")
					}
					enc = decl
				}
			}
		}
	}
	source := io.MultiReader(bytes.NewReader(head), r)
	switch enc {
	case "utf-8":
		return source, enc, nil
	case "iso-8859-1", "windows-1252", "us-ascii":
		return &transcoder{r: source, encoding: enc}, enc, nil
	default:
		return nil, "", problem("encoding", "unsupported or contradictory encoding")
	}
}

type transcoder struct {
	inputErr error
	r        io.Reader
	err      error
	encoding string
	lo       int
	hi       int
	n        int
	raw      [transcoderBufferBytes]byte
	pending  [4]byte
}

var cp1252 = [cp1252ControlCount]rune{0x20AC, 0, 0x201A, 0x192, 0x201E, 0x2026, 0x2020, 0x2021, 0x2C6, 0x2030, 0x160, 0x2039, 0x152, 0, 0x17D, 0, 0, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014, 0x2DC, 0x2122, 0x161, 0x203A, 0x153, 0, 0x17E, 0x178}

func (t *transcoder) nextByte() (byte, error) {
	if t.lo == t.hi {
		if t.inputErr != nil {
			return 0, t.inputErr
		}
		n, err := t.r.Read(t.raw[:])
		t.lo = 0
		t.hi = n
		t.inputErr = err
		if n == 0 {
			if err == nil {
				err = io.ErrNoProgress
			}
			return 0, err
		}
	}
	b := t.raw[t.lo]
	t.lo++
	return b, nil
}

func (t *transcoder) unit() (uint16, error) {
	a, err := t.nextByte()
	if err != nil {
		return 0, err
	}
	b, err := t.nextByte()
	if err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return 0, err
	}
	if t.encoding == "utf-16le" {
		return uint16(a) | uint16(b)<<bitsPerByte, nil
	}
	return uint16(a)<<bitsPerByte | uint16(b), nil
}

// Read transcodes input bytes into validated UTF-8.
func (t *transcoder) Read(p []byte) (int, error) {
	n := 0
	for n < len(p) {
		if t.n > 0 {
			k := copy(p[n:], t.pending[:t.n])
			copy(t.pending[:], t.pending[k:t.n])
			t.n -= k
			n += k
			continue
		}
		if t.err != nil {
			return n, t.err
		}
		var r rune
		var err error
		if strings.HasPrefix(t.encoding, "utf-16") {
			var u uint16
			u, err = t.unit()
			r = rune(u)
			if err == nil && u >= 0xD800 && u <= 0xDBFF {
				var v uint16
				v, err = t.unit()
				if err != nil || v < 0xDC00 || v > 0xDFFF {
					err = problem("encoding", "malformed UTF-16 surrogate pair")
				} else {
					r = utf16.DecodeRune(r, rune(v))
				}
			} else if err == nil && u >= 0xDC00 && u <= 0xDFFF {
				err = problem("encoding", "unpaired UTF-16 surrogate")
			}
		} else {
			var b byte
			b, err = t.nextByte()
			r = rune(b)
			if err == nil {
				if t.encoding == "us-ascii" && r > 127 {
					err = problem("encoding", "malformed US-ASCII")
				} else if t.encoding == "windows-1252" && r >= 0x80 && r <= 0x9F {
					r = cp1252[r-0x80]
					if r == 0 {
						err = problem("encoding", "undefined windows-1252 byte")
					}
				}
			}
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			err = problem("encoding", "truncated encoded character")
		}
		if err != nil {
			t.err = err
			continue
		}
		t.n = utf8.EncodeRune(t.pending[:], r)
	}
	return n, nil
}
