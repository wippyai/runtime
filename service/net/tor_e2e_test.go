// SPDX-License-Identifier: MPL-2.0

package net

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	gohttp "net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	netapi "github.com/wippyai/runtime/api/net"
)

// torCheckResponse is the JSON response from check.torproject.org/api/ip.
type torCheckResponse struct {
	IP    string `json:"IP"`
	IsTor bool   `json:"IsTor"`
}

// TestTorE2E_TrafficRoutedThroughTor verifies that HTTP requests made through
// the TorService are actually routed through the Tor network.
//
// This is the definitive proof: check.torproject.org maintains a list of all
// known Tor exit node IPs and returns IsTor=true if the request originates
// from one of them.
//
// Requires: Tor SOCKS5 proxy running (docker-compose up tor-proxy)
func TestTorE2E_TrafficRoutedThroughTor(t *testing.T) {
	skipIfNoTor(t)

	host, port := torAddr()
	cfg := &netapi.TorConfig{
		NetworkConfig: netapi.NetworkConfig{Host: host, Port: port},
	}
	svc, err := NewTorService(cfg)
	require.NoError(t, err)

	client := &gohttp.Client{
		Transport: &gohttp.Transport{
			DialContext:       svc.DialContext,
			ForceAttemptHTTP2: false,
			TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12},
		},
		Timeout: 60 * time.Second, // Tor can be slow
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	req, err := gohttp.NewRequestWithContext(ctx, "GET", "https://check.torproject.org/api/ip", nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err, "request through Tor should succeed")
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var result torCheckResponse
	require.NoError(t, json.Unmarshal(body, &result), "response should be valid JSON: %s", string(body))

	assert.True(t, result.IsTor,
		"traffic MUST be routed through Tor (check.torproject.org reports IsTor=%v, IP=%s)",
		result.IsTor, result.IP)

	t.Logf("Tor exit IP: %s (IsTor: %v)", result.IP, result.IsTor)
}

// TestTorE2E_ExitIPDiffers verifies that the exit IP through Tor is different
// from the machine's real IP. This is a basic privacy verification.
//
// Requires: Tor SOCKS5 proxy running + internet access
func TestTorE2E_ExitIPDiffers(t *testing.T) {
	skipIfNoTor(t)

	host, port := torAddr()
	cfg := &netapi.TorConfig{
		NetworkConfig: netapi.NetworkConfig{Host: host, Port: port},
	}
	svc, err := NewTorService(cfg)
	require.NoError(t, err)

	// Get real IP (direct, no proxy)
	directClient := &gohttp.Client{Timeout: 15 * time.Second}
	realIP := getExternalIP(t, directClient)
	require.NotEmpty(t, realIP, "should be able to get real IP")
	t.Logf("Real IP: %s", realIP)

	// Get Tor exit IP
	torClient := &gohttp.Client{
		Transport: &gohttp.Transport{
			DialContext:       svc.DialContext,
			ForceAttemptHTTP2: false,
			TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12},
		},
		Timeout: 60 * time.Second,
	}
	torIP := getExternalIP(t, torClient)
	require.NotEmpty(t, torIP, "should be able to get Tor exit IP")
	t.Logf("Tor exit IP: %s", torIP)

	assert.NotEqual(t, realIP, torIP,
		"Tor exit IP (%s) should differ from real IP (%s) — traffic is being routed through Tor",
		torIP, realIP)
}

