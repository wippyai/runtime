// SPDX-License-Identifier: MPL-2.0

package sserelay

import (
	"errors"
	"io"
	"net/http"
	"strings"
)

// sseEncoder writes RFC 8895-compatible event stream frames.
// It is owned by a single goroutine.
type sseEncoder struct {
	flusher http.Flusher
	writer  io.Writer
}

func newSSEEncoder(w http.ResponseWriter) (*sseEncoder, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, ErrSSEFlusherUnavailable
	}
	return &sseEncoder{
		flusher: flusher,
		writer:  w,
	}, nil
}

func (e *sseEncoder) writeEvent(name, data string) error {
	if strings.ContainsAny(name, "\r\n") {
		return errors.New("event name contains newline")
	}

	var b strings.Builder
	b.Grow(len(name) + len(data) + 32)

	if name != "" {
		b.WriteString("event: ")
		b.WriteString(name)
		b.WriteByte('\n')
	}

	appendSSEDataLines(&b, data)
	b.WriteByte('\n')

	if _, err := io.WriteString(e.writer, b.String()); err != nil {
		return err
	}
	e.flusher.Flush()
	return nil
}

func (e *sseEncoder) writeComment(comment string) error {
	var b strings.Builder
	b.Grow(len(comment) + 8)

	appendSSECommentLines(&b, comment)
	b.WriteByte('\n')

	if _, err := io.WriteString(e.writer, b.String()); err != nil {
		return err
	}
	e.flusher.Flush()
	return nil
}

func appendSSEDataLines(b *strings.Builder, data string) {
	start := 0
	for i := 0; i <= len(data); i++ {
		if i == len(data) || data[i] == '\n' {
			b.WriteString("data: ")
			b.WriteString(data[start:i])
			b.WriteByte('\n')
			start = i + 1
		}
	}
}

func appendSSECommentLines(b *strings.Builder, comment string) {
	start := 0
	for i := 0; i <= len(comment); i++ {
		if i == len(comment) || comment[i] == '\n' {
			b.WriteString(": ")
			b.WriteString(comment[start:i])
			b.WriteByte('\n')
			start = i + 1
		}
	}
}
