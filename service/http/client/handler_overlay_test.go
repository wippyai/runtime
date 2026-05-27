// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"fmt"
	"net"
	gohttp "net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/registry"
	secapi "github.com/wippyai/runtime/api/security"
	httpapi "github.com/wippyai/runtime/api/service/http"
)

// overlayTestCtx returns a context that allows private IP access.
// This is needed because test servers bind to 127.0.0.1 and the SSRF
// protection (checkOverlayPrivateIP) blocks private IPs by default.
func overlayTestCtx() context.Context {
	ctx := ctxapi.NewRootContext()
	secapi.SetStrictMode(ctx, false)
	return ctx
}

// --- Mock overlay network types ---

// dialRecord captures a single DialContext call.
type dialRecord struct {
	Network string
	Address string
}

// recordingService implements netapi.Service and records all DialContext calls.
// It dials the real destination (allowing HTTP requests to succeed) while recording.
type recordingService struct {
	calls []dialRecord
	mu    sync.Mutex
}

func (s *recordingService) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	s.mu.Lock()
	s.calls = append(s.calls, dialRecord{Network: network, Address: address})
	s.mu.Unlock()

	// Actually dial the destination so HTTP requests work
	var d net.Dialer
	return d.DialContext(ctx, network, address)
}

func (s *recordingService) Listen(_ context.Context, _, _ string) (net.Listener, error) {
	return nil, fmt.Errorf("not supported")
}
func (s *recordingService) ListenPacket(_ context.Context, _, _ string) (net.PacketConn, error) {
	return nil, fmt.Errorf("not supported")
}
func (s *recordingService) LookupHost(_ context.Context, _ string) ([]string, error) {
	return nil, fmt.Errorf("not supported")
}

func (s *recordingService) dialCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

// failingService implements netapi.Service but always returns an error on dial.
type failingService struct {
	err error
}

func (s *failingService) DialContext(_ context.Context, _, _ string) (net.Conn, error) {
	return nil, s.err
}
func (s *failingService) Listen(_ context.Context, _, _ string) (net.Listener, error) {
	return nil, s.err
}
func (s *failingService) ListenPacket(_ context.Context, _, _ string) (net.PacketConn, error) {
	return nil, s.err
}
func (s *failingService) LookupHost(_ context.Context, _ string) ([]string, error) {
	return nil, s.err
}

// mockNetworkRegistry implements netapi.NetworkRegistry for testing.
type mockNetworkRegistry struct {
	services map[string]netapi.Service
	kinds    map[string]registry.Kind
}

func newMockNetworkRegistry() *mockNetworkRegistry {
	return &mockNetworkRegistry{
		services: make(map[string]netapi.Service),
		kinds:    make(map[string]registry.Kind),
	}
}

func (r *mockNetworkRegistry) register(id string, svc netapi.Service, kind registry.Kind) {
	r.services[id] = svc
	r.kinds[id] = kind
}

func (r *mockNetworkRegistry) GetNetwork(id registry.ID) (netapi.Service, error) {
	svc, ok := r.services[id.String()]
	if !ok {
		return nil, netapi.ErrNetworkNotFound
	}
	return svc, nil
}

func (r *mockNetworkRegistry) HasNetwork(id registry.ID) bool {
	_, ok := r.services[id.String()]
	return ok
}

func (r *mockNetworkRegistry) NetworkKind(id registry.ID) registry.Kind {
	return r.kinds[id.String()]
}

// --- Overlay routing tests ---

