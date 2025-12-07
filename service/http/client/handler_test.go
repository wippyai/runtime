package client

import (
	"context"
	"io"
	gohttp "net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	httpapi "github.com/wippyai/runtime/api/dispatcher/http"
)

type testReceiver struct {
	fn func(data any)
}

func (r *testReceiver) CompleteYield(_ uint64, data any, _ error) {
	r.fn(data)
}

func TestClientPoolDefaultClient(t *testing.T) {
	pool := NewClientPool()

	c1 := pool.GetClient(0, "")
	c2 := pool.GetClient(defaultTimeout, "")
	c3 := pool.GetClient(0, "")

	if c1 != c2 || c2 != c3 {
		t.Error("default client should be reused")
	}

	if pool.Size() != 0 {
		t.Errorf("expected empty pool, got %d", pool.Size())
	}
}

func TestClientPoolCustomTimeout(t *testing.T) {
	pool := NewClientPool()

	c1 := pool.GetClient(time.Minute, "")
	c2 := pool.GetClient(time.Minute, "")
	c3 := pool.GetClient(2*time.Minute, "")

	if c1 != c2 {
		t.Error("same timeout should reuse client")
	}
	if c1 == c3 {
		t.Error("different timeout should create new client")
	}

	if pool.Size() != 2 {
		t.Errorf("expected 2 pooled clients, got %d", pool.Size())
	}
}

func TestClientPoolUnixSocket(t *testing.T) {
	pool := NewClientPool()

	c1 := pool.GetClient(0, "/var/run/docker.sock")
	c2 := pool.GetClient(0, "/var/run/docker.sock")
	c3 := pool.GetClient(0, "/var/run/other.sock")
	c4 := pool.GetClient(0, "")

	if c1 != c2 {
		t.Error("same socket should reuse client")
	}
	if c1 == c3 {
		t.Error("different socket should create new client")
	}
	if c1 == c4 || c3 == c4 {
		t.Error("socket client should differ from default")
	}
}

func TestClientPoolConcurrentAccess(t *testing.T) {
	pool := NewClientPool()
	var wg sync.WaitGroup
	const goroutines = 100
	const iterations = 100

	clients := make([]map[*gohttp.Client]bool, goroutines)
	for i := 0; i < goroutines; i++ {
		clients[i] = make(map[*gohttp.Client]bool)
	}

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				timeout := time.Duration((j%5)+1) * time.Second
				c := pool.GetClient(timeout, "")
				clients[idx][c] = true
			}
		}(i)
	}

	wg.Wait()

	for i, cm := range clients {
		if len(cm) > 5 {
			t.Errorf("goroutine %d saw %d clients, expected <= 5", i, len(cm))
		}
	}

	if pool.Size() != 5 {
		t.Errorf("expected 5 pooled clients, got %d", pool.Size())
	}
}

func TestClientPoolNoResourceExhaustion(t *testing.T) {
	pool := NewClientPool()

	for i := 0; i < 1000; i++ {
		pool.GetClient(time.Duration(i)*time.Millisecond, "")
	}

	size := pool.Size()
	if size > 1000 {
		t.Errorf("pool grew unbounded: %d", size)
	}
}

