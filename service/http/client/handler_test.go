// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	gohttp "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	httpapi "github.com/wippyai/runtime/api/service/http"
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
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		w.Header().Set("X-Custom", "test")
		gohttp.SetCookie(w, &gohttp.Cookie{Name: "session", Value: "abc123"})
		w.WriteHeader(gohttp.StatusOK)
		_, _ = w.Write([]byte("hello"))
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
		if len(resp.Headers["X-Custom"]) == 0 || resp.Headers["X-Custom"][0] != "test" {
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
		Headers: map[string][]string{"Content-Type": {"application/json"}},
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
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, _ *gohttp.Request) {
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
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, _ *gohttp.Request) {
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
	}, 0, &testReceiver{fn: func(_ any) {
		emitted <- true
	}})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// When context is canceled, emit should NOT be called to avoid races
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
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		reqCount.Add(1)
		w.WriteHeader(gohttp.StatusOK)
		_, _ = w.Write([]byte("ok"))
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
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		data := make([]byte, size)
		for i := range data {
			data[i] = byte(i % 256)
		}
		_, _ = w.Write(data)
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

	d.RegisterAll(func(id dispatcher.CommandID, _ dispatcher.Handler) {
		handlers[id] = true
	})

	if !handlers[httpapi.Request] {
		t.Error("request handler not registered")
	}
	if !handlers[httpapi.RequestBatch] {
		t.Error("batch request handler not registered")
	}
}

func TestDispatcher_RequestBatch(t *testing.T) {
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		w.Header().Set("X-Path", r.URL.Path)
		_, _ = w.Write([]byte("ok"))
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
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		_, _ = w.Write([]byte("ok"))
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
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		reqCount.Add(1)
		time.Sleep(20 * time.Millisecond)
		_, _ = w.Write([]byte("ok"))
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

func TestDispatcher_RequestFileUpload(t *testing.T) {
	var receivedContentType string
	var receivedFiles = make(map[string][]byte)

	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		err := r.ParseMultipartForm(10 << 20)
		if err != nil {
			w.WriteHeader(gohttp.StatusBadRequest)
			return
		}
		for name, files := range r.MultipartForm.File {
			for _, fh := range files {
				f, _ := fh.Open()
				data, _ := io.ReadAll(f)
				_ = f.Close()
				receivedFiles[name] = data
			}
		}
		w.WriteHeader(gohttp.StatusOK)
	}))
	defer ts.Close()

	d := NewDispatcher()
	done := make(chan httpapi.Response, 1)

	err := d.handleRequest(context.Background(), &httpapi.RequestCmd{
		Method: "POST",
		URL:    ts.URL,
		Files: []httpapi.FileUpload{
			{FieldName: "document", FileName: "test.txt", Data: []byte("hello world")},
		},
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
		if !strings.HasPrefix(receivedContentType, "multipart/form-data") {
			t.Errorf("expected multipart content type, got %s", receivedContentType)
		}
		if string(receivedFiles["document"]) != "hello world" {
			t.Errorf("file content mismatch: %s", receivedFiles["document"])
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestDispatcher_RequestMultipleFilesWithForm(t *testing.T) {
	var receivedForm = make(map[string]string)
	var receivedFiles = make(map[string][]byte)

	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		_ = r.ParseMultipartForm(10 << 20)
		for k, v := range r.MultipartForm.Value {
			if len(v) > 0 {
				receivedForm[k] = v[0]
			}
		}
		for name, files := range r.MultipartForm.File {
			for _, fh := range files {
				f, _ := fh.Open()
				data, _ := io.ReadAll(f)
				_ = f.Close()
				receivedFiles[name] = data
			}
		}
		w.WriteHeader(gohttp.StatusOK)
	}))
	defer ts.Close()

	d := NewDispatcher()
	done := make(chan httpapi.Response, 1)

	err := d.handleRequest(context.Background(), &httpapi.RequestCmd{
		Method: "POST",
		URL:    ts.URL,
		Form:   map[string]string{"title": "My Upload", "description": "Test files"},
		Files: []httpapi.FileUpload{
			{FieldName: "file1", FileName: "doc1.txt", Data: []byte("first file")},
			{FieldName: "file2", FileName: "doc2.txt", Data: []byte("second file")},
		},
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
		if receivedForm["title"] != "My Upload" {
			t.Errorf("form field mismatch: %s", receivedForm["title"])
		}
		if string(receivedFiles["file1"]) != "first file" {
			t.Errorf("file1 content mismatch")
		}
		if string(receivedFiles["file2"]) != "second file" {
			t.Errorf("file2 content mismatch")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestDispatcher_RequestFileUploadMissingFieldName(t *testing.T) {
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		w.WriteHeader(gohttp.StatusOK)
	}))
	defer ts.Close()

	d := NewDispatcher()
	done := make(chan httpapi.Response, 1)

	err := d.handleRequest(context.Background(), &httpapi.RequestCmd{
		Method: "POST",
		URL:    ts.URL,
		Files: []httpapi.FileUpload{
			{FieldName: "", FileName: "test.txt", Data: []byte("data")},
		},
	}, 0, &testReceiver{fn: func(data any) {
		done <- data.(httpapi.Response)
	}})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case resp := <-done:
		if resp.Error == "" {
			t.Error("expected error for empty field name")
		}
		if !strings.Contains(resp.Error, "field name required") {
			t.Errorf("unexpected error: %s", resp.Error)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

// generateTestCerts creates a self-signed CA and a client certificate signed by that CA.
func generateTestCerts(t *testing.T) (caCertPEM, clientCertPEM, clientKeyPEM []byte) {
	t.Helper()

	// Generate CA key and cert
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}

	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create CA cert: %v", err)
	}

	caCertPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertDER})

	// Generate client key and cert signed by CA
	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate client key: %v", err)
	}

	clientTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "Test Client"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	clientCertDER, err := x509.CreateCertificate(rand.Reader, clientTemplate, caTemplate, &clientKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create client cert: %v", err)
	}

	clientCertPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientCertDER})

	clientKeyDER, err := x509.MarshalECPrivateKey(clientKey)
	if err != nil {
		t.Fatalf("marshal client key: %v", err)
	}
	clientKeyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: clientKeyDER})

	return caCertPEM, clientCertPEM, clientKeyPEM
}