func TestHandler_OverlayNetworkRouting(t *testing.T) {
	// Test server that returns a known response
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		w.WriteHeader(gohttp.StatusOK)
		w.Write([]byte("overlay-routed"))
	}))
	defer ts.Close()

	// Create a recording overlay service
	overlaySvc := &recordingService{}
	reg := newMockNetworkRegistry()
	reg.register("network:test-overlay", overlaySvc, netapi.KindSOCKS5)

	d := NewDispatcher(WithNetworkRegistry(reg))
	done := make(chan httpapi.Response, 1)

	err := d.handleRequest(overlayTestCtx(), &httpapi.RequestCmd{
		Method:         "GET",
		URL:            ts.URL,
		OverlayNetwork: "network:test-overlay",
	}, 0, &testReceiver{fn: func(data any) {
		done <- data.(httpapi.Response)
	}})
	require.NoError(t, err)

	select {
	case resp := <-done:
		require.Empty(t, resp.Error, "request should succeed through overlay")
		assert.Equal(t, 200, resp.StatusCode)
		assert.Equal(t, "overlay-routed", string(resp.Body))

		// Verify the overlay service was used (DialContext was called)
		assert.Greater(t, overlaySvc.dialCount(), 0, "overlay DialContext should have been called")
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for response")
	}
}