func TestDispatcher_Request(t *testing.T) {
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.Header().Set("X-Custom", "test")
		gohttp.SetCookie(w, &gohttp.Cookie{Name: "session", Value: "abc123"})
		w.WriteHeader(gohttp.StatusOK)
		w.Write([]byte("hello"))
	}))
	defer ts.Close()

	d := NewDispatcher()
	done := make(chan httpapi.Response, 1)

	err := d.handleRequest(context.Background(), &httpapi.RequestCmd{
		Method: "GET",
		URL:    ts.URL,
	}, 0, &testReceiver{fn: func(data any) {
		done <- data.(httpapi.Response)
	}})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case resp := <-done:
		if resp.Error != "" {
			t.Fatalf("unexpected response error: %s", resp.Error)
		}
		if resp.StatusCode != 200 {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
		if string(resp.Body) != "hello" {
			t.Errorf("expected 'hello', got %q", resp.Body)
		}
		if resp.Headers["X-Custom"] != "test" {
			t.Errorf("missing custom header")
		}
		if resp.Cookies["session"] != "abc123" {
			t.Errorf("missing cookie")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestDispatcher_RequestPost(t *testing.T) {
	var receivedBody []byte
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(gohttp.StatusOK)
	}))
	defer ts.Close()

	d := NewDispatcher()
	done := make(chan httpapi.Response, 1)

	err := d.handleRequest(context.Background(), &httpapi.RequestCmd{
		Method:  "POST",
		URL:     ts.URL,
		Body:    []byte(`{"key":"value"}`),
		Headers: map[string]string{"Content-Type": "application/json"},
	}, 0, &testReceiver{fn: func(data any) {
		done <- data.(httpapi.Response)
	}})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case resp := <-done:
		if resp.Error != "" {
			t.Fatalf("unexpected response error: %s", resp.Error)
		}
		if string(receivedBody) != `{"key":"value"}` {
			t.Errorf("body mismatch: %s", receivedBody)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestDispatcher_RequestTimeout(t *testing.T) {
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(gohttp.StatusOK)
	}))
	defer ts.Close()

	d := NewDispatcher()
	done := make(chan httpapi.Response, 1)

	start := time.Now()
	err := d.handleRequest(context.Background(), &httpapi.RequestCmd{
		Method:  "GET",
		URL:     ts.URL,
		Timeout: 50 * time.Millisecond,
	}, 0, &testReceiver{fn: func(data any) {
		done <- data.(httpapi.Response)
	}})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case resp := <-done:
		elapsed := time.Since(start)
		if resp.Error == "" {
			t.Error("expected timeout error")
		}
		if elapsed > 150*time.Millisecond {
			t.Errorf("timeout took too long: %v", elapsed)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for response")
	}
}

func TestDispatcher_RequestContextCancellation(t *testing.T) {
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		time.Sleep(time.Second)
		w.WriteHeader(gohttp.StatusOK)
	}))
	defer ts.Close()

	d := NewDispatcher()
	ctx, cancel := context.WithCancel(context.Background())
	emitted := make(chan bool, 1)

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := d.handleRequest(ctx, &httpapi.RequestCmd{
		Method: "GET",
		URL:    ts.URL,
	}, 0, &testReceiver{fn: func(data any) {
		emitted <- true
	}})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// When context is cancelled, emit should NOT be called to avoid races
	// with the executor that has already moved on
	select {
	case <-emitted:
		t.Fatal("emit should not be called after context cancellation")
	case <-time.After(300 * time.Millisecond):
		// Expected: no emit after cancellation
		elapsed := time.Since(start)
		if elapsed > 500*time.Millisecond {
			t.Errorf("test took too long: %v", elapsed)
		}
	}
}

