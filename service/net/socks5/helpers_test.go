// SPDX-License-Identifier: MPL-2.0

package socks5

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/wippyai/runtime/service/net/nettest"
)

const (
	defaultSOCKS5Host = "127.0.0.1"
	defaultSOCKS5Port = 9050
)

// socks5Addr returns the SOCKS5 proxy address for testing, honoring
// SOCKS5_PROXY_HOST / SOCKS5_PROXY_PORT.
func socks5Addr() (string, int) {
	host := os.Getenv("SOCKS5_PROXY_HOST")
	if host == "" {
		host = defaultSOCKS5Host
	}
	port := defaultSOCKS5Port
	if p := os.Getenv("SOCKS5_PROXY_PORT"); p != "" {
		if n, ok := nettest.ParsePort(p); ok && n > 0 {
			port = n
		}
	}
	return host, port
}

// skipIfNoSOCKS5 skips the test if a SOCKS5 proxy is not reachable.
// Tests guarded by this require a running SOCKS5 server (e.g. Tor via
// docker-compose).
func skipIfNoSOCKS5(t *testing.T) {
	t.Helper()

	if testing.Short() {
		t.Skip("SOCKS5 E2E tests skipped in short mode")
	}
	if os.Getenv("SKIP_NETWORK_TESTS") == "1" {
		t.Skip("Network tests disabled via SKIP_NETWORK_TESTS")
	}

	host, port := socks5Addr()
	addr := net.JoinHostPort(host, nettest.PortToString(port))

	conn, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(context.Background(), "tcp", addr)
	if err != nil {
		t.Skipf("SOCKS5 proxy not available at %s: %v", addr, err)
	}
	conn.Close()
}
