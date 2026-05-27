// SPDX-License-Identifier: MPL-2.0

package i2p

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/wippyai/runtime/service/net/nettest"
)

const (
	defaultI2PHost = "127.0.0.1"
	defaultI2PPort = 7656
)

// i2pAddr returns the I2P SAM bridge address for testing. Respects
// I2P_SAM_HOST and I2P_SAM_PORT environment variables.
func i2pAddr() (string, int) {
	host := os.Getenv("I2P_SAM_HOST")
	if host == "" {
		host = defaultI2PHost
	}
	port := defaultI2PPort
	if p := os.Getenv("I2P_SAM_PORT"); p != "" {
		if n, ok := nettest.ParsePort(p); ok && n > 0 {
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
	addr := net.JoinHostPort(host, nettest.PortToString(port))

	conn, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(context.Background(), "tcp", addr)
	if err != nil {
		t.Skipf("I2P SAM bridge not available at %s: %v", addr, err)
	}
	conn.Close()
}

// contains reports whether substr is a substring of s.
func contains(s, substr string) bool { return nettest.Contains(s, substr) }

// containsAny reports whether any of substrs is a substring of s.
func containsAny(s string, substrs ...string) bool { return nettest.ContainsAny(s, substrs...) }

// errorAs walks the error chain looking for a match that As can extract.
func errorAs(err error, target any) bool { return nettest.ErrorAs(err, target) }