// newMTLSServer creates an httptest server that requires client certificates signed by the given CA.
func newMTLSServer(t *testing.T, caCertPEM []byte, handler gohttp.Handler) *httptest.Server {
	t.Helper()

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCertPEM) {
		t.Fatal("failed to add CA cert to pool")
	}

	ts := httptest.NewUnstartedServer(handler)
	ts.TLS = &tls.Config{
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  caPool,
		MinVersion: tls.VersionTLS12,
	}
	ts.StartTLS()
	return ts
}

func TestClientPoolTLSFingerprint(t *testing.T) {
	cfg1 := &httpapi.TLSConfig{CertPEM: []byte("cert1"), KeyPEM: []byte("key1")}
	cfg2 := &httpapi.TLSConfig{CertPEM: []byte("cert1"), KeyPEM: []byte("key1")}
	cfg3 := &httpapi.TLSConfig{CertPEM: []byte("cert2"), KeyPEM: []byte("key2")}

	fp1 := tlsFingerprint(cfg1)
	fp2 := tlsFingerprint(cfg2)
	fp3 := tlsFingerprint(cfg3)

	if fp1 != fp2 {
		t.Error("identical configs produce different fingerprints")
	}
	if fp1 == fp3 {
		t.Error("different configs produce same fingerprint")
	}
	if len(fp1) != 64 {
		t.Errorf("expected 64-char hex fingerprint, got %d", len(fp1))
	}
}

func TestClientPoolTLSFingerprintIncludesAllFields(t *testing.T) {
	base := &httpapi.TLSConfig{
		CertPEM:    []byte("cert"),
		KeyPEM:     []byte("key"),
		CAPEM:      []byte("ca"),
		ServerName: "example.com",
	}
	withInsecure := &httpapi.TLSConfig{
		CertPEM:            []byte("cert"),
		KeyPEM:             []byte("key"),
		CAPEM:              []byte("ca"),
		ServerName:         "example.com",
		InsecureSkipVerify: true,
	}
	withDifferentSNI := &httpapi.TLSConfig{
		CertPEM:    []byte("cert"),
		KeyPEM:     []byte("key"),
		CAPEM:      []byte("ca"),
		ServerName: "other.com",
	}

	fpBase := tlsFingerprint(base)
	fpInsecure := tlsFingerprint(withInsecure)
	fpSNI := tlsFingerprint(withDifferentSNI)

	if fpBase == fpInsecure {
		t.Error("insecure_skip_verify should change fingerprint")
	}
	if fpBase == fpSNI {
		t.Error("different server_name should change fingerprint")
	}
}

