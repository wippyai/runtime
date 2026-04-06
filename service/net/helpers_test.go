// SPDX-License-Identifier: MPL-2.0

package net

import (
	"net"
	"os"
	"testing"
	"time"
)

const (
	defaultTorHost = "127.0.0.1"
	defaultTorPort = 9050
	defaultI2PHost = "127.0.0.1"
	defaultI2PPort = 7656
)

// torAddr returns the Tor SOCKS5 proxy address for testing.
// Respects TOR_PROXY_HOST and TOR_PROXY_PORT environment variables.
func torAddr() (string, int) {
	host := os.Getenv("TOR_PROXY_HOST")
	if host == "" {
		host = defaultTorHost
	}
	port := defaultTorPort
	if p := os.Getenv("TOR_PROXY_PORT"); p != "" {
		var parsed int
		if n, _ := parsePort(p); n > 0 {
			parsed = n
		}
		if parsed > 0 {
			port = parsed
		}
	}
	return host, port
}

// skipIfNoTor skips the test if a Tor SOCKS5 proxy is not reachable.
// Tests guarded by this require a running Tor proxy (e.g. via docker-compose).
func skipIfNoTor(t *testing.T) {
	t.Helper()

	if testing.Short() {
		t.Skip("Tor E2E tests skipped in short mode")
	}
	if os.Getenv("SKIP_NETWORK_TESTS") == "1" {
		t.Skip("Network tests disabled via SKIP_NETWORK_TESTS")
	}

	host, port := torAddr()
	addr := net.JoinHostPort(host, portToString(port))

	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Skipf("Tor SOCKS5 proxy not available at %s: %v", addr, err)
	}
	conn.Close()
}

// i2pAddr returns the I2P SAM bridge address for testing.
// Respects I2P_SAM_HOST and I2P_SAM_PORT environment variables.
func i2pAddr() (string, int) {
	host := os.Getenv("I2P_SAM_HOST")
	if host == "" {
		host = defaultI2PHost
	}
	port := defaultI2PPort
	if p := os.Getenv("I2P_SAM_PORT"); p != "" {
		if n, ok := parsePort(p); ok && n > 0 {
			port = n
		}
	}
	return host, port
}

// skipIfNoI2P skips the test if an I2P SAM bridge is not reachable.
// Tests guarded by this require a running I2P router with SAM enabled
// (e.g. via docker-compose).
func skipIfNoI2P(t *testing.T) {
	t.Helper()

	if testing.Short() {
		t.Skip("I2P E2E tests skipped in short mode")
	}
	if os.Getenv("SKIP_NETWORK_TESTS") == "1" {
		t.Skip("Network tests disabled via SKIP_NETWORK_TESTS")
	}

	host, port := i2pAddr()
	addr := net.JoinHostPort(host, portToString(port))

	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Skipf("I2P SAM bridge not available at %s: %v", addr, err)
	}
	conn.Close()
}

func parsePort(s string) (int, bool) {
	port := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		port = port*10 + int(c-'0')
	}
	if port <= 0 || port > 65535 {
		return 0, false
	}
	return port, true
}

func portToString(port int) string {
	if port <= 0 {
		return "0"
	}
	s := ""
	for port > 0 {
		s = string(rune('0'+port%10)) + s
		port /= 10
	}
	return s
}

// --- Tailscale E2E helpers ---

// tailscaleEnv returns the Tailscale auth key and control URL from environment.
// TS_AUTHKEY is required for E2E tests. TS_CONTROL_URL is optional (defaults
// to tailscale.com if not set, or can point to a Headscale instance).
func tailscaleEnv() (authKey, controlURL string) {
	authKey = os.Getenv("TS_AUTHKEY")
	controlURL = os.Getenv("TS_CONTROL_URL")
	return authKey, controlURL
}

// tailscalePeerAddr returns a tailnet peer address to dial for connectivity
// testing. Set TS_PEER_ADDR (e.g. "my-server.tailnet:22") to enable peer
// connectivity tests. Returns empty string if not configured.
func tailscalePeerAddr() string {
	return os.Getenv("TS_PEER_ADDR")
}

// skipIfNoTailscale skips the test if Tailscale credentials are not available.
// Unlike Tor/I2P which just need a proxy running, Tailscale needs an auth key
// and a control server (tailscale.com or Headscale).
func skipIfNoTailscale(t *testing.T) {
	t.Helper()

	if testing.Short() {
		t.Skip("Tailscale E2E tests skipped in short mode")
	}
	if os.Getenv("SKIP_NETWORK_TESTS") == "1" {
		t.Skip("Network tests disabled via SKIP_NETWORK_TESTS")
	}

	authKey, _ := tailscaleEnv()
	if authKey == "" {
		t.Skip("Tailscale E2E tests require TS_AUTHKEY environment variable")
	}
}