func TestDispatcher_RequestInvalidURL(t *testing.T) {
	d := NewDispatcher()
	done := make(chan httpapi.Response, 1)

	err := d.handleRequest(context.Background(), &httpapi.RequestCmd{
		Method: "GET",
		URL:    "://invalid",
	}, 0, &testReceiver{fn: func(data any) {
		done <- data.(httpapi.Response)
	}})

	if err != nil {
		t.Fatalf("handler should not return error: %v", err)
	}

	select {
	case resp := <-done:
		if resp.Error == "" {
			t.Error("expected error in response")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestDispatcher_RequestConnectionError(t *testing.T) {
	d := NewDispatcher()
	done := make(chan httpapi.Response, 1)

	err := d.handleRequest(context.Background(), &httpapi.RequestCmd{
		Method:  "GET",
		URL:     "http://localhost:59999",
		Timeout: 100 * time.Millisecond,
	}, 0, &testReceiver{fn: func(data any) {
		done <- data.(httpapi.Response)
	}})

	if err != nil {
		t.Fatalf("handler should not return error: %v", err)
	}

	select {
	case resp := <-done:
		if resp.Error == "" {
			t.Error("expected connection error in response")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestDispatcher_RequestConcurrent(t *testing.T) {
	var reqCount atomic.Int64
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		reqCount.Add(1)
		w.WriteHeader(gohttp.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer ts.Close()

	d := NewDispatcher()
	const concurrency = 50
	const requests = 20

	var wg sync.WaitGroup
	var errCount atomic.Int64

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < requests; j++ {
				done := make(chan httpapi.Response, 1)
				err := d.handleRequest(context.Background(), &httpapi.RequestCmd{
					Method: "GET",
					URL:    ts.URL,
				}, 0, &testReceiver{fn: func(data any) {
					done <- data.(httpapi.Response)
				}})
				if err != nil {
					errCount.Add(1)
					continue
				}

				select {
				case resp := <-done:
					if resp.Error != "" || resp.StatusCode != 200 {
						errCount.Add(1)
					}
				case <-time.After(5 * time.Second):
					errCount.Add(1)
				}
			}
		}()
	}

	wg.Wait()

	if errCount.Load() > 0 {
		t.Errorf("had %d errors in concurrent requests", errCount.Load())
	}

	if reqCount.Load() != concurrency*requests {
		t.Errorf("expected %d requests, got %d", concurrency*requests, reqCount.Load())
	}
}

func TestDispatcher_RequestLargeResponse(t *testing.T) {
	const size = 10 * 1024 * 1024
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		data := make([]byte, size)
		for i := range data {
			data[i] = byte(i % 256)
		}
		w.Write(data)
	}))
	defer ts.Close()

	d := NewDispatcher()
	done := make(chan httpapi.Response, 1)

	err := d.handleRequest(context.Background(), &httpapi.RequestCmd{
		Method: "GET",
		URL:    ts.URL,
	}, 0, &testReceiver{fn: func(data any) {
		done <- data.(httpapi.Response)
	}})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case resp := <-done:
		if resp.Error != "" {
			t.Fatalf("unexpected response error: %s", resp.Error)
		}
		if len(resp.Body) != size {
			t.Errorf("expected %d bytes, got %d", size, len(resp.Body))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout")
	}
}

func TestDispatcher_RegisterAll(t *testing.T) {
	d := NewDispatcher()
	handlers := make(map[dispatcher.CommandID]bool)

	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = true
	})

	if !handlers[httpapi.CmdRequest] {
		t.Error("request handler not registered")
	}
	if !handlers[httpapi.CmdRequestBatch] {
		t.Error("batch request handler not registered")
	}
}

