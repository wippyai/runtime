package httpclient

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// clientKey represents a unique configuration for an HTTP client
type clientKey struct {
	timeout    time.Duration
	unixSocket string
}

// String returns a string representation of the clientKey for debugging
func (k clientKey) String() string {
	if k.unixSocket != "" {
		return fmt.Sprintf("unix:%s,timeout:%v", k.unixSocket, k.timeout)
	}
	return fmt.Sprintf("tcp,timeout:%v", k.timeout)
}

// clientOnce holds a sync.Once and the resulting client to ensure single creation
type clientOnce struct {
	once   sync.Once
	client Client
	err    error
}

// clientPool manages a pool of HTTP clients using lock-free operations
type clientPool struct {
	clients       sync.Map // map[clientKey]*clientOnce
	defaultClient Client
}

// newClientPool creates a new lock-free client pool with the given default client
func newClientPool(defaultClient Client) *clientPool {
	return &clientPool{
		defaultClient: defaultClient,
	}
}

// GetClient returns an appropriate HTTP client for the given configuration
// Uses lock-free operations and ensures no duplicate clients are created
func (p *clientPool) GetClient(timeout time.Duration, unixSocket string) Client {
	// Use default client for standard cases
	if unixSocket == "" && timeout <= 90*time.Second {
		return p.defaultClient
	}

	key := clientKey{
		timeout:    timeout,
		unixSocket: unixSocket,
	}

	// Load or store a clientOnce for this key
	value, _ := p.clients.LoadOrStore(key, &clientOnce{})
	clientOncePtr := value.(*clientOnce)

	// Use sync.Once to ensure the client is created exactly once
	clientOncePtr.once.Do(func() {
		clientOncePtr.client = p.createClient(timeout, unixSocket)
	})

	return clientOncePtr.client
}

// createClient creates a new HTTP client with the specified configuration
func (p *clientPool) createClient(timeout time.Duration, unixSocket string) Client {
	transport := &http.Transport{
		IdleConnTimeout:       timeout + 30*time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   2,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	// Configure Unix socket dialing if specified
	if unixSocket != "" {
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialer := &net.Dialer{}
			return dialer.DialContext(ctx, "unix", unixSocket)
		}
	}

	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}
}