func TestHandler_OverlayNetworkNotFound(t *testing.T) {
	// Empty registry — overlay ID won't be found
	reg := newMockNetworkRegistry()
	d := NewDispatcher(WithNetworkRegistry(reg))
	done := make(chan httpapi.Response, 1)

	err := d.handleRequest(overlayTestCtx(), &httpapi.RequestCmd{
		Method:         "GET",
		URL:            "http://example.com/",
		OverlayNetwork: "network:nonexistent",
	}, 0, &testReceiver{fn: func(data any) {
		done <- data.(httpapi.Response)
	}})
	require.NoError(t, err)

	select {
	case resp := <-done:
		require.NotEmpty(t, resp.Error)
		assert.Contains(t, resp.Error, "overlay network")
		assert.Contains(t, resp.Error, "nonexistent")
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestHandler_ClearnetWhenNoOverlay(t *testing.T) {
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		w.WriteHeader(gohttp.StatusOK)
		w.Write([]byte("clearnet"))
	}))
	defer ts.Close()

	// Create overlay but DON'T specify it in the request
	overlaySvc := &recordingService{}
	reg := newMockNetworkRegistry()
	reg.register("network:unused-overlay", overlaySvc, netapi.KindSOCKS5)

	d := NewDispatcher(WithNetworkRegistry(reg))
	done := make(chan httpapi.Response, 1)

	// No OverlayNetwork set — should use clearnet
	err := d.handleRequest(context.Background(), &httpapi.RequestCmd{
		Method: "GET",
		URL:    ts.URL,
	}, 0, &testReceiver{fn: func(data any) {
		done <- data.(httpapi.Response)
	}})
	require.NoError(t, err)

	select {
	case resp := <-done:
		require.Empty(t, resp.Error)
		assert.Equal(t, 200, resp.StatusCode)
		assert.Equal(t, "clearnet", string(resp.Body))

		// Overlay should NOT have been used
		assert.Equal(t, 0, overlaySvc.dialCount(), "overlay should not be used for clearnet requests")
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestHandler_OverlayWithNoRegistry(t *testing.T) {
	// Dispatcher without network registry — an explicit overlay request must
	// fail rather than silently fall back to clearnet, which would leak DNS
	// and the target IP to the local network.
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		w.WriteHeader(gohttp.StatusOK)
		w.Write([]byte("no-registry"))
	}))
	defer ts.Close()

	d := NewDispatcher() // no WithNetworkRegistry
	done := make(chan httpapi.Response, 1)

	err := d.handleRequest(context.Background(), &httpapi.RequestCmd{
		Method:         "GET",
		URL:            ts.URL,
		OverlayNetwork: "network:some-overlay",
	}, 0, &testReceiver{fn: func(data any) {
		done <- data.(httpapi.Response)
	}})
	require.NoError(t, err)

	select {
	case resp := <-done:
		require.NotEmpty(t, resp.Error, "overlay without registry must error, not fall back to clearnet")
		assert.Contains(t, resp.Error, "network registry is not configured")
		assert.Equal(t, 0, resp.StatusCode)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestHandler_OverlaySSRFProtection_PrivateIP(t *testing.T) {
	reg := newMockNetworkRegistry()
	overlaySvc := &recordingService{}
	reg.register("network:test-overlay", overlaySvc, netapi.KindSOCKS5)

	d := NewDispatcher(WithNetworkRegistry(reg))

	// Test various private IP addresses that should be blocked through overlay
	privateURLs := []string{
		"http://127.0.0.1:8080/secret",
		"http://10.0.0.1:80/internal",
		"http://192.168.1.1/admin",
		"http://172.16.0.1/api",
		"http://[::1]:8080/local",
		"http://0.0.0.0/",
	}

	for _, privateURL := range privateURLs {
		t.Run(privateURL, func(t *testing.T) {
			done := make(chan httpapi.Response, 1)

			err := d.handleRequest(context.Background(), &httpapi.RequestCmd{
				Method:         "GET",
				URL:            privateURL,
				OverlayNetwork: "network:test-overlay",
			}, 0, &testReceiver{fn: func(data any) {
				done <- data.(httpapi.Response)
			}})
			require.NoError(t, err)

			select {
			case resp := <-done:
				require.NotEmpty(t, resp.Error, "private IP %s should be blocked through overlay", privateURL)
				assert.Contains(t, resp.Error, "private IP")
			case <-time.After(5 * time.Second):
				t.Fatalf("timeout for %s", privateURL)
			}
		})
	}

	// Overlay should NOT have been called for any private IP
	assert.Equal(t, 0, overlaySvc.dialCount(), "overlay should never dial private IPs")
}

func TestHandler_OverlayDialError(t *testing.T) {
	// Overlay that always fails to connect
	reg := newMockNetworkRegistry()
	reg.register("network:broken-overlay", &failingService{err: fmt.Errorf("overlay dial failed")}, netapi.KindSOCKS5)

	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		w.WriteHeader(gohttp.StatusOK)
	}))
	defer ts.Close()

	d := NewDispatcher(WithNetworkRegistry(reg))
	done := make(chan httpapi.Response, 1)

	err := d.handleRequest(overlayTestCtx(), &httpapi.RequestCmd{
		Method:         "GET",
		URL:            ts.URL,
		OverlayNetwork: "network:broken-overlay",
	}, 0, &testReceiver{fn: func(data any) {
		done <- data.(httpapi.Response)
	}})
	require.NoError(t, err)

	select {
	case resp := <-done:
		require.NotEmpty(t, resp.Error, "should report overlay dial failure")
		assert.Contains(t, resp.Error, "overlay dial failed")
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestHandler_OverlayBatchMixedRouting(t *testing.T) {
	// Test batch requests where some use overlay and some use clearnet
	var clearnetHits, overlayHits atomic.Int32

	tsClearnet := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		clearnetHits.Add(1)
		w.WriteHeader(gohttp.StatusOK)
		w.Write([]byte("clearnet"))
	}))
	defer tsClearnet.Close()

	tsOverlay := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		overlayHits.Add(1)
		w.WriteHeader(gohttp.StatusOK)
		w.Write([]byte("overlay"))
	}))
	defer tsOverlay.Close()

	overlaySvc := &recordingService{}
	reg := newMockNetworkRegistry()
	reg.register("network:test-overlay", overlaySvc, netapi.KindSOCKS5)

	d := NewDispatcher(WithNetworkRegistry(reg))
	done := make(chan httpapi.BatchResponse, 1)

	err := d.handleRequestBatch(overlayTestCtx(), &httpapi.RequestBatchCmd{
		Requests: []*httpapi.RequestCmd{
			{Method: "GET", URL: tsClearnet.URL},                                        // clearnet
			{Method: "GET", URL: tsOverlay.URL, OverlayNetwork: "network:test-overlay"}, // overlay
			{Method: "GET", URL: tsClearnet.URL},                                        // clearnet
			{Method: "GET", URL: tsOverlay.URL, OverlayNetwork: "network:test-overlay"}, // overlay
		},
	}, 0, &testReceiver{fn: func(data any) {
		done <- data.(httpapi.BatchResponse)
	}})
	require.NoError(t, err)

	select {
	case batch := <-done:
		require.Len(t, batch.Responses, 4)

		// All requests should succeed
		for i, resp := range batch.Responses {
			require.Empty(t, resp.Error, "request %d should succeed", i)
			assert.Equal(t, 200, resp.StatusCode, "request %d", i)
		}

		// Clearnet requests
		assert.Equal(t, "clearnet", string(batch.Responses[0].Body))
		assert.Equal(t, "clearnet", string(batch.Responses[2].Body))

		// Overlay requests
		assert.Equal(t, "overlay", string(batch.Responses[1].Body))
		assert.Equal(t, "overlay", string(batch.Responses[3].Body))

		// Overlay service should have been used for 2 requests
		assert.GreaterOrEqual(t, overlaySvc.dialCount(), 1, "overlay should have been called")

	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestHandler_OverlayClientPooling(t *testing.T) {
	// Verify that overlay clients are properly pooled by networkID
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		w.WriteHeader(gohttp.StatusOK)
	}))
	defer ts.Close()

	svc1 := &recordingService{}
	svc2 := &recordingService{}
	reg := newMockNetworkRegistry()
	reg.register("network:overlay-1", svc1, netapi.KindSOCKS5)
	reg.register("network:overlay-2", svc2, netapi.KindSOCKS5)

	d := NewDispatcher(WithNetworkRegistry(reg))

	// Make multiple requests to the same overlay — should reuse the pooled client
	for i := 0; i < 3; i++ {
		done := make(chan httpapi.Response, 1)
		err := d.handleRequest(overlayTestCtx(), &httpapi.RequestCmd{
			Method:         "GET",
			URL:            ts.URL,
			OverlayNetwork: "network:overlay-1",
		}, 0, &testReceiver{fn: func(data any) {
			done <- data.(httpapi.Response)
		}})
		require.NoError(t, err)

		select {
		case resp := <-done:
			require.Empty(t, resp.Error)
		case <-time.After(5 * time.Second):
			t.Fatal("timeout")
		}
	}

	// Make a request through a different overlay
	done := make(chan httpapi.Response, 1)
	err := d.handleRequest(overlayTestCtx(), &httpapi.RequestCmd{
		Method:         "GET",
		URL:            ts.URL,
		OverlayNetwork: "network:overlay-2",
	}, 0, &testReceiver{fn: func(data any) {
		done <- data.(httpapi.Response)
	}})
	require.NoError(t, err)

	select {
	case resp := <-done:
		require.Empty(t, resp.Error)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	// Both overlay services should have been used
	assert.Greater(t, svc1.dialCount(), 0, "overlay-1 should have been called")
	assert.Greater(t, svc2.dialCount(), 0, "overlay-2 should have been called")

	// Pool should have 2 overlay clients
	assert.Equal(t, 2, d.pool.Size(), "pool should contain 2 overlay clients")
}

