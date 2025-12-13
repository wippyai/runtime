package client

import (
	"context"
	"fmt"
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

// Pool provides pooled HTTP clients with proper connection reuse.
// Thread-safe, lock-free for hot path using sync.Map.
type Pool struct {
	clients         sync.Map // map[clientKey]*clientOnce
	defaultClient   *gohttp.Client
	blockPrivateIPs bool
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

// NewClientPool creates a new HTTP client pool with default settings.
// SSRF protection is disabled by default for backward compatibility.
func NewClientPool() *Pool {
	return &Pool{
		defaultClient:   createClientWithSSRF(defaultTimeout, "", defaultMaxIdleConns, defaultMaxIdlePerHost, defaultIdleConnTimeout, false),
		blockPrivateIPs: false,
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
		defaultClient:   createClientWithSSRF(timeout, "", maxIdle, maxPerHost, idleTimeout, cfg.BlockPrivateIPs),
		blockPrivateIPs: cfg.BlockPrivateIPs,
	}
}

// GetClient returns a pooled client using pool's default SSRF setting.
func (p *Pool) GetClient(timeout time.Duration, unixSocket string) *gohttp.Client {
	return p.GetClientWithSSRF(timeout, unixSocket, p.blockPrivateIPs)
}

// GetClientWithSSRF returns a client for the given configuration.
// Uses default client when possible to maximize connection reuse.
func (p *Pool) GetClientWithSSRF(timeout time.Duration, unixSocket string, blockPrivateIPs bool) *gohttp.Client {
	// If requested SSRF setting differs from pool default, create unpooled client
	if blockPrivateIPs != p.blockPrivateIPs {
		if timeout <= 0 {
			timeout = defaultTimeout
		}
		return createClientWithSSRF(timeout, unixSocket, defaultMaxIdleConns, defaultMaxIdlePerHost, defaultIdleConnTimeout, blockPrivateIPs)
	}

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
			co.client = createClientWithSSRF(timeout, unixSocket, defaultMaxIdleConns, defaultMaxIdlePerHost, defaultIdleConnTimeout, p.blockPrivateIPs)
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
		co.client = createClientWithSSRF(timeout, unixSocket, defaultMaxIdleConns, defaultMaxIdlePerHost, defaultIdleConnTimeout, p.blockPrivateIPs)
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

// isPrivateIP checks if an IP address is private, loopback, or otherwise internal.
func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	// Check for IPv4-mapped IPv6 addresses
	if ip4 := ip.To4(); ip4 != nil {
		return ip4.IsLoopback() || ip4.IsPrivate() || ip4.IsLinkLocalUnicast()
	}
	return false
}

// safeDialContext wraps a dialer to block connections to private IP addresses.
func safeDialContext(dialer *net.Dialer) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}

		// Resolve the hostname
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}

		// Check all resolved IPs for private addresses
		for _, ip := range ips {
			if isPrivateIP(ip) {
				return nil, fmt.Errorf("SSRF protection: connection to private IP %s blocked", ip)
			}
		}

		// Use the first non-private IP
		if len(ips) == 0 {
			return nil, fmt.Errorf("no IP addresses resolved for %s", host)
		}

		// Connect to the first resolved IP
		return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}
}

// createClient builds an HTTP client with proper transport configuration.
func createClient(timeout time.Duration, unixSocket string, maxIdleConns, maxIdlePerHost int, idleConnTimeout time.Duration) *gohttp.Client {
	return createClientWithSSRF(timeout, unixSocket, maxIdleConns, maxIdlePerHost, idleConnTimeout, true)
}

// createClientWithSSRF builds an HTTP client with optional SSRF protection.
func createClientWithSSRF(timeout time.Duration, unixSocket string, maxIdleConns, maxIdlePerHost int, idleConnTimeout time.Duration, blockPrivateIPs bool) *gohttp.Client {
	dialer := &net.Dialer{
		Timeout:   defaultDialTimeout,
		KeepAlive: defaultKeepAlive,
	}

	var dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)
	if blockPrivateIPs && unixSocket == "" {
		dialFunc = safeDialContext(dialer)
	} else {
		dialFunc = dialer.DialContext
	}

	transport := &gohttp.Transport{
		DialContext:           dialFunc,
		MaxIdleConns:          maxIdleConns,
		MaxIdleConnsPerHost:   maxIdlePerHost,
		IdleConnTimeout:       idleConnTimeout,
		TLSHandshakeTimeout:   defaultTLSHandshake,
		ExpectContinueTimeout: defaultExpectContinue,
		ForceAttemptHTTP2:     true,
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
