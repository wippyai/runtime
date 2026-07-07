// SPDX-License-Identifier: MPL-2.0

package http

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	httpservice "github.com/wippyai/runtime/api/service/http"
)

// delayedErrReader returns err after delay, regardless of ctx. Used to drive
// the requestBody / requestBodyJSON error path AFTER the request's timeout
// has already fired — exactly the condition under which the production code
// used to block forever on an unbuffered errChan, leaking the body-reader
// goroutine.
type delayedErrReader struct {
	done  chan struct{}
	err   error
	delay time.Duration
}

func newDelayedErrReader(delay time.Duration, err error) *delayedErrReader {
	r := &delayedErrReader{delay: delay, err: err, done: make(chan struct{})}
	go func() {
		time.Sleep(delay)
		close(r.done)
	}()
	return r
}

func (r *delayedErrReader) Read([]byte) (int, error) {
	<-r.done
	return 0, r.err
}

func (r *delayedErrReader) Close() error { return nil }

// TestRequestBodyDoesNotLeakGoroutineOnErrorAfterTimeout reproduces a
// goroutine leak in requestBody / requestBodyJSON:
//
// The body-reader goroutine performs `errChan <- err` (an unbuffered channel)
// when io.ReadAll returns an error. The outer select may have already chosen
// <-ctx.Done() (timeout), in which case nobody reads errChan and the goroutine
// blocks forever. Each request that times out AND whose body read subsequently
// errors leaks one goroutine permanently.
func TestRequestBodyDoesNotLeakGoroutineOnErrorAfterTimeout(t *testing.T) {
	const iters = 50
	before := runtime.NumGoroutine()
	t.Logf("goroutines before=%d", before)

	l := lua.NewState()
	defer l.Close()
	bind(l)
	ctx, fc := newTestContext()
	defer ctxapi.ReleaseFrameContext(fc)
	l.SetContext(ctx)

	for i := 0; i < iters; i++ {
		// Body errors 200 ms after construction; request timeout is 50 ms.
		// The outer select fires ctx.Done first; then the goroutine's
		// io.ReadAll returns the error and tries errChan <- err.
		body := newDelayedErrReader(200*time.Millisecond, errors.New("simulated read error"))
		req, err := http.NewRequestWithContext(context.Background(), "POST", "/test", body)
		require.NoError(t, err)
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		require.NoError(t, fc.Set(httpservice.RequestKey(), reqCtx))

		err = l.DoString(`
			local req = http.request({timeout = 50})
			local body, err = req:body()
			assert(body == nil, "body should be nil on timeout")
			assert(err ~= nil, "should return error on timeout")
		`)
		require.NoError(t, err, "lua iteration %d", i)
	}

	// Wait long enough that every goroutine's delayedErrReader has fired and
	// (pre-fix) parked on the blocking errChan send. Then GC several times to
	// recycle anything legitimately short-lived.
	time.Sleep(500 * time.Millisecond)
	for i := 0; i < 5; i++ {
		runtime.GC()
		time.Sleep(20 * time.Millisecond)
	}

	after := runtime.NumGoroutine()
	t.Logf("goroutines after=%d (delta=%d over %d timed-out+errored body reads)",
		after, after-before, iters)

	// Tolerate small framework noise. A real leak would be ~+iters.
	const tolerance = 5
	require.Less(t, after-before, tolerance,
		"goroutine count grew by %d after %d timed-out body reads with subsequent errors; "+
			"the body-reader goroutine must not block forever on errChan after the outer "+
			"select has chosen ctx.Done",
		after-before, iters)
}

// TestRequestBodyJSONDoesNotLeakGoroutineOnErrorAfterTimeout is the same
// contract for the body_json code path, which has the identical bug.
func TestRequestBodyJSONDoesNotLeakGoroutineOnErrorAfterTimeout(t *testing.T) {
	const iters = 50
	before := runtime.NumGoroutine()
	t.Logf("goroutines before=%d", before)

	l := lua.NewState()
	defer l.Close()
	bind(l)
	ctx, fc := newTestContext()
	defer ctxapi.ReleaseFrameContext(fc)
	l.SetContext(ctx)

	for i := 0; i < iters; i++ {
		body := newDelayedErrReader(200*time.Millisecond, io.ErrUnexpectedEOF)
		req, err := http.NewRequestWithContext(context.Background(), "POST", "/test", body)
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		require.NoError(t, fc.Set(httpservice.RequestKey(), reqCtx))

		err = l.DoString(`
			local req = http.request({timeout = 50})
			local data, err = req:body_json()
			assert(data == nil, "data should be nil on timeout")
			assert(err ~= nil, "should return error on timeout")
		`)
		require.NoError(t, err, "lua iteration %d", i)
	}

	time.Sleep(500 * time.Millisecond)
	for i := 0; i < 5; i++ {
		runtime.GC()
		time.Sleep(20 * time.Millisecond)
	}

	after := runtime.NumGoroutine()
	t.Logf("goroutines after=%d (delta=%d over %d timed-out+errored body_json reads)",
		after, after-before, iters)

	const tolerance = 5
	require.Less(t, after-before, tolerance,
		"goroutine count grew by %d after %d timed-out body_json reads with subsequent errors",
		after-before, iters)
}
