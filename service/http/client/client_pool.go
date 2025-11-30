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
	timeout    time.Duration
	unixSocket string
}

// clientOnce ensures single initialization of a client.
type clientOnce struct {
	once   sync.Once
	client *gohttp.Client
}

// ClientPool provides pooled HTTP clients with proper connection reuse.
// Thread-safe, lock-free for hot path using sync.Map.
type ClientPool struct {
	clients       sync.Map // map[clientKey]*clientOnce
	defaultClient *gohttp.Client
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
	maxPooledClients       = 64 // prevent unbounded growth
)

// NewClientPool creates a new HTTP client pool.
func NewClientPool() *ClientPool {
	return &ClientPool{
		defaultClient: createClient(defaultTimeout, ""),
	}
}

// GetClient returns a pooled client for the given configuration.
// Uses default client when possible to maximize connection reuse.
func (p *ClientPool) GetClient(timeout time.Duration, unixSocket string) *gohttp.Client {
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
			co.client = createClient(timeout, unixSocket)
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
		co.client = createClient(timeout, unixSocket)
	})

	return co.client
}

// Size returns the number of pooled clients (for monitoring/testing).
func (p *ClientPool) Size() int {
	count := 0
	p.clients.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

// createClient builds an HTTP client with proper transport configuration.
func createClient(timeout time.Duration, unixSocket string) *gohttp.Client {
	dialer := &net.Dialer{
		Timeout:   defaultDialTimeout,
		KeepAlive: defaultKeepAlive,
	}

	transport := &gohttp.Transport{
		DialContext:           dialer.DialContext,
		MaxIdleConns:          defaultMaxIdleConns,
		MaxIdleConnsPerHost:   defaultMaxIdlePerHost,
		IdleConnTimeout:       defaultIdleConnTimeout,
		TLSHandshakeTimeout:   defaultTLSHandshake,
		ExpectContinueTimeout: defaultExpectContinue,
		ForceAttemptHTTP2:     true,
	}

	// Unix socket support
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
