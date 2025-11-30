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

func TestRequestHandler(t *testing.T) {
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.Header().Set("X-Custom", "test")
		gohttp.SetCookie(w, &gohttp.Cookie{Name: "session", Value: "abc123"})
		w.WriteHeader(gohttp.StatusOK)
		w.Write([]byte("hello"))
	}))
	defer ts.Close()

	h := NewRequestHandler()
	var resp httpapi.Response

	err := h.Handle(context.Background(), &httpapi.RequestCmd{
		Method: "GET",
		URL:    ts.URL,
	}, func(data any) {
		resp = data.(httpapi.Response)
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
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
}

func TestRequestHandlerPost(t *testing.T) {
	var receivedBody []byte
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(gohttp.StatusOK)
	}))
	defer ts.Close()

	h := NewRequestHandler()
	var resp httpapi.Response

	err := h.Handle(context.Background(), &httpapi.RequestCmd{
		Method:  "POST",
		URL:     ts.URL,
		Body:    []byte(`{"key":"value"}`),
		Headers: map[string]string{"Content-Type": "application/json"},
	}, func(data any) {
		resp = data.(httpapi.Response)
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("unexpected response error: %s", resp.Error)
	}
	if string(receivedBody) != `{"key":"value"}` {
		t.Errorf("body mismatch: %s", receivedBody)
	}
}

