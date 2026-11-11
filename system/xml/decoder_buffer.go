// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"context"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

func (d *Decoder) fail(code, message, limit string) apierror.Error {
	return d.failAtPosition(code, message, d.pos, limit)
}

// failAtPosition records a sticky error located at pos, which may precede the cursor.
func (d *Decoder) failAtPosition(code, message string, pos Position, limit ...string) apierror.Error {
	if d.err != nil {
		return d.err
	}
	path := make([]string, len(d.stack))
	for i, f := range d.stack {
		path[i] = f.name
	}
	d.err = failure(code, message, strings.Join(limit, ""), pos, path)
	return d.err
}

// failure builds a document error located at pos inside the open elements of path.
func failure(code, message, limit string, pos Position, path []string) apierror.Error {
	details := attrs.Bag{"code": code, "line": pos.Line, "column": pos.Column, "offset": pos.Offset, "path": path}
	if limit != "" {
		details["limit"] = limit
	}
	kind := apierror.Invalid
	if code == "io" {
		kind = apierror.Internal
	}
	if code == "canceled" {
		kind = apierror.Canceled
	}
	return apierror.WithDetails(kind, message, details)
}

func (d *Decoder) wrap(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		d.err = apierror.SetCause(d.fail("canceled", err.Error(), ""), err)
		return d.err
	}
	var source sourceError
	var e apierror.Error
	if !errors.As(err, &source) && errors.As(err, &e) {
		c := e.Details().GetString("code", "syntax")
		limit := ""
		if c == "limit" {
			limit = "max_tag_bytes"
		}
		return d.fail(c, err.Error(), limit)
	}
	d.err = apierror.SetCause(d.fail("io", err.Error(), ""), err)
	return d.err
}

func (d *Decoder) limit(name string) error { return d.fail("limit", name+" exceeded", name) }

func (d *Decoder) ensure(n int) bool {
	for d.hi-d.lo < n {
		if d.readErr != nil {
			if !errors.Is(d.readErr, io.EOF) {
				_ = d.wrap(d.readErr)
			}
			return false
		}
		if err := d.ctx.Err(); err != nil {
			_ = d.wrap(err)
			return false
		}
		if d.lo > 0 {
			copy(d.buf, d.buf[d.lo:d.hi])
			d.hi -= d.lo
			d.lo = 0
		}
		if len(d.buf)-d.hi < n-(d.hi-d.lo) {
			size := len(d.buf) * 2
			if size < n {
				size = n
			}
			ceiling := max(d.opts.MaxTagBytes, d.opts.MaxTokenBytes) + bufferLookahead
			if size > ceiling {
				size = ceiling
			}
			if size < n {
				_ = d.limit("max_tag_bytes")
				return false
			}
			b := make([]byte, size)
			copy(b, d.buf[:d.hi])
			d.buf = b
		}
		k, err := d.reader.Read(d.buf[d.hi:])
		d.hi += k
		d.readErr = err
		if k == 0 && err == nil {
			d.readErr = io.ErrNoProgress
		}
	}
	return true
}

func (d *Decoder) consume(n int) error {
	p := d.buf[d.lo : d.lo+n]
	offset, line, column := d.pos.Offset, d.pos.Line, d.pos.Column
	cr := d.cr
	for len(p) > 0 {
		i := 0
		for i < len(p) && p[i] >= 0x20 && p[i] < 0x80 {
			i++
		}
		if i > 0 {
			offset += int64(i)
			column += i
			cr = false
			p = p[i:]
			continue
		}
		r, k := utf8.DecodeRune(p)
		if r == utf8.RuneError && k == 1 {
			d.pos = Position{Offset: offset, Line: line, Column: column}
			return d.fail("encoding", "malformed UTF-8", "")
		}
		if !validChar(r) {
			d.pos = Position{Offset: offset, Line: line, Column: column}
			return d.fail("syntax", "invalid XML character", "")
		}
		offset += int64(k)
		switch r {
		case '\r':
			line++
			column = 1
			cr = true
		case '\n':
			if !cr {
				line++
			}
			column = 1
			cr = false
		default:
			column++
			cr = false
		}
		p = p[k:]
	}
	d.pos = Position{Offset: offset, Line: line, Column: column}
	d.cr = cr
	if d.opts.MaxBytes > 0 && offset > d.opts.MaxBytes {
		return d.limit("max_bytes")
	}
	d.lo += n
	return nil
}

func (d *Decoder) failAt(code, message string, offset int) error {
	if err := d.consume(offset); err != nil {
		return err
	}
	return d.fail(code, message, "")
}

func (d *Decoder) inputRune() (rune, int, error) {
	if !d.ensure(1) {
		if d.err != nil {
			return 0, 0, d.err
		}
		return 0, 0, d.fail("syntax", "unterminated markup", "")
	}
	n := 1
	for !utf8.FullRune(d.buf[d.lo : d.lo+n]) {
		n++
		if !d.ensure(n) {
			if d.err != nil {
				return 0, 0, d.err
			}
			return 0, 0, d.fail("encoding", "truncated UTF-8", "")
		}
	}
	r, n := utf8.DecodeRune(d.buf[d.lo : d.lo+n])
	if r == utf8.RuneError && n == 1 {
		return 0, 0, d.fail("encoding", "malformed UTF-8", "")
	}
	return r, n, nil
}