func TestClientPoolTLSClientReuse(t *testing.T) {
	caCert, clientCert, clientKey := generateTestCerts(t)
	pool := NewClientPool()

	cfg := &httpapi.TLSConfig{CertPEM: clientCert, KeyPEM: clientKey, CAPEM: caCert}
	c1, err := pool.GetClientWithTLS(0, "", cfg)
	if err != nil {
		t.Fatalf("GetClientWithTLS: %v", err)
	}

	c2, err := pool.GetClientWithTLS(0, "", cfg)
	if err != nil {
		t.Fatalf("GetClientWithTLS: %v", err)
	}

	if c1 != c2 {
		t.Error("same TLS config should reuse pooled client")
	}
}

func TestClientPoolTLSDifferentConfigs(t *testing.T) {
	caCert, clientCert, clientKey := generateTestCerts(t)
	_, clientCert2, clientKey2 := generateTestCerts(t)
	pool := NewClientPool()

	cfg1 := &httpapi.TLSConfig{CertPEM: clientCert, KeyPEM: clientKey, CAPEM: caCert}
	cfg2 := &httpapi.TLSConfig{CertPEM: clientCert2, KeyPEM: clientKey2, CAPEM: caCert}

	c1, err := pool.GetClientWithTLS(0, "", cfg1)
	if err != nil {
		t.Fatalf("GetClientWithTLS: %v", err)
	}

	c2, err := pool.GetClientWithTLS(0, "", cfg2)
	if err != nil {
		t.Fatalf("GetClientWithTLS: %v", err)
	}

	if c1 == c2 {
		t.Error("different TLS configs should create separate clients")
	}
}

func TestClientPoolTLSInvalidCert(t *testing.T) {
	pool := NewClientPool()

	cfg := &httpapi.TLSConfig{CertPEM: []byte("bad"), KeyPEM: []byte("bad")}
	_, err := pool.GetClientWithTLS(0, "", cfg)
	if err == nil {
		t.Error("expected error for invalid cert/key pair")
	}
}

func TestClientPoolTLSNilConfig(t *testing.T) {
	pool := NewClientPool()
	c1, err := pool.GetClientWithTLS(0, "", nil)
	if err != nil {
		t.Fatalf("GetClientWithTLS(nil): %v", err)
	}
	c2 := pool.GetClient(0, "")
	if c1 != c2 {
		t.Error("nil TLS config should return default client")
	}
}

func TestBuildTLSConfigClientCert(t *testing.T) {
	_, clientCert, clientKey := generateTestCerts(t)
	cfg, err := buildTLSConfig(&httpapi.TLSConfig{CertPEM: clientCert, KeyPEM: clientKey})
	if err != nil {
		t.Fatalf("buildTLSConfig: %v", err)
	}
	if len(cfg.Certificates) != 1 {
		t.Errorf("expected 1 certificate, got %d", len(cfg.Certificates))
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Error("expected MinVersion TLS 1.2")
	}
}

func TestBuildTLSConfigCA(t *testing.T) {
	caCert, _, _ := generateTestCerts(t)
	cfg, err := buildTLSConfig(&httpapi.TLSConfig{CAPEM: caCert})
	if err != nil {
		t.Fatalf("buildTLSConfig: %v", err)
	}
	if cfg.RootCAs == nil {
		t.Error("expected RootCAs to be set")
	}
}

func TestBuildTLSConfigInvalidCA(t *testing.T) {
	_, err := buildTLSConfig(&httpapi.TLSConfig{CAPEM: []byte("not a cert")})
	if err == nil {
		t.Error("expected error for invalid CA PEM")
	}
}

func TestBuildTLSConfigServerName(t *testing.T) {
	cfg, err := buildTLSConfig(&httpapi.TLSConfig{ServerName: "example.com"})
	if err != nil {
		t.Fatalf("buildTLSConfig: %v", err)
	}
	if cfg.ServerName != "example.com" {
		t.Errorf("expected ServerName 'example.com', got %q", cfg.ServerName)
	}
}

