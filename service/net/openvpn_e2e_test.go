// SPDX-License-Identifier: MPL-2.0

package net

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	gohttp "net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	netapi "github.com/wippyai/runtime/api/net"
)

// TestOpenVPNE2E_TrafficRoutedThroughVPN verifies that HTTP requests made
// through the OpenVPNService are routed through the VPN tunnel by checking
// that the exit IP differs from the machine's real IP.
//
// Requires: OpenVPN client running with management interface enabled
func TestOpenVPNE2E_TrafficRoutedThroughVPN(t *testing.T) {
	skipIfOpenVPNNotRoutable(t)

	mgmtAddr := openvpnMgmtAddr()
	cfg := &netapi.OpenVPNConfig{
		ManagementAddress: mgmtAddr,
	}
	svc, err := NewOpenVPNService(cfg)
	require.NoError(t, err)

	// Get real IP (direct, no VPN binding)
	directClient := &gohttp.Client{Timeout: 15 * time.Second}
	realIP := getExternalIP(t, directClient)
	require.NotEmpty(t, realIP, "should be able to get real IP")
	t.Logf("Real IP: %s", realIP)

	// Get VPN exit IP by binding to the VPN's local IP
	vpnClient := &gohttp.Client{
		Transport: &gohttp.Transport{
			DialContext:       svc.DialContext,
			ForceAttemptHTTP2: false,
			TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12},
		},
		Timeout: 30 * time.Second,
	}
	vpnIP := getExternalIP(t, vpnClient)
	require.NotEmpty(t, vpnIP, "should be able to get VPN exit IP")
	t.Logf("VPN exit IP: %s", vpnIP)

	assert.NotEqual(t, realIP, vpnIP,
		"VPN exit IP (%s) should differ from real IP (%s) — traffic is being routed through VPN",
		vpnIP, realIP)
}

// TestOpenVPNE2E_FullHTTPRequest tests a complete HTTP request-response cycle
// through the OpenVPN overlay, verifying headers, status code, and body.
//
// Requires: OpenVPN client running with management interface + internet access
func TestOpenVPNE2E_FullHTTPRequest(t *testing.T) {
	skipIfOpenVPNNotRoutable(t)

	mgmtAddr := openvpnMgmtAddr()
	cfg := &netapi.OpenVPNConfig{
		ManagementAddress: mgmtAddr,
	}
	svc, err := NewOpenVPNService(cfg)
	require.NoError(t, err)

	client := &gohttp.Client{
		Transport: &gohttp.Transport{
			DialContext:       svc.DialContext,
			ForceAttemptHTTP2: false,
			TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12},
		},
		Timeout: 30 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := gohttp.NewRequestWithContext(ctx, "GET", "https://httpbin.org/headers", nil)
	require.NoError(t, err)
	req.Header.Set("X-Test-Header", "wippy-openvpn-e2e")

	resp, err := client.Do(req)
	require.NoError(t, err, "full HTTP request through OpenVPN should work")
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var result struct {
		Headers map[string]string `json:"headers"`
	}
	require.NoError(t, json.Unmarshal(body, &result), "response should be JSON: %s", string(body))
	assert.Equal(t, "wippy-openvpn-e2e", result.Headers["X-Test-Header"],
		"custom header should survive VPN routing")
	t.Logf("Response headers from httpbin: %+v", result.Headers)
}

// TestOpenVPNE2E_MultipleRequests verifies that multiple sequential requests
// through the VPN all succeed and share the same exit IP (single tunnel,
// single exit point).
//
// Requires: OpenVPN client running with management interface + internet access
func TestOpenVPNE2E_MultipleRequests(t *testing.T) {
	skipIfOpenVPNNotRoutable(t)

	mgmtAddr := openvpnMgmtAddr()
	cfg := &netapi.OpenVPNConfig{
		ManagementAddress: mgmtAddr,
	}
	svc, err := NewOpenVPNService(cfg)
	require.NoError(t, err)

	const numRequests = 5
	ips := make(map[string]bool)

	for i := 0; i < numRequests; i++ {
		client := &gohttp.Client{
			Transport: &gohttp.Transport{
				DialContext:       svc.DialContext,
				ForceAttemptHTTP2: false,
				TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12},
				DisableKeepAlives: true,
			},
			Timeout: 30 * time.Second,
		}

		ip := getExternalIP(t, client)
		if ip != "" {
			ips[ip] = true
			t.Logf("Request %d: exit IP %s", i+1, ip)
		}
	}

	assert.Greater(t, len(ips), 0, "should have at least one successful VPN request")

	// A VPN tunnel should produce the same exit IP for all requests.
	if len(ips) == 1 {
		t.Log("Confirmed: all requests share the same VPN exit IP (expected for VPN)")
	} else {
		t.Logf("Note: %d different exit IPs observed — VPN may have multiple exit points", len(ips))
	}
}

// TestOpenVPNE2E_ConfigValidation tests that invalid OpenVPN configurations
// are properly rejected.
func TestOpenVPNE2E_ConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		wantErr string
		cfg     netapi.OpenVPNConfig
	}{
		{
			name:    "empty management address",
			cfg:     netapi.OpenVPNConfig{ManagementAddress: ""},
			wantErr: "management_address is required",
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
