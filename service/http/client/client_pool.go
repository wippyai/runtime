// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"net"
	gohttp "net/http"
	"sync"
	"time"

	httpapi "github.com/wippyai/runtime/api/service/http"
)

// clientKey identifies a unique client configuration.
type clientKey struct {
	unixSocket     string
	tlsFingerprint string
	timeout        time.Duration
}

// clientOnce ensures single initialization of a client.
type clientOnce struct {
	client *gohttp.Client
	err    error
	once   sync.Once
}

// Pool provides pooled HTTP clients with proper connection reuse.
// Thread-safe, lock-free for hot path using sync.Map.
// SSRF protection happens at runtime level via security policies.
type Pool struct {
	defaultClient *gohttp.Client
	clients       sync.Map
}

// Default transport settings
const (
	defaultTimeout         = 30 * time.Second
	defaultIdleConnTimeout = 90 * time.Second
	defaultMaxIdleConns    = 100
	defaultMaxIdlePerHost  = 10
	defaultTLSHandshake    = 10 * time.Second
	defaultExpectContinue  = 1 * time.Second
	defaultKeepAlive       = 30 * time.Second
	defaultDialTimeout     = 30 * time.Second
)

// NewClientPool creates a new HTTP client pool with default settings.
func NewClientPool() *Pool {
	return &Pool{
		defaultClient: createClient(defaultTimeout, "", defaultMaxIdleConns, defaultMaxIdlePerHost, defaultIdleConnTimeout),
	}
}

// NewClientPoolWithConfig creates a pool with custom configuration.
func NewClientPoolWithConfig(cfg PoolConfig) *Pool {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	maxIdle := cfg.MaxIdleConns
	if maxIdle <= 0 {
		maxIdle = defaultMaxIdleConns
	}
	maxPerHost := cfg.MaxIdlePerHost
	if maxPerHost <= 0 {
		maxPerHost = defaultMaxIdlePerHost
	}
	idleTimeout := cfg.IdleConnTimeout
	if idleTimeout <= 0 {
		idleTimeout = defaultIdleConnTimeout
	}
	return &Pool{
		defaultClient: createClient(timeout, "", maxIdle, maxPerHost, idleTimeout),
	}
}

// GetClient returns a pooled client for the given configuration.
// Uses default client when possible to maximize connection reuse.
func (p *Pool) GetClient(timeout time.Duration, unixSocket string) *gohttp.Client {
	// Use default for standard cases (most common path)
	if unixSocket == "" && (timeout <= 0 || timeout == defaultTimeout) {
		return p.defaultClient
	}

	if timeout <= 0 {
		timeout = defaultTimeout
	}

	key := clientKey{timeout: timeout, unixSocket: unixSocket}

	// Fast path: client exists
	if v, ok := p.clients.Load(key); ok {
		co := v.(*clientOnce)
		co.once.Do(func() {
			co.client = createClient(timeout, unixSocket, defaultMaxIdleConns, defaultMaxIdlePerHost, defaultIdleConnTimeout)
		})
		return co.client
	}

	// Slow path: create new entry
	co := &clientOnce{}
	actual, loaded := p.clients.LoadOrStore(key, co)
	if loaded {
		co = actual.(*clientOnce)
	}

	co.once.Do(func() {
		co.client = createClient(timeout, unixSocket, defaultMaxIdleConns, defaultMaxIdlePerHost, defaultIdleConnTimeout)
	})

	return co.client
}

