package httphandler

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"

	"go.uber.org/zap"
)

var _ io.ReadCloser = (*wrapper)(nil)
var _ http.ResponseWriter = (*wrapper)(nil)

type wrapper struct {
	io.ReadCloser
	read  int
	write int

	// TwoXXSent is true if the response headers with >= 2xx code were sent
	// 1xx header might be sent unlimited number of times
	wc bool

	w    http.ResponseWriter
	code int
	data []byte
}

func (w *wrapper) Read(b []byte) (int, error) {
	n, err := w.ReadCloser.Read(b)
	w.read += n
	return n, err
}

func (w *wrapper) WriteHeader(code int) {
	w.code = code
	if w.wc {
		return
	}

	// do not allow sending 200 twice
	if code >= 100 && code < 200 {
		w.wc = true
	}

	w.w.WriteHeader(code)
}

func (w *wrapper) Header() http.Header {
	return w.w.Header()
}

func (w *wrapper) Write(b []byte) (int, error) {
	w.wc = true
	n, err := w.w.Write(b)
	w.write += n
	return n, err
}

func (w *wrapper) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := w.w.(http.Hijacker); ok {
		return hj.Hijack()
	}

	return nil, nil, errors.New("http.Hijacker interface is not supported")
}

func (w *wrapper) Flush() {
	if fl, ok := w.w.(http.Flusher); ok {
		fl.Flush()
	}
}

func (w *wrapper) Close() error {
	return w.ReadCloser.Close()
}

func (w *wrapper) reset() {
	w.code = http.StatusOK
	w.read = 0
	w.wc = false
	w.write = 0
	w.w = nil
	w.data = nil
	w.ReadCloser = nil
}

type lm struct {
	pool sync.Pool
	log  *zap.Logger
}

func (l *lm) Wrapper(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wr := l.pool.Get().(*wrapper)
		wr.reset()
		wr.w = w
		wr.ReadCloser = r.Body
		next.ServeHTTP(wr, r)
		l.pool.Put(wr)
	})
}

func NewResponseWriteWrapperHandler(next http.Handler, log *zap.Logger) http.Handler {
	l := &lm{
		log: log,
		pool: sync.Pool{
			New: func() any {
				return &wrapper{
					code: http.StatusOK,
				}
			},
		},
	}

	return l.Wrapper(next)
}