// TestTorE2E_StreamIsolation_DifferentCircuits verifies that with stream
// isolation enabled, multiple connections may use different Tor circuits
// (and thus different exit IPs).
//
// Note: Different circuits don't ALWAYS mean different exit IPs (Tor may reuse
// exits), but with enough requests we should see at least some diversity.
//
// Requires: Tor SOCKS5 proxy running + internet access
func TestTorE2E_StreamIsolation_DifferentCircuits(t *testing.T) {
	skipIfNoTor(t)

	host, port := torAddr()
	cfg := &netapi.TorConfig{
		NetworkConfig:  netapi.NetworkConfig{Host: host, Port: port},
		IsolateStreams: true,
	}
	svc, err := NewTorService(cfg)
	require.NoError(t, err)

	// Make several requests — each should get its own circuit
	ips := make(map[string]bool)
	const numRequests = 5

	for i := 0; i < numRequests; i++ {
		// Each request gets a new HTTP client to force new connections
		// (don't pool connections, which would reuse the same circuit)
		client := &gohttp.Client{
			Transport: &gohttp.Transport{
				DialContext:       svc.DialContext,
				ForceAttemptHTTP2: false,
				TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12},
				DisableKeepAlives: true, // Force new connections
			},
			Timeout: 60 * time.Second,
		}

		ip := getExternalIP(t, client)
		if ip != "" {
			ips[ip] = true
			t.Logf("Request %d: exit IP %s", i+1, ip)
		}
	}

	t.Logf("Unique exit IPs seen: %d out of %d requests", len(ips), numRequests)

	// All requests should succeed through Tor
	assert.Greater(t, len(ips), 0, "should have at least one successful Tor request")

	// With stream isolation, we EXPECT to sometimes see different IPs.
	// However, Tor may legitimately reuse the same exit node even with
	// different circuits, so we log rather than assert on diversity.
	if len(ips) > 1 {
		t.Logf("Stream isolation confirmed: %d different exit IPs observed", len(ips))
	} else {
		t.Logf("Note: Only 1 exit IP observed — Tor may reuse exits across circuits")
	}
}

// TestTorE2E_FullHTTPRequest tests a complete HTTP request-response cycle
// through the Tor overlay, verifying headers, status code, and body.
//
// Requires: Tor SOCKS5 proxy running + internet access
func TestTorE2E_FullHTTPRequest(t *testing.T) {
	skipIfNoTor(t)

	host, port := torAddr()
	cfg := &netapi.TorConfig{
		NetworkConfig: netapi.NetworkConfig{Host: host, Port: port},
	}
	svc, err := NewTorService(cfg)
	require.NoError(t, err)

	client := &gohttp.Client{
		Transport: &gohttp.Transport{
			DialContext:       svc.DialContext,
			ForceAttemptHTTP2: false,
			TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12},
		},
		Timeout: 60 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Use httpbin.org which returns request metadata as JSON
	req, err := gohttp.NewRequestWithContext(ctx, "GET", "https://httpbin.org/headers", nil)
	require.NoError(t, err)
	req.Header.Set("X-Test-Header", "wippy-tor-e2e")

	resp, err := client.Do(req)
	require.NoError(t, err, "full HTTP request through Tor should work")
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	// httpbin.org/headers returns {"headers": {...}} with all request headers
	var result struct {
		Headers map[string]string `json:"headers"`
	}
	require.NoError(t, json.Unmarshal(body, &result), "response should be JSON: %s", string(body))
	assert.Equal(t, "wippy-tor-e2e", result.Headers["X-Test-Header"],
		"custom header should survive Tor routing")
	t.Logf("Response headers from httpbin: %+v", result.Headers)
}