func TestBuildTLSConfigCertWithoutKey(t *testing.T) {
	_, err := buildTLSConfig(&httpapi.TLSConfig{CertPEM: []byte("cert-data")})
	if err == nil {
		t.Error("expected error for cert without key")
	}
	if !strings.Contains(err.Error(), "both cert and key must be provided") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBuildTLSConfigKeyWithoutCert(t *testing.T) {
	_, err := buildTLSConfig(&httpapi.TLSConfig{KeyPEM: []byte("key-data")})
	if err == nil {
		t.Error("expected error for key without cert")
	}
	if !strings.Contains(err.Error(), "both cert and key must be provided") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBuildTLSConfigInsecure(t *testing.T) {
	cfg, err := buildTLSConfig(&httpapi.TLSConfig{InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("buildTLSConfig: %v", err)
	}
	if !cfg.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify true")
	}
}

func TestDispatcher_RequestMTLS(t *testing.T) {
	caCert, clientCert, clientKey := generateTestCerts(t)

	ts := newMTLSServer(t, caCert, gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			w.WriteHeader(gohttp.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte("mtls-ok"))
	}))
	defer ts.Close()

	// Server uses self-signed cert, client needs server's CA too
	serverCACert := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: ts.TLS.Certificates[0].Certificate[0],
	})

	d := NewDispatcher()
	done := make(chan httpapi.Response, 1)

	err := d.handleRequest(context.Background(), &httpapi.RequestCmd{
		Method: "GET",
		URL:    ts.URL,
		TLS: &httpapi.TLSConfig{
			CertPEM: clientCert,
			KeyPEM:  clientKey,
			CAPEM:   serverCACert,
		},
	}, 0, &testReceiver{fn: func(data any) {
		done <- data.(httpapi.Response)
	}})

	if err != nil {
		t.Fatalf("handleRequest: %v", err)
	}

	select {
	case resp := <-done:
		if resp.Error != "" {
			t.Fatalf("response error: %s", resp.Error)
		}
		if resp.StatusCode != 200 {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
		if string(resp.Body) != "mtls-ok" {
			t.Errorf("expected 'mtls-ok', got %q", resp.Body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestDispatcher_RequestTLSCustomCA(t *testing.T) {
	// Use a regular TLS server (no client cert required)
	ts := httptest.NewTLSServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		_, _ = w.Write([]byte("tls-ok"))
	}))
	defer ts.Close()

	serverCACert := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: ts.TLS.Certificates[0].Certificate[0],
	})

	d := NewDispatcher()
	done := make(chan httpapi.Response, 1)

	err := d.handleRequest(context.Background(), &httpapi.RequestCmd{
		Method: "GET",
		URL:    ts.URL,
		TLS: &httpapi.TLSConfig{
			CAPEM: serverCACert,
		},
	}, 0, &testReceiver{fn: func(data any) {
		done <- data.(httpapi.Response)
	}})

	if err != nil {
		t.Fatalf("handleRequest: %v", err)
	}

	select {
	case resp := <-done:
		if resp.Error != "" {
			t.Fatalf("response error: %s", resp.Error)
		}
		if resp.StatusCode != 200 {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
		if string(resp.Body) != "tls-ok" {
			t.Errorf("expected 'tls-ok', got %q", resp.Body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestDispatcher_RequestTLSInvalidCert(t *testing.T) {
	d := NewDispatcher()
	done := make(chan httpapi.Response, 1)

	err := d.handleRequest(context.Background(), &httpapi.RequestCmd{
		Method: "GET",
		URL:    "https://example.com",
		TLS: &httpapi.TLSConfig{
			CertPEM: []byte("bad-cert"),
			KeyPEM:  []byte("bad-key"),
		},
	}, 0, &testReceiver{fn: func(data any) {
		done <- data.(httpapi.Response)
	}})

	if err != nil {
		t.Fatalf("handleRequest: %v", err)
	}

	select {
	case resp := <-done:
		if resp.Error == "" {
			t.Error("expected error for invalid cert")
		}
		if !strings.Contains(resp.Error, "parse client certificate") {
			t.Errorf("expected cert parse error, got: %s", resp.Error)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestDispatcher_RequestBatchMTLS(t *testing.T) {
	caCert, clientCert, clientKey := generateTestCerts(t)

	ts := newMTLSServer(t, caCert, gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		_, _ = w.Write([]byte(r.URL.Path))
	}))
	defer ts.Close()

	serverCACert := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: ts.TLS.Certificates[0].Certificate[0],
	})

	tlsCfg := &httpapi.TLSConfig{
		CertPEM: clientCert,
		KeyPEM:  clientKey,
		CAPEM:   serverCACert,
	}

	d := NewDispatcher()
	done := make(chan httpapi.BatchResponse, 1)

	err := d.handleRequestBatch(context.Background(), &httpapi.RequestBatchCmd{
		Requests: []*httpapi.RequestCmd{
			{Method: "GET", URL: ts.URL + "/a", TLS: tlsCfg},
			{Method: "GET", URL: ts.URL + "/b", TLS: tlsCfg},
		},
	}, 0, &testReceiver{fn: func(data any) {
		done <- data.(httpapi.BatchResponse)
	}})

	if err != nil {
		t.Fatalf("handleRequestBatch: %v", err)
	}

	select {
	case resp := <-done:
		if len(resp.Responses) != 2 {
			t.Fatalf("expected 2 responses, got %d", len(resp.Responses))
		}
		for i, r := range resp.Responses {
			if r.Error != "" {
				t.Errorf("response %d error: %s", i, r.Error)
			}
			if r.StatusCode != 200 {
				t.Errorf("response %d status: %d", i, r.StatusCode)
			}
		}
		if string(resp.Responses[0].Body) != "/a" {
			t.Errorf("expected '/a', got %q", resp.Responses[0].Body)
		}
		if string(resp.Responses[1].Body) != "/b" {
			t.Errorf("expected '/b', got %q", resp.Responses[1].Body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestDispatcher_RequestTLSInsecureSkipVerify(t *testing.T) {
	ts := httptest.NewTLSServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		_, _ = w.Write([]byte("insecure-ok"))
	}))
	defer ts.Close()

	d := NewDispatcher()
	done := make(chan httpapi.Response, 1)

	err := d.handleRequest(context.Background(), &httpapi.RequestCmd{
		Method: "GET",
		URL:    ts.URL,
		TLS: &httpapi.TLSConfig{
			InsecureSkipVerify: true,
		},
	}, 0, &testReceiver{fn: func(data any) {
		done <- data.(httpapi.Response)
	}})

	if err != nil {
		t.Fatalf("handleRequest: %v", err)
	}

	select {
	case resp := <-done:
		if resp.Error != "" {
			t.Fatalf("response error: %s", resp.Error)
		}
		if resp.StatusCode != 200 {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
		if string(resp.Body) != "insecure-ok" {
			t.Errorf("expected 'insecure-ok', got %q", resp.Body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestClientPoolTLSConcurrentAccess(t *testing.T) {
	caCert, clientCert, clientKey := generateTestCerts(t)
	pool := NewClientPool()
	cfg := &httpapi.TLSConfig{CertPEM: clientCert, KeyPEM: clientKey, CAPEM: caCert}

	var wg sync.WaitGroup
	const goroutines = 50
	clients := make([]*gohttp.Client, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			c, err := pool.GetClientWithTLS(0, "", cfg)
			if err != nil {
				t.Errorf("goroutine %d: %v", idx, err)
				return
			}
			clients[idx] = c
		}(i)
	}

	wg.Wait()

	for i := 1; i < goroutines; i++ {
		if clients[i] != clients[0] {
			t.Errorf("goroutine %d got different client", i)
		}
	}
}

func TestClientPoolTLSDoesNotAffectNonTLS(t *testing.T) {
	caCert, clientCert, clientKey := generateTestCerts(t)
	pool := NewClientPool()

	defaultClient := pool.GetClient(0, "")
	cfg := &httpapi.TLSConfig{CertPEM: clientCert, KeyPEM: clientKey, CAPEM: caCert}
	tlsClient, err := pool.GetClientWithTLS(0, "", cfg)
	if err != nil {
		t.Fatalf("GetClientWithTLS: %v", err)
	}
	defaultAfter := pool.GetClient(0, "")

	if defaultClient != defaultAfter {
		t.Error("TLS client creation changed the default client")
	}
	if defaultClient == tlsClient {
		t.Error("TLS client should differ from default client")
	}
}

func TestDispatcher_RequestTLSCertWithoutKey(t *testing.T) {
	d := NewDispatcher()
	done := make(chan httpapi.Response, 1)

	err := d.handleRequest(context.Background(), &httpapi.RequestCmd{
		Method: "GET",
		URL:    "https://example.com",
		TLS: &httpapi.TLSConfig{
			CertPEM: []byte("-----BEGIN CERTIFICATE-----\ncert\n-----END CERTIFICATE-----"),
		},
	}, 0, &testReceiver{fn: func(data any) {
		done <- data.(httpapi.Response)
	}})

	if err != nil {
		t.Fatalf("handleRequest: %v", err)
	}

	select {
	case resp := <-done:
		if resp.Error == "" {
			t.Error("expected error for cert without key")
		}
		if !strings.Contains(resp.Error, "both cert and key must be provided") {
			t.Errorf("unexpected error: %s", resp.Error)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestDispatcher_RequestMTLSWrongClientCert(t *testing.T) {
	caCert, _, _ := generateTestCerts(t)
	_, wrongCert, wrongKey := generateTestCerts(t)

	ts := newMTLSServer(t, caCert, gohttp.HandlerFunc(func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		_, _ = w.Write([]byte("should not reach"))
	}))
	defer ts.Close()

	serverCACert := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: ts.TLS.Certificates[0].Certificate[0],
	})

	d := NewDispatcher()
	done := make(chan httpapi.Response, 1)

	err := d.handleRequest(context.Background(), &httpapi.RequestCmd{
		Method: "GET",
		URL:    ts.URL,
		TLS: &httpapi.TLSConfig{
			CertPEM: wrongCert,
			KeyPEM:  wrongKey,
			CAPEM:   serverCACert,
		},
	}, 0, &testReceiver{fn: func(data any) {
		done <- data.(httpapi.Response)
	}})

	if err != nil {
		t.Fatalf("handleRequest: %v", err)
	}

	select {
	case resp := <-done:
		if resp.Error == "" {
			t.Error("expected TLS handshake error for wrong client cert")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestDispatcher_RequestTLSWithoutCA(t *testing.T) {
	ts := httptest.NewTLSServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		_, _ = w.Write([]byte("should not reach"))
	}))
	defer ts.Close()

	d := NewDispatcher()
	done := make(chan httpapi.Response, 1)

	err := d.handleRequest(context.Background(), &httpapi.RequestCmd{
		Method: "GET",
		URL:    ts.URL,
		TLS: &httpapi.TLSConfig{
			ServerName: "127.0.0.1",
		},
	}, 0, &testReceiver{fn: func(data any) {
		done <- data.(httpapi.Response)
	}})

	if err != nil {
		t.Fatalf("handleRequest: %v", err)
	}

	select {
	case resp := <-done:
		if resp.Error == "" {
			t.Error("expected cert verification error without CA")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestDispatcher_RequestTLSServerName(t *testing.T) {
	caCert, clientCert, clientKey := generateTestCerts(t)

	ts := newMTLSServer(t, caCert, gohttp.HandlerFunc(func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		_, _ = w.Write([]byte("sni-ok"))
	}))
	defer ts.Close()

	serverCACert := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: ts.TLS.Certificates[0].Certificate[0],
	})

	d := NewDispatcher()
	done := make(chan httpapi.Response, 1)

	err := d.handleRequest(context.Background(), &httpapi.RequestCmd{
		Method: "GET",
		URL:    ts.URL,
		TLS: &httpapi.TLSConfig{
			CertPEM:            clientCert,
			KeyPEM:             clientKey,
			CAPEM:              serverCACert,
			InsecureSkipVerify: true,
			ServerName:         "custom-sni",
		},
	}, 0, &testReceiver{fn: func(data any) {
		done <- data.(httpapi.Response)
	}})

	if err != nil {
		t.Fatalf("handleRequest: %v", err)
	}

	select {
	case resp := <-done:
		if resp.Error != "" {
			t.Fatalf("response error: %s", resp.Error)
		}
		if string(resp.Body) != "sni-ok" {
			t.Errorf("expected 'sni-ok', got %q", resp.Body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestDispatcher_RequestNoTLSUnchanged(t *testing.T) {
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		_, _ = w.Write([]byte("plain-ok"))
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
		t.Fatalf("handleRequest: %v", err)
	}

	select {
	case resp := <-done:
		if resp.Error != "" {
			t.Fatalf("response error: %s", resp.Error)
		}
		if string(resp.Body) != "plain-ok" {
			t.Errorf("expected 'plain-ok', got %q", resp.Body)
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
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		w.WriteHeader(gohttp.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer ts.Close()

	d := NewDispatcher()
	ctx := context.Background()
	cmd := &httpapi.RequestCmd{Method: "GET", URL: ts.URL}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			done := make(chan struct{})
			_ = d.handleRequest(ctx, cmd, 0, &testReceiver{fn: func(_ any) {
				close(done)
			}})
			<-done
		}
	})
}

func BenchmarkDispatcher_RequestWithTimeout(b *testing.B) {
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, _ *gohttp.Request) {
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
			_ = d.handleRequest(ctx, cmd, 0, &testReceiver{fn: func(_ any) {
				close(done)
			}})
			<-done
		}
	})
}