func TestDispatcher_RequestBatch(t *testing.T) {
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.Header().Set("X-Path", r.URL.Path)
		w.Write([]byte("ok"))
	}))
	defer ts.Close()

	d := NewDispatcher()
	done := make(chan httpapi.BatchResponse, 1)

	err := d.handleRequestBatch(context.Background(), &httpapi.RequestBatchCmd{
		Requests: []*httpapi.RequestCmd{
			{Method: "GET", URL: ts.URL + "/one"},
			{Method: "GET", URL: ts.URL + "/two"},
			{Method: "GET", URL: ts.URL + "/three"},
		},
	}, 0, &testReceiver{fn: func(data any) {
		done <- data.(httpapi.BatchResponse)
	}})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case resp := <-done:
		if len(resp.Responses) != 3 {
			t.Fatalf("expected 3 responses, got %d", len(resp.Responses))
		}
		for i, r := range resp.Responses {
			if r.Error != "" {
				t.Errorf("response %d error: %s", i, r.Error)
			}
			if r.StatusCode != 200 {
				t.Errorf("response %d status: %d", i, r.StatusCode)
			}
			if string(r.Body) != "ok" {
				t.Errorf("response %d body: %s", i, r.Body)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestDispatcher_RequestBatchEmpty(t *testing.T) {
	d := NewDispatcher()
	var resp httpapi.BatchResponse

	err := d.handleRequestBatch(context.Background(), &httpapi.RequestBatchCmd{
		Requests: nil,
	}, 0, &testReceiver{fn: func(data any) {
		resp = data.(httpapi.BatchResponse)
	}})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Responses) != 0 {
		t.Errorf("expected 0 responses, got %d", len(resp.Responses))
	}
}

func TestDispatcher_RequestBatchPartialFailure(t *testing.T) {
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.Write([]byte("ok"))
	}))
	defer ts.Close()

	d := NewDispatcher()
	done := make(chan httpapi.BatchResponse, 1)

	err := d.handleRequestBatch(context.Background(), &httpapi.RequestBatchCmd{
		Requests: []*httpapi.RequestCmd{
			{Method: "GET", URL: ts.URL},
			{Method: "GET", URL: "http://localhost:59998", Timeout: 50 * time.Millisecond},
			{Method: "GET", URL: ts.URL},
		},
	}, 0, &testReceiver{fn: func(data any) {
		done <- data.(httpapi.BatchResponse)
	}})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case resp := <-done:
		if len(resp.Responses) != 3 {
			t.Fatalf("expected 3 responses, got %d", len(resp.Responses))
		}
		if resp.Responses[0].Error != "" {
			t.Errorf("response 0 should succeed: %s", resp.Responses[0].Error)
		}
		if resp.Responses[1].Error == "" {
			t.Error("response 1 should fail")
		}
		if resp.Responses[2].Error != "" {
			t.Errorf("response 2 should succeed: %s", resp.Responses[2].Error)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestDispatcher_RequestBatchConcurrent(t *testing.T) {
	var reqCount atomic.Int64
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		reqCount.Add(1)
		time.Sleep(20 * time.Millisecond)
		w.Write([]byte("ok"))
	}))
	defer ts.Close()

	d := NewDispatcher()
	done := make(chan httpapi.BatchResponse, 1)

	requests := make([]*httpapi.RequestCmd, 10)
	for i := range requests {
		requests[i] = &httpapi.RequestCmd{Method: "GET", URL: ts.URL}
	}

	start := time.Now()
	err := d.handleRequestBatch(context.Background(), &httpapi.RequestBatchCmd{
		Requests: requests,
	}, 0, &testReceiver{fn: func(data any) {
		done <- data.(httpapi.BatchResponse)
	}})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case resp := <-done:
		elapsed := time.Since(start)
		if len(resp.Responses) != 10 {
			t.Fatalf("expected 10 responses, got %d", len(resp.Responses))
		}
		// All requests should run concurrently
		if elapsed > 150*time.Millisecond {
			t.Errorf("batch not concurrent: took %v", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func BenchmarkClientPoolGetDefault(b *testing.B) {
	pool := NewClientPool()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			pool.GetClient(0, "")
		}
	})
}

func BenchmarkClientPoolGetCustom(b *testing.B) {
	pool := NewClientPool()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			timeout := time.Duration((i%5)+1) * time.Second
			pool.GetClient(timeout, "")
			i++
		}
	})
}

func BenchmarkDispatcher_Request(b *testing.B) {
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.WriteHeader(gohttp.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer ts.Close()

	d := NewDispatcher()
	ctx := context.Background()
	cmd := &httpapi.RequestCmd{Method: "GET", URL: ts.URL}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			done := make(chan struct{})
			d.handleRequest(ctx, cmd, 0, &testReceiver{fn: func(data any) {
				close(done)
			}})
			<-done
		}
	})
}

func BenchmarkDispatcher_RequestWithTimeout(b *testing.B) {
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.WriteHeader(gohttp.StatusOK)
	}))
	defer ts.Close()

	d := NewDispatcher()
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			cmd := &httpapi.RequestCmd{
				Method:  "GET",
				URL:     ts.URL,
				Timeout: 5 * time.Second,
			}
			done := make(chan struct{})
			d.handleRequest(ctx, cmd, 0, &testReceiver{fn: func(data any) {
				close(done)
			}})
			<-done
		}
	})
}
