// SPDX-License-Identifier: MPL-2.0

package http

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type drainProbeBody struct {
	io.Reader
	reads int
}

func (b *drainProbeBody) Read(p []byte) (int, error) {
	b.reads++
	return b.Reader.Read(p)
}

func (*drainProbeBody) Close() error { return nil }

type drainProbeWriter struct {
	*httptest.ResponseRecorder
	conn            net.Conn
	hijackErr       error
	deadlineChanges int
}

func (w *drainProbeWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.conn, nil, w.hijackErr
}

func (w *drainProbeWriter) SetReadDeadline(time.Time) error {
	w.deadlineChanges++
	return nil
}

func TestClosingBodyDrainRespectsHijackOwnership(t *testing.T) {
	for _, failed := range []bool{false, true} {
		name := "successful"
		if failed {
			name = "failed"
		}
		t.Run(name, func(t *testing.T) {
			conn, peer := net.Pipe()
			defer conn.Close()
			defer peer.Close()
			w := &drainProbeWriter{ResponseRecorder: httptest.NewRecorder(), conn: conn}
			if failed {
				w.hijackErr = errors.New("hijack refused")
			}
			body := &drainProbeBody{Reader: strings.NewReader("remaining body")}
			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil)
			r.Close, r.Body = true, body
			drainClosingRequestBody(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				got, _, err := http.NewResponseController(writer).Hijack()
				require.ErrorIs(t, err, w.hijackErr)
				if !failed {
					require.Same(t, conn, got)
				}
			})).ServeHTTP(w, r)
			if failed {
				require.Positive(t, body.reads, "a refused hijack must not suppress normal HTTP cleanup")
				require.Equal(t, 1, w.deadlineChanges)
			} else {
				require.Zero(t, body.reads, "hijacked input belongs to the handler")
				require.Zero(t, w.deadlineChanges, "hijacked deadlines belong to the handler")
			}
		})
	}
}

func TestClosingBodyDrainPreservesStreamingCapabilities(t *testing.T) {
	w := &drainProbeWriter{ResponseRecorder: httptest.NewRecorder()}
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader("body"))
	r.Close = true
	drainClosingRequestBody(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, canHijack := writer.(http.Hijacker)
		require.True(t, canHijack)
		_, canPush := writer.(http.Pusher)
		require.False(t, canPush, "the wrapper must not invent unsupported capabilities")
		require.NoError(t, http.NewResponseController(writer).Flush())
		require.True(t, w.Flushed, "streaming must reach the original writer")
	})).ServeHTTP(w, r)
	require.Equal(t, 1, w.deadlineChanges)
}

func TestClosingBodyDrainLeavesOtherRequestsUntouched(t *testing.T) {
	for _, tc := range []struct {
		body  io.ReadCloser
		name  string
		close bool
	}{
		{name: "keepalive", body: &drainProbeBody{Reader: strings.NewReader("body")}},
		{name: "no body", close: true, body: http.NoBody},
		{name: "nil body", close: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := &drainProbeWriter{ResponseRecorder: httptest.NewRecorder()}
			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil)
			r.Close, r.Body = tc.close, tc.body
			drainClosingRequestBody(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				require.Same(t, w, writer, "ordinary requests must keep their original writer")
			})).ServeHTTP(w, r)
			require.Zero(t, w.deadlineChanges)
			if body, ok := tc.body.(*drainProbeBody); ok {
				require.Zero(t, body.reads)
			}
		})
	}
}

func BenchmarkClosingBodyDrainKeepAlive(b *testing.B) {
	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(b.Context(), http.MethodGet, "/", nil)
	handler := drainClosingRequestBody(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		handler.ServeHTTP(w, r)
	}
}

func BenchmarkClosingBodyDrainClosing(b *testing.B) {
	w := &drainProbeWriter{ResponseRecorder: httptest.NewRecorder()}
	r := httptest.NewRequestWithContext(b.Context(), http.MethodPost, "/", nil)
	r.Close = true
	r.Body = &drainProbeBody{Reader: strings.NewReader("")}
	handler := drainClosingRequestBody(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		handler.ServeHTTP(w, r)
	}
}
