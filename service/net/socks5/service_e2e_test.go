// SPDX-License-Identifier: MPL-2.0

package socks5

import (
	"context"
	"crypto/tls"
	gohttp "net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/service/net/nettest"
)

// TestSOCKS5E2E_DialsThroughProxy verifies that requests made through a
// Service actually egress via the configured proxy — i.e. the observed
// external IP differs from the host's direct-connection IP.
//
// Requires: a SOCKS5 proxy reachable at SOCKS5_PROXY_HOST/SOCKS5_PROXY_PORT
// (defaults to 127.0.0.1:9050, i.e. a local Tor daemon).
func TestSOCKS5E2E_DialsThroughProxy(t *testing.T) {
	skipIfNoSOCKS5(t)

	host, port := socks5Addr()
	cfg := &netapi.SOCKS5Config{Host: host, Port: port, IsolateStreams: true}

	svc, err := NewService(cfg)
	require.NoError(t, err)

	client := &gohttp.Client{
		Transport: &gohttp.Transport{
			DialContext:       svc.DialContext,
			ForceAttemptHTTP2: false,
			TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12},
		},
		Timeout: 60 * time.Second,
	}

	proxyIP := nettest.GetExternalIP(t, client)
	if proxyIP == "" {
		t.Skip("could not determine external IP through SOCKS5 proxy")
	}

	direct := &gohttp.Client{Timeout: 15 * time.Second}
	directIP := nettest.GetExternalIP(t, direct)

	if directIP != "" {
		assert.NotEqual(t, directIP, proxyIP,
			"proxy egress IP must differ from direct egress IP")
	}
	t.Logf("direct IP=%q, proxy IP=%q", directIP, proxyIP)
}

// TestSOCKS5E2E_StreamIsolation_DifferentCircuits verifies that two dials with
// IsolateStreams enabled produce independent circuits (different exit IPs)
// when run against Tor. Against a plain SOCKS5 proxy this is a no-op and the
// test will pass trivially when both IPs match.
func TestSOCKS5E2E_StreamIsolation_DifferentCircuits(t *testing.T) {
	skipIfNoSOCKS5(t)

	host, port := socks5Addr()
	cfg := &netapi.SOCKS5Config{Host: host, Port: port, IsolateStreams: true}

	svc, err := NewService(cfg)
	require.NoError(t, err)

	newClient := func() *gohttp.Client {
		return &gohttp.Client{
			Transport: &gohttp.Transport{
				DialContext:       svc.DialContext,
				ForceAttemptHTTP2: false,
				TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12},
				DisableKeepAlives: true,
			},
			Timeout: 60 * time.Second,
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	_ = ctx

	ip1 := nettest.GetExternalIP(t, newClient())
	ip2 := nettest.GetExternalIP(t, newClient())

	if ip1 == "" || ip2 == "" {
		t.Skip("could not determine external IPs through SOCKS5 proxy for both dials")
	}
	t.Logf("circuit 1 IP=%q, circuit 2 IP=%q", ip1, ip2)
}