func TestHandler_OverlayDefaultNetworkFromContext(t *testing.T) {
	// Verify that when req.OverlayNetwork is empty, the handler falls back
	// to netapi.GetDefaultNetwork(ctx).
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		w.WriteHeader(gohttp.StatusOK)
		w.Write([]byte("via-default"))
	}))
	defer ts.Close()

	overlaySvc := &recordingService{}
	reg := newMockNetworkRegistry()
	reg.register("network:ctx-default-overlay", overlaySvc, netapi.KindSOCKS5)

	d := NewDispatcher(WithNetworkRegistry(reg))

	// Create a context with a default network set
	ctx := overlayTestCtx()
	ctx, fc := ctxapi.OpenFrameContext(ctx)
	defer ctxapi.ReleaseFrameContext(fc)
	require.NoError(t, fc.SetMultiple(netapi.DefaultNetworkPair("network:ctx-default-overlay")))

	done := make(chan httpapi.Response, 1)

	// No OverlayNetwork on the request — should use context default
	err := d.handleRequest(ctx, &httpapi.RequestCmd{
		Method: "GET",
		URL:    ts.URL,
		// OverlayNetwork intentionally left empty
	}, 0, &testReceiver{fn: func(data any) {
		done <- data.(httpapi.Response)
	}})
	require.NoError(t, err)

	select {
	case resp := <-done:
		require.Empty(t, resp.Error, "request should succeed through default overlay")
		assert.Equal(t, 200, resp.StatusCode)
		assert.Equal(t, "via-default", string(resp.Body))
		assert.Greater(t, overlaySvc.dialCount(), 0,
			"overlay DialContext should be called via context default network")
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestHandler_OverlayTakesPriorityOverTLS(t *testing.T) {
	// When both OverlayNetwork and TLS config are set, overlay should win.
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		w.WriteHeader(gohttp.StatusOK)
		w.Write([]byte("overlay-wins"))
	}))
	defer ts.Close()

	overlaySvc := &recordingService{}
	reg := newMockNetworkRegistry()
	reg.register("network:prio-overlay", overlaySvc, netapi.KindSOCKS5)

	d := NewDispatcher(WithNetworkRegistry(reg))
	done := make(chan httpapi.Response, 1)

	err := d.handleRequest(overlayTestCtx(), &httpapi.RequestCmd{
		Method:         "GET",
		URL:            ts.URL,
		OverlayNetwork: "network:prio-overlay",
		TLS: &httpapi.TLSConfig{
			InsecureSkipVerify: true,
		},
	}, 0, &testReceiver{fn: func(data any) {
		done <- data.(httpapi.Response)
	}})
	require.NoError(t, err)

	select {
	case resp := <-done:
		require.Empty(t, resp.Error, "request should succeed via overlay, not TLS")
		assert.Equal(t, 200, resp.StatusCode)
		assert.Equal(t, "overlay-wins", string(resp.Body))
		assert.Greater(t, overlaySvc.dialCount(), 0,
			"overlay should be used even when TLS config is also provided")
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestHandler_ExplicitOverlayOverridesDefaultContext(t *testing.T) {
	// When both req.OverlayNetwork and context default are set,
	// the explicit per-request one should take priority.
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		w.WriteHeader(gohttp.StatusOK)
		w.Write([]byte("explicit-overlay"))
	}))
	defer ts.Close()

	ctxOverlay := &recordingService{}
	explicitOverlay := &recordingService{}
	reg := newMockNetworkRegistry()
	reg.register("network:ctx-overlay", ctxOverlay, netapi.KindSOCKS5)
	reg.register("network:explicit-overlay", explicitOverlay, netapi.KindSOCKS5)

	d := NewDispatcher(WithNetworkRegistry(reg))

	// Set a default network in context
	ctx := overlayTestCtx()
	ctx, fc := ctxapi.OpenFrameContext(ctx)
	defer ctxapi.ReleaseFrameContext(fc)
	require.NoError(t, fc.SetMultiple(netapi.DefaultNetworkPair("network:ctx-overlay")))

	done := make(chan httpapi.Response, 1)

	// Explicitly specify a different overlay
	err := d.handleRequest(ctx, &httpapi.RequestCmd{
		Method:         "GET",
		URL:            ts.URL,
		OverlayNetwork: "network:explicit-overlay",
	}, 0, &testReceiver{fn: func(data any) {
		done <- data.(httpapi.Response)
	}})
	require.NoError(t, err)

	select {
	case resp := <-done:
		require.Empty(t, resp.Error)
		assert.Equal(t, 200, resp.StatusCode)
		// Explicit overlay should be used, not the context default
		assert.Greater(t, explicitOverlay.dialCount(), 0,
			"explicit overlay should be used")
		assert.Equal(t, 0, ctxOverlay.dialCount(),
			"context default overlay should NOT be used when explicit is set")
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestHandler_OverlayPostWithBody(t *testing.T) {
	var receivedBody string
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		receivedBody = string(buf[:n])
		w.WriteHeader(gohttp.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer ts.Close()

	overlaySvc := &recordingService{}
	reg := newMockNetworkRegistry()
	reg.register("network:test-overlay", overlaySvc, netapi.KindSOCKS5)

	d := NewDispatcher(WithNetworkRegistry(reg))
	done := make(chan httpapi.Response, 1)

	bodyData := `{"secret": "data"}`
	err := d.handleRequest(overlayTestCtx(), &httpapi.RequestCmd{
		Method:         "POST",
		URL:            ts.URL,
		Body:           []byte(bodyData),
		Headers:        map[string][]string{"Content-Type": {"application/json"}},
		OverlayNetwork: "network:test-overlay",
	}, 0, &testReceiver{fn: func(data any) {
		done <- data.(httpapi.Response)
	}})
	require.NoError(t, err)

	select {
	case resp := <-done:
		require.Empty(t, resp.Error)
		assert.Equal(t, 200, resp.StatusCode)
		// Verify overlay was used
		assert.Greater(t, overlaySvc.dialCount(), 0)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	assert.Equal(t, `{"secret": "data"}`, receivedBody, "body should arrive through overlay")
}

func TestHandler_OverlayConcurrentRequests(t *testing.T) {
	var requestCount atomic.Int32
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		requestCount.Add(1)
		w.WriteHeader(gohttp.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer ts.Close()

	overlaySvc := &recordingService{}
	reg := newMockNetworkRegistry()
	reg.register("network:concurrent-overlay", overlaySvc, netapi.KindSOCKS5)

	d := NewDispatcher(WithNetworkRegistry(reg))

	const numRequests = 20
	var wg sync.WaitGroup
	wg.Add(numRequests)

	errors := make([]string, numRequests)

	ctx := overlayTestCtx()
	for i := 0; i < numRequests; i++ {
		go func(idx int) {
			defer wg.Done()
			done := make(chan httpapi.Response, 1)

			err := d.handleRequest(ctx, &httpapi.RequestCmd{
				Method:         "GET",
				URL:            ts.URL,
				OverlayNetwork: "network:concurrent-overlay",
			}, 0, &testReceiver{fn: func(data any) {
				done <- data.(httpapi.Response)
			}})
			if err != nil {
				errors[idx] = err.Error()
				return
			}

			select {
			case resp := <-done:
				if resp.Error != "" {
					errors[idx] = resp.Error
				}
			case <-time.After(10 * time.Second):
				errors[idx] = "timeout"
			}
		}(i)
	}

	wg.Wait()

	for i, e := range errors {
		assert.Empty(t, e, "request %d failed: %s", i, e)
	}

	assert.Equal(t, int32(numRequests), requestCount.Load(), "all requests should reach the server")
	assert.Greater(t, overlaySvc.dialCount(), 0, "overlay should have been used")
}

func TestCheckOverlayPrivateIP(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		blocked bool
	}{
		// Literal private IPs — must be blocked
		{"loopback IPv4", "http://127.0.0.1:8080/", true},
		{"loopback IPv6", "http://[::1]:8080/", true},
		{"private 10.x", "http://10.0.0.1/", true},
		{"private 192.168.x", "http://192.168.1.1/", true},
		{"private 172.16.x", "http://172.16.0.1/", true},
		{"unspecified", "http://0.0.0.0/", true},
		{"link-local", "http://169.254.1.1/", true},

		// Literal public IPs — allowed
		{"public IP", "http://8.8.8.8/", false},
		{"public IP 2", "http://1.1.1.1/", false},

		// Hostnames — must NOT be resolved locally (no DNS leak)
		// These pass through to the overlay for remote resolution.
		{"hostname", "http://example.com/", false},
		{"hostname onion", "http://duckduckgogg42xjoc72x3sjasowoarfbgcmvfimaftt6twagswzczad.onion/", false},
		{"hostname localhost", "http://localhost/", false}, // NOT blocked — it's a hostname, not an IP literal

		// Edge cases
		{"empty host", "http:///path", false},
		{"invalid URL", "://broken", false},
	}

	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := checkOverlayPrivateIP(ctx, tc.url)
			if tc.blocked {
				assert.Error(t, err, "should block %s", tc.url)
				assert.Contains(t, err.Error(), "private IP")
			} else {
				assert.NoError(t, err, "should allow %s", tc.url)
			}
		})
	}
}