// GetClientWithTLS returns a pooled client configured with custom TLS settings.
// Clients with the same TLS fingerprint, timeout, and socket are reused.
// Fingerprint is checked first; PEM parsing only happens on cache miss.
func (p *Pool) GetClientWithTLS(timeout time.Duration, unixSocket string, cfg *httpapi.TLSConfig) (*gohttp.Client, error) {
	if cfg == nil {
		return p.GetClient(timeout, unixSocket), nil
	}

	if timeout <= 0 {
		timeout = defaultTimeout
	}

	fp := tlsFingerprint(cfg)
	key := clientKey{timeout: timeout, unixSocket: unixSocket, tlsFingerprint: fp}

	// Fast path: entry exists (once.Do handles init race)
	if v, ok := p.clients.Load(key); ok {
		co := v.(*clientOnce)
		co.once.Do(func() {
			tlsCfg, err := buildTLSConfig(cfg)
			if err != nil {
				co.err = err
				return
			}
			co.client = createClientFromTLS(timeout, unixSocket, tlsCfg)
		})
		if co.err != nil {
			return nil, co.err
		}
		return co.client, nil
	}

	// Slow path: validate PEM upfront, then cache
	tlsCfg, err := buildTLSConfig(cfg)
	if err != nil {
		return nil, err
	}

	co := &clientOnce{}
	actual, loaded := p.clients.LoadOrStore(key, co)
	if loaded {
		co = actual.(*clientOnce)
	}

	co.once.Do(func() {
		co.client = createClientFromTLS(timeout, unixSocket, tlsCfg)
	})

	return co.client, nil
}

// buildTLSConfig constructs a *tls.Config from the per-request TLS configuration.
func buildTLSConfig(cfg *httpapi.TLSConfig) (*tls.Config, error) {
	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	hasCert := len(cfg.CertPEM) > 0
	hasKey := len(cfg.KeyPEM) > 0
	if hasCert != hasKey {
		return nil, fmt.Errorf("both cert and key must be provided together")
	}
	if hasCert {
		cert, err := tls.X509KeyPair(cfg.CertPEM, cfg.KeyPEM)
		if err != nil {
			return nil, fmt.Errorf("parse client certificate: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	if len(cfg.CAPEM) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(cfg.CAPEM) {
			return nil, fmt.Errorf("parse CA certificate: no valid certificates found")
		}
		tlsCfg.RootCAs = pool
	}

	if cfg.ServerName != "" {
		tlsCfg.ServerName = cfg.ServerName
	}

	tlsCfg.InsecureSkipVerify = cfg.InsecureSkipVerify

	return tlsCfg, nil
}

// tlsFingerprint produces a hex-encoded SHA256 hash of the TLS config material,
// used as a cache key for pooled clients.
func tlsFingerprint(cfg *httpapi.TLSConfig) string {
	h := sha256.New()
	h.Write(cfg.CertPEM)
	h.Write([]byte{0})
	h.Write(cfg.KeyPEM)
	h.Write([]byte{0})
	h.Write(cfg.CAPEM)
	h.Write([]byte{0})
	h.Write([]byte(cfg.ServerName))
	if cfg.InsecureSkipVerify {
		h.Write([]byte{1})
	} else {
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// createClientFromTLS builds an HTTP client with a pre-parsed *tls.Config.
func createClientFromTLS(timeout time.Duration, unixSocket string, tlsCfg *tls.Config) *gohttp.Client {
	return createClient(timeout, unixSocket, defaultMaxIdleConns, defaultMaxIdlePerHost, defaultIdleConnTimeout, tlsCfg)
}

// Size returns the number of pooled clients (for monitoring/testing).
func (p *Pool) Size() int {
	count := 0
	p.clients.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

// createClient builds an HTTP client with optional TLS configuration.
// SSRF protection happens at runtime level via security policies.
func createClient(timeout time.Duration, unixSocket string, maxIdleConns, maxIdlePerHost int, idleConnTimeout time.Duration, tlsCfg ...*tls.Config) *gohttp.Client {
	dialer := &net.Dialer{
		Timeout:   defaultDialTimeout,
		KeepAlive: defaultKeepAlive,
	}

	transport := &gohttp.Transport{
		MaxIdleConns:          maxIdleConns,
		MaxIdleConnsPerHost:   maxIdlePerHost,
		IdleConnTimeout:       idleConnTimeout,
		TLSHandshakeTimeout:   defaultTLSHandshake,
		ExpectContinueTimeout: defaultExpectContinue,
		ForceAttemptHTTP2:     true,
		DialContext:           dialer.DialContext,
	}

	if len(tlsCfg) > 0 && tlsCfg[0] != nil {
		transport.TLSClientConfig = tlsCfg[0]
	}

	if unixSocket != "" {
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", unixSocket)
		}
	}

	return &gohttp.Client{
		Transport: transport,
		Timeout:   timeout,
	}
}
