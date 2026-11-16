// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"context"
	"errors"
	"io"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Fail records a terminal error with the given code, for callers that validate
// their own input while serializing; the first recorded error is kept.
func (w *Encoder) Fail(code, message string) error {
	if code == "limit" {
		return w.limit(message)
	}
	return w.fail(code, message)
}

// Flush writes buffered output to dst and keeps the buffer storage for reuse.
// A failed or short write is terminal.
func (w *Encoder) Flush(dst io.Writer) error {
	if w.err != nil {
		return w.err
	}
	p := w.buf
	for len(p) > 0 {
		n, err := dst.Write(p)
		if err == nil && n == 0 {
			err = io.ErrShortWrite
		}
		if err != nil {
			return w.Abort(err)
		}
		p = p[n:]
	}
	w.written += int64(len(w.buf))
	w.buf = w.buf[:0]
	return nil
}

// Abort records a terminal destination failure supplied by the stream adapter.
func (w *Encoder) Abort(err error) error {
	if w.err != nil {
		return w.err
	}
	if err == nil {
		return nil
	}
	kind := apierror.Internal
	code := "io"
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		kind = apierror.Canceled
		code = "canceled"
	}
	w.err = apierror.New(kind, "XML output failed").WithDetails(attrs.Bag{"code": code}).WithCause(err)
	return w.err
}
