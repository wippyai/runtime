// SPDX-License-Identifier: MPL-2.0

package net

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"testing"
	"time"

	netapi "github.com/wippyai/runtime/api/net"
)

// execCommandContext is a variable so tests could override it if needed.
// It defaults to exec.CommandContext for running external commands.
var execCommandContext = exec.CommandContext

const (
	defaultTorHost         = "127.0.0.1"
	defaultTorPort         = 9050
	defaultI2PHost         = "127.0.0.1"
	defaultI2PPort         = 7656
	defaultOpenVPNMgmtAddr = "127.0.0.1:7505"
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

	conn, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(context.Background(), "tcp", addr)
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

	conn, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(context.Background(), "tcp", addr)
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

// --- OpenVPN E2E helpers ---

// openvpnMgmtAddr returns the OpenVPN management interface address for testing.
// Respects OPENVPN_MGMT_ADDR environment variable (e.g. "127.0.0.1:7505").
func openvpnMgmtAddr() string {
	addr := os.Getenv("OPENVPN_MGMT_ADDR")
	if addr == "" {
		addr = defaultOpenVPNMgmtAddr
	}
	return addr
}

// skipIfNoOpenVPN skips the test if the OpenVPN management interface is not reachable.
// Tests guarded by this require a running OpenVPN client with the management
// interface enabled (e.g. --management 127.0.0.1 7505).
func skipIfNoOpenVPN(t *testing.T) {
	t.Helper()

	if testing.Short() {
		t.Skip("OpenVPN E2E tests skipped in short mode")
	}
	if os.Getenv("SKIP_NETWORK_TESTS") == "1" {
		t.Skip("Network tests disabled via SKIP_NETWORK_TESTS")
	}

	addr := openvpnMgmtAddr()
	conn, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(context.Background(), "tcp", addr)
	if err != nil {
		t.Skipf("OpenVPN management interface not available at %s: %v", addr, err)
	}
	conn.Close()
}

// skipIfOpenVPNNotRoutable skips the test if the OpenVPN VPN IP is not
// bindable on the host. This happens when the OpenVPN client runs in Docker
// but the tests run on the host — the TUN interface (10.8.0.x) exists inside
// the container, not on the host.
//
// Use this instead of skipIfNoOpenVPN for tests that need actual traffic
// routing through the VPN tunnel.
func skipIfOpenVPNNotRoutable(t *testing.T) {
	t.Helper()
	skipIfNoOpenVPN(t)

	mgmtAddr := openvpnMgmtAddr()
	svc, err := NewOpenVPNService(&netapi.OpenVPNConfig{ManagementAddress: mgmtAddr})
	if err != nil {
		t.Skipf("OpenVPN service unavailable: %v", err)
	}

	// Verify the VPN's local IP exists on a local interface.
	// When the OpenVPN client runs in Docker, 10.8.0.x is inside the
	// container — not on the host — so binding will fail.
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", net.JoinHostPort(svc.localIP.String(), "0"))
	if err != nil {
		t.Skipf("OpenVPN VPN IP %s not bindable on host (VPN client likely in Docker): %v",
			svc.localIP, err)
	}
	ln.Close()
}

// --- Tailscale E2E helpers ---

const (
	// defaultHeadscaleContainer is the Docker container name for Headscale.
	defaultHeadscaleContainer = "overlay-networks-app-headscale-1"

	// defaultHeadscaleUser is the Headscale user for creating preauthkeys.
	defaultHeadscaleUser = "wippy-test"

	// defaultHeadscaleControlURL is the default Headscale control URL
	// when auto-generating auth keys from the local Docker instance.
	defaultHeadscaleControlURL = "http://localhost:8090"
)

// headscaleAuthKey attempts to generate a preauthkey from a running Headscale
// Docker container. It returns the key or an error if Docker is not available
// or the container is not running.
//
// The container name and user can be overridden via HEADSCALE_CONTAINER and
// HEADSCALE_USER environment variables.
func headscaleAuthKey() (string, error) {
	container := os.Getenv("HEADSCALE_CONTAINER")
	if container == "" {
		container = defaultHeadscaleContainer
	}
	user := os.Getenv("HEADSCALE_USER")
	if user == "" {
		user = defaultHeadscaleUser
	}

	out, err := execCommand("docker", "exec", container,
		"headscale", "preauthkeys", "create",
		"--user", user,
		"--reusable",
		"--expiration", "1h",
	)
	if err != nil {
		return "", fmt.Errorf("headscale preauthkey creation failed: %w (output: %s)", err, out)
	}

	// The output contains log lines (on stderr, captured separately) and the
	// key on stdout. Trim whitespace and take the last non-empty line.
	key := lastNonEmptyLine(out)
	if key == "" {
		return "", fmt.Errorf("headscale returned empty preauthkey (raw output: %q)", out)
	}

	return key, nil
}

// execCommand runs an external command and returns its combined output.
func execCommand(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := execCommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// lastNonEmptyLine returns the last non-empty line from a multi-line string.
func lastNonEmptyLine(s string) string {
	lines := splitLines(s)
	for i := len(lines) - 1; i >= 0; i-- {
		line := trimSpace(lines[i])
		if line != "" {
			return line
		}
	}
	return ""
}

// splitLines splits a string into lines without importing strings.
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// trimSpace trims leading and trailing whitespace without importing strings.
func trimSpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r' || s[start] == '\n') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r' || s[end-1] == '\n') {
		end--
	}
	return s[start:end]
}

// tailscaleEnv returns the Tailscale auth key and control URL for E2E tests.
//
// Resolution order for auth key:
//  1. TS_AUTHKEY environment variable (explicit key)
//  2. Auto-generate from running Headscale Docker container
//
// When auto-generating, TS_CONTROL_URL defaults to http://localhost:8090
// (the standard Headscale docker-compose port mapping).
func tailscaleEnv() (authKey, controlURL string) {
	authKey = os.Getenv("TS_AUTHKEY")
	controlURL = os.Getenv("TS_CONTROL_URL")

	if authKey != "" {
		return authKey, controlURL
	}

	// Try to auto-generate from Headscale Docker container.
	key, err := headscaleAuthKey()
	if err != nil {
		return "", controlURL
	}

	authKey = key
	// Default control URL to local Headscale when auto-generating.
	if controlURL == "" {
		controlURL = defaultHeadscaleControlURL
	}

	return authKey, controlURL
}

// skipIfNoTailscale skips the test if Tailscale credentials are not available.
// Unlike Tor/I2P which just need a proxy running, Tailscale needs an auth key
// and a control server (tailscale.com or Headscale).
//
// When TS_AUTHKEY is not set, this function attempts to auto-generate a
// preauthkey from a running Headscale Docker container before skipping.
func skipIfNoTailscale(t *testing.T) {
	t.Helper()

	if testing.Short() {
		t.Skip("Tailscale E2E tests skipped in short mode")
	}
	if os.Getenv("SKIP_NETWORK_TESTS") == "1" {
		t.Skip("Network tests disabled via SKIP_NETWORK_TESTS")
	}

	authKey, controlURL := tailscaleEnv()
	if authKey == "" {
		t.Skip("Tailscale E2E tests require TS_AUTHKEY or a running Headscale Docker container")
	}

	t.Logf("Tailscale auth key available (control=%s)", controlURL)
}
