// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"math"
	"net"
	gohttp "net/http"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	netapi "github.com/wippyai/runtime/api/net"
	httpapi "github.com/wippyai/runtime/api/service/http"
	lru "github.com/wippyai/runtime/internal/cache"
)

// clientKey identifies a unique client configuration. networkIdentity is a
// stable handle on the underlying overlay Service (its pointer address) so
// a driver hot-swap produces a new cache entry instead of returning an old
// client whose Transport still holds the closed service's DialContext.
type clientKey struct {
	unixSocket      string
	tlsFingerprint  string
	networkID       string
	networkIdentity uintptr
	timeout         time.Duration
}

// clientOnce ensures single initialization of a client. client is stored via
// an atomic pointer so concurrent evictors can safely inspect it without
// participating in once.Do — an evictor that hits the race between insert
// and init simply observes a nil pointer and skips cleanup (the stale
// transport's idle conns will still be GC'd via IdleConnTimeout).
type clientOnce struct {
	client atomic.Pointer[gohttp.Client]
	err    error
	once   sync.Once
}

// Pool provides pooled HTTP clients with proper connection reuse. Entries are
// held in a bounded LRU (backed by internal/cache) so long-running processes
// don't accumulate transports across many distinct TLS / unix-socket / overlay
// keys. SSRF protection happens at runtime level via security policies.
type Pool struct {
	defaultClient *gohttp.Client
	cache         *lru.Cache[clientKey, *clientOnce]
	overlayKeys   map[string]clientKey
	mu            sync.Mutex
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
	return newPool(defaultTimeout, defaultMaxIdleConns, defaultMaxIdlePerHost, defaultIdleConnTimeout, 0)
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
	return newPool(timeout, maxIdle, maxPerHost, idleTimeout, cfg.MaxClients)
}

// newPool constructs a Pool wired to internal/cache with an OnEvict callback
// that closes idle transports and prunes the overlay index. maxClients == 0
// leaves the pool effectively unbounded.
func newPool(timeout time.Duration, maxIdle, maxPerHost int, idleTimeout time.Duration, maxClients int) *Pool {
	p := &Pool{
		defaultClient: createClient(timeout, "", maxIdle, maxPerHost, idleTimeout),
		overlayKeys:   make(map[string]clientKey),
	}

	capacity := maxClients
	if capacity <= 0 {
		capacity = math.MaxInt32
	}

	p.cache = lru.New[clientKey, *clientOnce](
		lru.WithCapacity(capacity),
		lru.WithOnEvict(func(k clientKey, co *clientOnce) {
			closeIdle(co)
			if k.networkID != "" {
				if cur, ok := p.overlayKeys[k.networkID]; ok && cur == k {
					delete(p.overlayKeys, k.networkID)
				}
			}
		}),
	)
	return p
}

// getOrCreate looks up or inserts the clientOnce for key. On a hit the entry
// is promoted to MRU via cache.Get. On a miss a new clientOnce is inserted;
// the underlying cache enforces capacity and fires the OnEvict callback for
// any entry it displaces (closing idle connections). The returned clientOnce
// is not yet initialized — callers run build under co.once.Do outside the
// pool lock. If overlayNetworkID is non-empty, any stale entry for that
// network (previous identity) is deleted first, all under one lock so a
// concurrent caller with an even older identity can't evict the just-
// inserted entry.
func (p *Pool) getOrCreate(key clientKey, overlayNetworkID string) *clientOnce {
	p.mu.Lock()
	defer p.mu.Unlock()

	if overlayNetworkID != "" {
		if prev, ok := p.overlayKeys[overlayNetworkID]; ok && prev != key {
			p.cache.Delete(prev)
		}
	}

	if co, ok := p.cache.Get(key); ok {
		return co
	}

	co := &clientOnce{}
	_ = p.cache.Set(key, co)
	if overlayNetworkID != "" {
		p.overlayKeys[overlayNetworkID] = key
	}
	return co
}

// closeIdle closes idle connections on co's client transport if co has been
// initialized. Safe to call on a never-initialized clientOnce and from any
// goroutine — the atomic Load synchronizes with the Store inside once.Do.
func closeIdle(co *clientOnce) {
	if co == nil {
		return
	}
	c := co.client.Load()
	if c == nil {
		return
	}
	if tr, ok := c.Transport.(*gohttp.Transport); ok {
		tr.CloseIdleConnections()
	}
}

// GetClient returns a pooled client for the given configuration.
// Uses default client when possible to maximize connection reuse.
func (p *Pool) GetClient(timeout time.Duration, unixSocket string) *gohttp.Client {
	if unixSocket == "" && (timeout <= 0 || timeout == defaultTimeout) {
		return p.defaultClient
	}

	if timeout <= 0 {
		timeout = defaultTimeout
	}

	key := clientKey{timeout: timeout, unixSocket: unixSocket}
	co := p.getOrCreate(key, "")
	co.once.Do(func() {
		co.client.Store(createClient(timeout, unixSocket, defaultMaxIdleConns, defaultMaxIdlePerHost, defaultIdleConnTimeout))
	})
	return co.client.Load()
}

// GetClientWithDialer returns a pooled client that dials through svc's
// DialContext. The cache key includes the address of svc so a hot-swap of
// the overlay network (Manager.Update) produces a new pool entry instead of
// reusing a client whose Transport still references the closed service.
// Stale entries for the same networkID (previous identity) are evicted on
// the miss path, which keeps the cache bounded to one live client per
// networkID after any number of hot-swaps.
func (p *Pool) GetClientWithDialer(timeout time.Duration, networkID string, svc netapi.Service) *gohttp.Client {
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	key := clientKey{
		timeout:         timeout,
		networkID:       networkID,
		networkIdentity: serviceIdentity(svc),
	}

	co := p.getOrCreate(key, networkID)
	co.once.Do(func() {
		co.client.Store(createClientWithDialer(timeout, svc.DialContext))
	})
	return co.client.Load()
}

// serviceIdentity returns a stable handle on the concrete value behind a
// netapi.Service interface — the address of the underlying struct. Two
// different Service instances always get different identities, even if
// they happen to be registered under the same overlay ID.
func serviceIdentity(svc netapi.Service) uintptr {
	if svc == nil {
		return 0
	}
	v := reflect.ValueOf(svc)
	if v.Kind() == reflect.Pointer {
		return v.Pointer()
	}
	return 0
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

	co := p.getOrCreate(key, "")
	co.once.Do(func() {
		tlsCfg, err := buildTLSConfig(cfg)
		if err != nil {
			co.err = err
			return
		}
		co.client.Store(createClientFromTLS(timeout, unixSocket, tlsCfg))
	})
	if co.err != nil {
		return nil, co.err
	}
	return co.client.Load(), nil
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
	return p.cache.Len()
}

// createClientWithDialer builds an HTTP client with a custom DialContext function.
func createClientWithDialer(timeout time.Duration, dialFn func(ctx context.Context, network, addr string) (net.Conn, error)) *gohttp.Client {
	transport := &gohttp.Transport{
		MaxIdleConns:          defaultMaxIdleConns,
		MaxIdleConnsPerHost:   defaultMaxIdlePerHost,
		IdleConnTimeout:       defaultIdleConnTimeout,
		TLSHandshakeTimeout:   defaultTLSHandshake,
		ExpectContinueTimeout: defaultExpectContinue,
		ForceAttemptHTTP2:     false,
		DialContext:           dialFn,
	}
	return &gohttp.Client{
		Transport: transport,
		Timeout:   timeout,
	}
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
