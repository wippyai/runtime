// SPDX-License-Identifier: MPL-2.0

package http

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/felixge/httpsnoop"
	"github.com/wippyai/runtime/api/logs"
	"go.uber.org/zap"
)

func drainClosingRequestBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !r.Close || r.Body == nil || r.Body == http.NoBody {
			next.ServeHTTP(w, r)
			return
		}

		// Keep the native writer for MaxBytesReader: its private limit callback
		// asks net/http to close gracefully when the drain budget is exceeded.
		nativeWriter := w
		hijacked := false
		w = httpsnoop.Wrap(w, httpsnoop.Hooks{
			Hijack: func(next httpsnoop.HijackFunc) httpsnoop.HijackFunc {
				return func() (net.Conn, *bufio.ReadWriter, error) {
					conn, rw, err := next()
					if err == nil {
						hijacked = true
					}
					return conn, rw, err
				}
			},
		})
		defer func() {
			if hijacked {
				return // The handler now owns the connection, including its unread bytes.
			}
			controller := http.NewResponseController(nativeWriter)
			if err := controller.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
				logs.GetLogger(r.Context()).Error("set request body drain deadline", zap.Error(err))
				return
			}
			// net/http skips its usual body drain for closing requests. Bound
			// cleanup by both bytes and time, including withheld request bodies.
			body := http.MaxBytesReader(nativeWriter, r.Body, 256<<10)
			if _, err := io.Copy(io.Discard, body); err != nil {
				logs.GetLogger(r.Context()).Debug("request body drain interrupted", zap.Error(err))
			}
		}()
		next.ServeHTTP(w, r)
	})
}
