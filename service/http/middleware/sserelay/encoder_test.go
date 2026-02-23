// SPDX-License-Identifier: MPL-2.0

package sserelay

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncoderWriteEvent(t *testing.T) {
	w := httptest.NewRecorder()
	enc, err := newSSEEncoder(w)
	require.NoError(t, err)

	require.NoError(t, enc.writeEvent("delta", `{"t":"hi"}`))

	body := w.Body.String()
	assert.Contains(t, body, "event: delta\n")
	assert.Contains(t, body, "data: {\"t\":\"hi\"}\n\n")
	assert.True(t, w.Flushed)
}

func TestEncoderWriteEventMultiline(t *testing.T) {
	w := httptest.NewRecorder()
	enc, err := newSSEEncoder(w)
	require.NoError(t, err)

	require.NoError(t, enc.writeEvent("delta", "a\nb\nc"))

	body := w.Body.String()
	assert.Contains(t, body, "data: a\n")
	assert.Contains(t, body, "data: b\n")
	assert.Contains(t, body, "data: c\n\n")
}

func TestEncoderWriteComment(t *testing.T) {
	w := httptest.NewRecorder()
	enc, err := newSSEEncoder(w)
	require.NoError(t, err)

	require.NoError(t, enc.writeComment("ping\nok"))

	body := w.Body.String()
	assert.Contains(t, body, ": ping\n")
	assert.Contains(t, body, ": ok\n\n")
}

func TestEncoderInvalidEventName(t *testing.T) {
	w := httptest.NewRecorder()
	enc, err := newSSEEncoder(w)
	require.NoError(t, err)

	err = enc.writeEvent("bad\nevent", "x")
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "newline")
}

func TestNewSSEEncoderRequiresFlusher(t *testing.T) {
	w := &noFlushWriter{header: make(http.Header)}
	enc, err := newSSEEncoder(w)
	assert.Nil(t, enc)
	assert.ErrorIs(t, err, ErrSSEFlusherUnavailable)
}

type noFlushWriter struct {
	header http.Header
}

func (w *noFlushWriter) Header() http.Header {
	return w.header
}

func (w *noFlushWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

func (w *noFlushWriter) WriteHeader(_ int) {}
