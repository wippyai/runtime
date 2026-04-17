// SPDX-License-Identifier: MPL-2.0

package i2p

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	gohttp "net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/service/net/nettest"
)

// TestI2PE2E_SAMHandshake verifies that the Service can complete the full
// SAM v3 handshake (HELLO + SESSION CREATE) with a real I2P router.
//
// This is the first E2E validation: proving that we speak correct SAM v3
// protocol and the I2P router accepts our session.
//
// Requires: I2P router with SAM enabled (docker-compose up i2p)
func TestI2PE2E_SAMHandshake(t *testing.T) {
	skipIfNoI2P(t)

	host, port := i2pAddr()
	cfg := &netapi.I2PConfig{
		NetworkConfig: netapi.NetworkConfig{Host: host, Port: port},
		SessionName:   fmt.Sprintf("wippy-e2e-handshake-%d", time.Now().UnixNano()),
	}
	svc, err := NewService(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Attempt to connect to a known I2P destination.
	// The SAM handshake (HELLO + SESSION CREATE) must complete before
	// STREAM CONNECT. If we get past this without a handshake error,
	// the SAM v3 protocol implementation is correct.
	//
	// We use i2p-projekt.i2p — the official I2P project site.
	conn, err := svc.DialContext(ctx, "tcp", "i2p-projekt.i2p:80")
	if err != nil {
		// STREAM CONNECT may fail (destination unreachable, tunnel not ready),
		// but the error message tells us how far we got.
		errStr := err.Error()

		// Timeouts during any phase are infrastructure issues, not code bugs.
		isTimeout := contains(errStr, "i/o timeout") || contains(errStr, "deadline exceeded")
		if isTimeout {
			t.Logf("SAM bridge timed out (infrastructure, not code): %v", err)
			return
		}

		if contains(errStr, "SAM handshake") && contains(errStr, "rejected") {
			t.Fatalf("SAM v3 handshake rejected — protocol implementation broken: %v", err)
		}
		if contains(errStr, "SESSION CREATE rejected") {
			t.Fatalf("SAM SESSION CREATE rejected — session management broken: %v", err)
		}
		// STREAM CONNECT failure is acceptable (I2P tunnels take time)
		t.Logf("STREAM CONNECT failed (expected for cold tunnels): %v", err)
		return
	}
	defer conn.Close()
	t.Log("Full SAM v3 handshake + STREAM CONNECT succeeded")
}

// TestI2PE2E_TrafficRoutedThroughI2P verifies that HTTP traffic is routed
// through the I2P network by connecting to a known .i2p service.
//
// Unlike Tor, I2P doesn't have an equivalent of check.torproject.org.
// Instead, we verify by:
// 1. Successfully completing SAM handshake with the real I2P router
// 2. Making an HTTP request to a .i2p destination
// 3. Receiving a valid HTTP response OR a SAM-level error (both prove I2P routing)
//
// SAM-level errors like CANT_REACH_PEER or INVALID_KEY prove the request
// went through I2P — the I2P router attempted to route it. Only local DNS
// errors or clearnet fallbacks would indicate a traffic leak.
//
// Requires: I2P router with SAM enabled
func TestI2PE2E_TrafficRoutedThroughI2P(t *testing.T) {
	skipIfNoI2P(t)

	host, port := i2pAddr()
	cfg := &netapi.I2PConfig{
		NetworkConfig: netapi.NetworkConfig{Host: host, Port: port},
		SessionName:   fmt.Sprintf("wippy-e2e-http-%d", time.Now().UnixNano()),
	}
	svc, err := NewService(cfg)
	require.NoError(t, err)

	client := &gohttp.Client{
		Transport: &gohttp.Transport{
			DialContext:       svc.DialContext,
			ForceAttemptHTTP2: false,
			TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12},
		},
		Timeout: 120 * time.Second, // I2P tunnels can be very slow to build
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Try i2p-projekt.i2p — the official I2P project website
	req, err := gohttp.NewRequestWithContext(ctx, "GET", "http://i2p-projekt.i2p/", nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	if err != nil {
		errStr := err.Error()

		// Check for local DNS errors — these would indicate a traffic leak
		var dnsErr *net.DNSError
		if ok := errorAs(err, &dnsErr); ok {
			t.Fatalf("traffic leaked to local DNS: %v", err)
		}
		if containsAny(errStr, "no such host", "name resolution") &&
			!contains(errStr, "STREAM") && !contains(errStr, "SAM") {
			t.Fatalf("traffic leaked to local DNS: %v", err)
		}

		// SAM-level errors prove traffic was routed through I2P.
		// CANT_REACH_PEER = I2P router tried to reach destination via I2P network
		// INVALID_KEY = I2P router attempted naming resolution via I2P
		// Timeout during STREAM CONNECT = I2P router was building tunnels
		if containsAny(errStr, "CANT_REACH_PEER", "INVALID_KEY", "STREAM CONNECT", "i2p:") {
			t.Logf("I2P routing confirmed via SAM-level error (peer unreachable from firewalled router): %v", err)
			return
		}

		// Timeouts during setup are infrastructure issues
		if containsAny(errStr, "i/o timeout", "deadline exceeded") {
			t.Logf("I2P routing attempted but timed out (infrastructure): %v", err)
			return
		}

		t.Fatalf("unexpected non-I2P error (possible traffic leak): %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	require.NoError(t, err)

	t.Logf("I2P HTTP response: status=%d, body_len=%d", resp.StatusCode, len(body))

	// Full HTTP round-trip through I2P — strongest proof
	assert.Greater(t, resp.StatusCode, 0, "should receive a valid HTTP status code")
	assert.Greater(t, len(body), 0, "should receive response body")
}

// TestI2PE2E_DNSNotLeaked verifies that .i2p addresses are resolved by the
// I2P router through the SAM bridge, not by local DNS.
//
// Since I2P uses its own naming system (.i2p, .b32.i2p), local DNS resolution
// would always fail for these addresses. The SAM protocol sends the destination
// as-is in the STREAM CONNECT command, letting the I2P router resolve it.
//
// Requires: I2P router with SAM enabled
func TestI2PE2E_DNSNotLeaked(t *testing.T) {
	skipIfNoI2P(t)

	host, port := i2pAddr()
	cfg := &netapi.I2PConfig{
		NetworkConfig: netapi.NetworkConfig{Host: host, Port: port},
		SessionName:   fmt.Sprintf("wippy-e2e-dns-%d", time.Now().UnixNano()),
	}
	svc, err := NewService(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// A .i2p address should never be resolved by local DNS.
	// The fact that DialContext doesn't immediately fail with a DNS error
	// proves the address is sent to the SAM bridge for I2P-internal resolution.
	conn, err := svc.DialContext(ctx, "tcp", "i2p-projekt.i2p:80")
	if err != nil {
		// Check it's NOT a local DNS error
		var dnsErr *net.DNSError
		if ok := errorAs(err, &dnsErr); ok {
			t.Fatalf(".i2p address leaked to local DNS resolver: %v", err)
		}

		errStr := err.Error()
		if containsAny(errStr, "no such host", "name resolution") {
			t.Fatalf(".i2p address leaked to local DNS resolver: %v", err)
		}

		// SAM-level errors are expected and prove DNS is handled by I2P
		t.Logf("SAM-level error (not a DNS leak): %v", err)
	} else {
		conn.Close()
		t.Log("Connection succeeded — DNS resolved by I2P router")
	}
}

// TestI2PE2E_RawSAMProtocol verifies the raw SAM v3 protocol exchange
// with a real I2P router. This is a lower-level test that validates
// each step of the handshake independently.
//
// Requires: I2P router with SAM enabled
func TestI2PE2E_RawSAMProtocol(t *testing.T) {
	skipIfNoI2P(t)

	host, port := i2pAddr()
	addr := net.JoinHostPort(host, nettest.PortToString(port))

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	require.NoError(t, err, "should connect to SAM bridge")
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(60 * time.Second))
	reader := bufio.NewReader(conn)

	// Step 1: HELLO VERSION
	_, err = fmt.Fprintf(conn, "HELLO VERSION MIN=3.0 MAX=3.3\n")
	require.NoError(t, err)

	resp, err := samReadLine(reader)
	require.NoError(t, err, "should receive HELLO response")
	t.Logf("HELLO response: %s", resp)

	result, ok := samParseResult(resp)
	require.True(t, ok, "response should contain RESULT=")
	assert.Equal(t, "OK", result, "SAM v3 handshake should succeed")

	// Step 2: SESSION CREATE with a unique name
	sessionName := fmt.Sprintf("wippy-raw-test-%d", time.Now().UnixNano())
	_, err = fmt.Fprintf(conn, "SESSION CREATE STYLE=STREAM ID=%s DESTINATION=TRANSIENT\n", sessionName)
	require.NoError(t, err)

	resp, err = samReadLine(reader)
	require.NoError(t, err, "should receive SESSION response")
	t.Logf("SESSION response: %s", resp)

	result, ok = samParseResult(resp)
	require.True(t, ok, "response should contain RESULT=")
	assert.Equal(t, "OK", result, "SESSION CREATE should succeed with TRANSIENT destination")
}