// TestTorE2E_OnionDNSResolution verifies that .onion addresses are resolved
// by the Tor proxy itself and not by the local DNS resolver.
//
// We test this by verifying that DialContext to a .onion address doesn't fail
// with a local DNS error — it should either succeed (if the .onion service
// is up) or fail with a Tor/SOCKS error.
//
// Requires: Tor SOCKS5 proxy running
func TestTorE2E_OnionDNSResolution(t *testing.T) {
	skipIfNoTor(t)

	host, port := torAddr()
	cfg := &netapi.TorConfig{
		NetworkConfig: netapi.NetworkConfig{Host: host, Port: port},
	}
	svc, err := NewTorService(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Try to dial a .onion address — it should NOT fail with a DNS resolution error
	// (which would mean the address leaked to local DNS)
	conn, err := svc.DialContext(ctx, "tcp", "duckduckgogg42xjoc72x3sjasowoarfbgcmvfimaftt6twagswzczad.onion:443")
	if err != nil {
		// The connection may fail for various Tor-related reasons (circuit timeout,
		// destination unreachable, etc.), but it should NOT be a local DNS error
		var dnsErr *net.DNSError
		assert.False(t, isLocalDNSError(err),
			"a .onion address should NOT cause local DNS resolution — got: %v", err)
		_ = dnsErr
		t.Logf(".onion dial returned (expected): %v", err)
	} else {
		conn.Close()
		t.Log(".onion address connected successfully — DNS resolved by Tor")
	}
}

// TestTorE2E_ConfigValidation tests that invalid Tor configurations are
// properly rejected.
func TestTorE2E_ConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		wantErr string
		cfg     netapi.TorConfig
	}{
		{
			name:    "empty host",
			cfg:     netapi.TorConfig{NetworkConfig: netapi.NetworkConfig{Host: "", Port: 9050}},
			wantErr: "host is required",
		},
		{
			name:    "zero port",
			cfg:     netapi.TorConfig{NetworkConfig: netapi.NetworkConfig{Host: "127.0.0.1", Port: 0}},
			wantErr: "invalid port",
		},
		{
			name:    "negative port",
			cfg:     netapi.TorConfig{NetworkConfig: netapi.NetworkConfig{Host: "127.0.0.1", Port: -1}},
			wantErr: "invalid port",
		},
		{
			name:    "port too large",
			cfg:     netapi.TorConfig{NetworkConfig: netapi.NetworkConfig{Host: "127.0.0.1", Port: 70000}},
			wantErr: "invalid port",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// --- helpers ---

// getExternalIP makes a request to a public IP echo service and returns the IP.
func getExternalIP(t *testing.T, client *gohttp.Client) string {
	t.Helper()

	// Try multiple IP echo services for reliability
	services := []struct {
		parse func([]byte) string
		url   string
	}{
		{
			url: "https://api.ipify.org?format=json",
			parse: func(body []byte) string {
				var r struct {
					IP string `json:"ip"`
				}
				if json.Unmarshal(body, &r) == nil {
					return r.IP
				}
				return ""
			},
		},
		{
			url: "https://httpbin.org/ip",
			parse: func(body []byte) string {
				var r struct {
					Origin string `json:"origin"`
				}
				if json.Unmarshal(body, &r) == nil {
					return r.Origin
				}
				return ""
			},
		},
	}

	for _, svc := range services {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		req, err := gohttp.NewRequestWithContext(ctx, "GET", svc.url, nil)
		if err != nil {
			cancel()
			continue
		}

		resp, err := client.Do(req)
		cancel()
		if err != nil {
			t.Logf("IP service %s failed: %v", svc.url, err)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}

		ip := svc.parse(body)
		if ip != "" {
			return ip
		}
	}

	t.Log("Warning: could not determine external IP from any service")
	return ""
}

// isLocalDNSError checks if the error is a local DNS resolution failure.
// This is used to detect if a .onion address leaked to the local resolver.
func isLocalDNSError(err error) bool {
	if err == nil {
		return false
	}
	var dnsErr *net.DNSError
	if ok := errorAs(err, &dnsErr); ok {
		return true
	}
	// Also check for common DNS error messages
	errStr := err.Error()
	return containsAny(errStr,
		"no such host",
		"Temporary failure in name resolution",
		"server misbehaving",
	)
}

func errorAs(err error, target any) bool {
	type asInterface interface {
		As(any) bool
	}
	for err != nil {
		if x, ok := err.(asInterface); ok {
			if x.As(target) {
				return true
			}
		}
		type unwrapper interface {
			Unwrap() error
		}
		if u, ok := err.(unwrapper); ok {
			err = u.Unwrap()
		} else {
			return false
		}
	}
	return false
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if containsStr(s, sub) {
			return true
		}
	}
	return false
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && s != "" && searchInString(s, sub)
}

func searchInString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