// TestCheckOverlayPrivateIP_NoDNSLeak verifies that checkOverlayPrivateIP does
// NOT perform local DNS resolution for hostnames. This is critical for overlay
// networks: resolving DNS locally would leak the target hostname to the system
// resolver, defeating Tor/I2P privacy.
//
// We verify this by passing a non-existent hostname — if DNS resolution were
// happening, it might either fail or hit the network. Since we only check
// literal IPs, the function should return nil instantly without any DNS query.
func TestCheckOverlayPrivateIP_NoDNSLeak(t *testing.T) {
	ctx := context.Background()

	// These are hostnames that would resolve to private IPs if DNS were performed.
	// With the fix, they must pass through without local DNS resolution.
	hostnameURLs := []string{
		"http://localhost:8080/secret",
		"http://internal.corp.example.com/api",
		"http://my-secret-service.local/data",
		"http://duckduckgogg42xjoc72x3sjasowoarfbgcmvfimaftt6twagswzczad.onion:443/",
		"http://this-domain-definitely-does-not-exist-xyz123.invalid/",
	}

	for _, u := range hostnameURLs {
		t.Run(u, func(t *testing.T) {
			err := checkOverlayPrivateIP(ctx, u)
			assert.NoError(t, err,
				"hostname %q must NOT be blocked — overlay resolves DNS remotely, no local leak", u)
		})
	}
}