func TestRequestHandlerTimeout(t *testing.T) {
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(gohttp.StatusOK)
	}))
	defer ts.Close()

	h := NewRequestHandler()
	var resp httpapi.Response

	start := time.Now()
	err := h.Handle(context.Background(), &httpapi.RequestCmd{
		Method:  "GET",
		URL:     ts.URL,
		Timeout: 50 * time.Millisecond,
	}, func(data any) {
		resp = data.(httpapi.Response)
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Error == "" {
		t.Error("expected timeout error")
	}
	if elapsed > 150*time.Millisecond {
		t.Errorf("timeout took too long: %v", elapsed)
	}
}

func TestRequestHandlerContextCancellation(t *testing.T) {
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		time.Sleep(time.Second)
		w.WriteHeader(gohttp.StatusOK)
	}))
	defer ts.Close()

	h := NewRequestHandler()
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	var resp httpapi.Response
	start := time.Now()
	err := h.Handle(ctx, &httpapi.RequestCmd{
		Method: "GET",
		URL:    ts.URL,
	}, func(data any) {
		resp = data.(httpapi.Response)
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Error == "" {
		t.Error("expected cancellation error")
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("cancellation took too long: %v", elapsed)
	}
}

func TestRequestHandlerInvalidURL(t *testing.T) {
	h := NewRequestHandler()
	var resp httpapi.Response

	err := h.Handle(context.Background(), &httpapi.RequestCmd{
		Method: "GET",
		URL:    "://invalid",
	}, func(data any) {
		resp = data.(httpapi.Response)
	})

	if err != nil {
		t.Fatalf("handler should not return error: %v", err)
	}
	if resp.Error == "" {
		t.Error("expected error in response")
	}
}

func TestRequestHandlerConnectionError(t *testing.T) {
	h := NewRequestHandler()
	var resp httpapi.Response

	err := h.Handle(context.Background(), &httpapi.RequestCmd{
		Method:  "GET",
		URL:     "http://localhost:59999",
		Timeout: 100 * time.Millisecond,
	}, func(data any) {
		resp = data.(httpapi.Response)
	})

	if err != nil {
		t.Fatalf("handler should not return error: %v", err)
	}
	if resp.Error == "" {
		t.Error("expected connection error in response")
	}
}

func TestRequestHandlerConcurrent(t *testing.T) {
	var reqCount atomic.Int64
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		reqCount.Add(1)
		w.WriteHeader(gohttp.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer ts.Close()

	h := NewRequestHandler()
	const concurrency = 50
	const requests = 20

	var wg sync.WaitGroup
	var errCount atomic.Int64

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < requests; j++ {
				var resp httpapi.Response
				err := h.Handle(context.Background(), &httpapi.RequestCmd{
					Method: "GET",
					URL:    ts.URL,
				}, func(data any) {
					resp = data.(httpapi.Response)
				})
				if err != nil || resp.Error != "" || resp.StatusCode != 200 {
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

func TestRequestHandlerLargeResponse(t *testing.T) {
	const size = 10 * 1024 * 1024
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		data := make([]byte, size)
		for i := range data {
			data[i] = byte(i % 256)
		}
		w.Write(data)
	}))
	defer ts.Close()

	h := NewRequestHandler()
	var resp httpapi.Response

	err := h.Handle(context.Background(), &httpapi.RequestCmd{
		Method: "GET",
		URL:    ts.URL,
	}, func(data any) {
		resp = data.(httpapi.Response)
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("unexpected response error: %s", resp.Error)
	}
	if len(resp.Body) != size {
		t.Errorf("expected %d bytes, got %d", size, len(resp.Body))
	}
}

func TestServiceRegisterAll(t *testing.T) {
	svc := NewService()
	handlers := make(map[dispatcher.CommandID]bool)

	svc.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = true
	})

	if !handlers[httpapi.CmdRequest] {
		t.Error("request handler not registered")
	}
	if !handlers[httpapi.CmdRequestBatch] {
		t.Error("batch request handler not registered")
	}
}

func TestRequestBatchHandler(t *testing.T) {
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.Header().Set("X-Path", r.URL.Path)
		w.Write([]byte("ok"))
	}))
	defer ts.Close()

	h := NewRequestBatchHandler()
	var resp httpapi.BatchResponse

	err := h.Handle(context.Background(), &httpapi.RequestBatchCmd{
		Requests: []*httpapi.RequestCmd{
			{Method: "GET", URL: ts.URL + "/one"},
			{Method: "GET", URL: ts.URL + "/two"},
			{Method: "GET", URL: ts.URL + "/three"},
		},
	}, func(data any) {
		resp = data.(httpapi.BatchResponse)
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
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
}

func TestRequestBatchHandlerEmpty(t *testing.T) {
	h := NewRequestBatchHandler()
	var resp httpapi.BatchResponse

	err := h.Handle(context.Background(), &httpapi.RequestBatchCmd{
		Requests: nil,
	}, func(data any) {
		resp = data.(httpapi.BatchResponse)
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Responses) != 0 {
		t.Errorf("expected 0 responses, got %d", len(resp.Responses))
	}
}

func TestRequestBatchHandlerPartialFailure(t *testing.T) {
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.Write([]byte("ok"))
	}))
	defer ts.Close()

	h := NewRequestBatchHandler()
	var resp httpapi.BatchResponse

	err := h.Handle(context.Background(), &httpapi.RequestBatchCmd{
		Requests: []*httpapi.RequestCmd{
			{Method: "GET", URL: ts.URL},
			{Method: "GET", URL: "http://localhost:59998", Timeout: 50 * time.Millisecond},
			{Method: "GET", URL: ts.URL},
		},
	}, func(data any) {
		resp = data.(httpapi.BatchResponse)
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
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
}

func TestRequestBatchHandlerConcurrent(t *testing.T) {
	var reqCount atomic.Int64
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		reqCount.Add(1)
		time.Sleep(20 * time.Millisecond)
		w.Write([]byte("ok"))
	}))
	defer ts.Close()

	h := NewRequestBatchHandler()
	var resp httpapi.BatchResponse

	requests := make([]*httpapi.RequestCmd, 10)
	for i := range requests {
		requests[i] = &httpapi.RequestCmd{Method: "GET", URL: ts.URL}
	}

	start := time.Now()
	err := h.Handle(context.Background(), &httpapi.RequestBatchCmd{
		Requests: requests,
	}, func(data any) {
		resp = data.(httpapi.BatchResponse)
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Responses) != 10 {
		t.Fatalf("expected 10 responses, got %d", len(resp.Responses))
	}

	// All requests should run concurrently, so total time should be ~20-50ms, not 200ms
	if elapsed > 150*time.Millisecond {
		t.Errorf("batch not concurrent: took %v", elapsed)
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

func BenchmarkRequestHandler(b *testing.B) {
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.WriteHeader(gohttp.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer ts.Close()

	h := NewRequestHandler()
	ctx := context.Background()
	cmd := &httpapi.RequestCmd{Method: "GET", URL: ts.URL}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			h.Handle(ctx, cmd, func(data any) {})
		}
	})
}

func BenchmarkRequestHandlerWithTimeout(b *testing.B) {
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.WriteHeader(gohttp.StatusOK)
	}))
	defer ts.Close()

	h := NewRequestHandler()
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			cmd := &httpapi.RequestCmd{
				Method:  "GET",
				URL:     ts.URL,
				Timeout: 5 * time.Second,
			}
			h.Handle(ctx, cmd, func(data any) {})
		}
	})
}
