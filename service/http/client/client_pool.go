package client

import (
	"context"
	"net"
	gohttp "net/http"
	"sync"
	"time"
)

// clientKey identifies a unique client configuration.
type clientKey struct {
	unixSocket string
	timeout    time.Duration
}

// clientOnce ensures single initialization of a client.
type clientOnce struct {
	client *gohttp.Client
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

// Size returns the number of pooled clients (for monitoring/testing).
func (p *Pool) Size() int {
	count := 0
	p.clients.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

// createClient builds an HTTP client.
// SSRF protection happens at runtime level via security policies.
func createClient(timeout time.Duration, unixSocket string, maxIdleConns, maxIdlePerHost int, idleConnTimeout time.Duration) *gohttp.Client {
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