// TestHandler_OverlayHostnamePassesThrough verifies the full handler path:
// when a request with a hostname (not IP literal) goes through an overlay,
// the handler does NOT block it even if the hostname would resolve to a
// private IP locally.
func TestHandler_OverlayHostnamePassesThrough(t *testing.T) {
	// Backend server
	ts := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		w.WriteHeader(gohttp.StatusOK)
		w.Write([]byte("reached"))
	}))
	defer ts.Close()

	// The test server URL uses 127.0.0.1 which is a literal private IP.
	// But if we construct a URL with "localhost" as hostname, it should
	// NOT be blocked by the overlay SSRF check (hostname, not literal IP).
	// Note: the actual connection still works because our recordingService
	// dials the real address.

	overlaySvc := &recordingService{}
	reg := newMockNetworkRegistry()
	reg.register("network:test-overlay", overlaySvc, netapi.KindSOCKS5)

	d := NewDispatcher(WithNetworkRegistry(reg))
	done := make(chan httpapi.Response, 1)

	// Use "localhost" hostname instead of 127.0.0.1 — this should NOT be
	// blocked because checkOverlayPrivateIP only checks literal IPs.
	localhostURL := "http://localhost" + ts.URL[len("http://127.0.0.1"):]

	err := d.handleRequest(overlayTestCtx(), &httpapi.RequestCmd{
		Method:         "GET",
		URL:            localhostURL,
		OverlayNetwork: "network:test-overlay",
	}, 0, &testReceiver{fn: func(data any) {
		done <- data.(httpapi.Response)
	}})
	require.NoError(t, err)

	select {
	case resp := <-done:
		require.Empty(t, resp.Error,
			"hostname 'localhost' must NOT be blocked via overlay — DNS resolved remotely")
		assert.Equal(t, 200, resp.StatusCode)
		assert.Equal(t, "reached", string(resp.Body))
		assert.Greater(t, overlaySvc.dialCount(), 0, "overlay should have been used")
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}
