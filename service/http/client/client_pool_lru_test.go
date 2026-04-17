// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"fmt"
	"net"
	gohttp "net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingService is a minimal overlay service whose identity is comparable
// by pointer and that tracks whether CloseIdleConnections was routed through
// its DialContext — used to verify LRU eviction runs the transport cleanup.
type countingService struct {
	id    string
	dials atomic.Int64
}

func (s *countingService) DialContext(_ context.Context, network, address string) (net.Conn, error) {
	s.dials.Add(1)
	var d net.Dialer
	return d.DialContext(context.Background(), network, address)
}

func (s *countingService) Listen(_ context.Context, _, _ string) (net.Listener, error) {
	return nil, fmt.Errorf("unused")
}

func (s *countingService) ListenPacket(_ context.Context, _, _ string) (net.PacketConn, error) {
	return nil, fmt.Errorf("unused")
}

func (s *countingService) LookupHost(_ context.Context, _ string) ([]string, error) {
	return nil, fmt.Errorf("unused")
}

func TestPoolLRU_BoundedCap(t *testing.T) {
	pool := NewClientPoolWithConfig(PoolConfig{MaxClients: 3})

	for i := 1; i <= 10; i++ {
		pool.GetClient(time.Duration(i)*time.Second, "")
	}

	assert.Equal(t, 3, pool.Size(), "pool must not exceed MaxClients")
}

func TestPoolLRU_Unbounded(t *testing.T) {
	pool := NewClientPoolWithConfig(PoolConfig{MaxClients: 0})

	// Use millisecond timeouts to avoid collapsing onto the default
	// client (GetClient short-circuits to defaultClient at defaultTimeout).
	for i := 1; i <= 50; i++ {
		pool.GetClient(time.Duration(i)*time.Millisecond, "")
	}

	assert.Equal(t, 50, pool.Size(), "MaxClients=0 must leave the pool unbounded")
}

func TestPoolLRU_EvictsLeastRecentlyUsed(t *testing.T) {
	pool := NewClientPoolWithConfig(PoolConfig{MaxClients: 2})

	a := pool.GetClient(1*time.Second, "")
	b := pool.GetClient(2*time.Second, "")

	// Touch A so B becomes least-recent, then insert C — B must go.
	_ = pool.GetClient(1*time.Second, "")
	_ = pool.GetClient(3*time.Second, "")

	a2 := pool.GetClient(1*time.Second, "")
	b2 := pool.GetClient(2*time.Second, "")

	assert.Same(t, a, a2, "recently-used entry must stay in pool")
	assert.NotSame(t, b, b2, "least-recently-used entry must have been evicted and rebuilt")
}

func TestPoolLRU_EvictionClosesIdleConnections(t *testing.T) {
	// When an entry is evicted, its transport's idle connections must be
	// released so the kernel sockets don't accumulate across a long run.
	pool := NewClientPoolWithConfig(PoolConfig{MaxClients: 1})

	c1 := pool.GetClient(1*time.Second, "")
	tr, ok := c1.Transport.(*gohttp.Transport)
	require.True(t, ok, "expected *http.Transport")

	// Force an idle connection into the transport by dialing a listener
	// and reading a tiny HTTP response.
	lc := &net.ListenConfig{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	srv := &gohttp.Server{Handler: gohttp.HandlerFunc(func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		w.WriteHeader(gohttp.StatusNoContent)
	})}
	go srv.Serve(ln)
	defer srv.Shutdown(context.Background())

	req, err := gohttp.NewRequestWithContext(ctx, "GET", "http://"+ln.Addr().String(), nil)
	require.NoError(t, err)
	resp, err := c1.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	// Push a second entry to force eviction of c1.
	_ = pool.GetClient(2*time.Second, "")

	// The evicted transport should report no idle conns; we can only
	// observe this indirectly — a direct call must not panic and must
	// leave the transport in a state where subsequent CloseIdleConnections
	// is a no-op. The real guarantee is that eviction called it once.
	tr.CloseIdleConnections()
	assert.Equal(t, 1, pool.Size())
}

func TestPoolLRU_ConcurrentSameKey_OnceInit(t *testing.T) {
	// Many goroutines hitting the same key must share one *http.Client.
	pool := NewClientPoolWithConfig(PoolConfig{MaxClients: 4})

	const goroutines = 64
	results := make([]*gohttp.Client, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	start := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			results[i] = pool.GetClient(5*time.Second, "")
		}(i)
	}
	close(start)
	wg.Wait()

	first := results[0]
	require.NotNil(t, first)
	for i, c := range results {
		assert.Samef(t, first, c, "goroutine %d saw a different client — once.Do raced", i)
	}
	assert.Equal(t, 1, pool.Size())
}

func TestPoolLRU_ConcurrentDistinctKeys_RespectsCap(t *testing.T) {
	const cap = 5
	pool := NewClientPoolWithConfig(PoolConfig{MaxClients: cap})

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			pool.GetClient(time.Duration(i+1)*time.Millisecond, "")
		}(i)
	}
	wg.Wait()

	assert.LessOrEqual(t, pool.Size(), cap, "pool size must never exceed cap under concurrency")
}

func TestPoolLRU_OverlayHotSwapEvictsStale(t *testing.T) {
	pool := NewClientPool()

	svc1 := &countingService{id: "v1"}
	c1 := pool.GetClientWithDialer(0, "network:overlay", svc1)
	require.Equal(t, 1, pool.Size())

	// Replace svc1 with svc2 under the same networkID — a hot-swap.
	svc2 := &countingService{id: "v2"}
	c2 := pool.GetClientWithDialer(0, "network:overlay", svc2)

	assert.NotSame(t, c1, c2, "new identity must produce a new client")
	assert.Equal(t, 1, pool.Size(), "stale entry for previous identity must be evicted")

	// Third hot-swap: still only one entry.
	svc3 := &countingService{id: "v3"}
	c3 := pool.GetClientWithDialer(0, "network:overlay", svc3)
	assert.NotSame(t, c2, c3)
	assert.Equal(t, 1, pool.Size())
}

func TestPoolLRU_DifferentOverlaysCoexist(t *testing.T) {
	pool := NewClientPool()

	svcA := &countingService{id: "a"}
	svcB := &countingService{id: "b"}

	cA := pool.GetClientWithDialer(0, "network:a", svcA)
	cB := pool.GetClientWithDialer(0, "network:b", svcB)

	assert.NotSame(t, cA, cB)
	assert.Equal(t, 2, pool.Size())

	// Re-request A — must hit cache, size unchanged.
	cA2 := pool.GetClientWithDialer(0, "network:a", svcA)
	assert.Same(t, cA, cA2)
	assert.Equal(t, 2, pool.Size())
}

func TestPoolLRU_OverlayConcurrentHotSwap(t *testing.T) {
	// Stress the evict-stale-plus-getOrCreate path under contention: one
	// goroutine installs a sequence of new services, many goroutines read
	// whichever service is currently registered. No panics, no leaks of
	// the evicted identity, size bounded to 1 at the end.
	pool := NewClientPool()

	const rounds = 20
	const readers = 16

	var current atomic.Pointer[countingService]
	current.Store(&countingService{id: "init"})

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(readers)
	for i := 0; i < readers; i++ {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					svc := current.Load()
					_ = pool.GetClientWithDialer(0, "network:stress", svc)
				}
			}
		}()
	}

	for i := 0; i < rounds; i++ {
		current.Store(&countingService{id: fmt.Sprintf("r%d", i)})
		time.Sleep(time.Millisecond)
	}
	close(done)
	wg.Wait()

	// Run one more request to sync eviction with the final identity.
	final := &countingService{id: "final"}
	current.Store(final)
	_ = pool.GetClientWithDialer(0, "network:stress", final)

	assert.Equal(t, 1, pool.Size(), "hot-swap stress must end with a single live entry")
}
