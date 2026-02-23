// SPDX-License-Identifier: MPL-2.0

package clock

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	clockapi "github.com/wippyai/runtime/api/clock"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

type tickerMockNode struct {
	sendErr  error
	packages []*relay.Package
	mu       sync.Mutex
}

func (m *tickerMockNode) Send(pkg *relay.Package) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendErr != nil {
		return m.sendErr
	}
	m.packages = append(m.packages, pkg)
	return nil
}

func (m *tickerMockNode) ID() pid.NodeID                                    { return "" }
func (m *tickerMockNode) RegisterHost(_ pid.HostID, _ relay.Receiver) error { return nil }
func (m *tickerMockNode) UnregisterHost(_ pid.HostID)                       {}
func (m *tickerMockNode) GetHost(_ pid.HostID) (relay.Receiver, bool)       { return nil, false }
func (m *tickerMockNode) Attach(_ pid.PID, _ chan *relay.Package) (context.CancelFunc, error) {
	return func() {}, nil
}
func (m *tickerMockNode) Detach(_ pid.PID) {}

func (m *tickerMockNode) packagesReceived() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.packages)
}

func waitForPackages(t *testing.T, node *tickerMockNode, min int, timeout time.Duration) int {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		received := node.packagesReceived()
		if received >= min {
			return received
		}
		if time.Now().After(deadline) {
			return received
		}
		time.Sleep(1 * time.Millisecond)
	}
}

func TestTickerRegistry_New(t *testing.T) {
	r := newTickerRegistry()
	if r == nil {
		t.Fatal("expected non-nil registry")
	}
	if r.count() != 0 {
		t.Errorf("expected 0 tickers, got %d", r.count())
	}
}

func TestTickerRegistry_Start(t *testing.T) {
	r := newTickerRegistry()
	defer r.close()

	ctx := context.Background()
	node := &tickerMockNode{}
	testPID := pid.PID{Node: "test-node", Host: "test-host", UniqID: "test-id"}

	id := r.start(ctx, 10*time.Millisecond, testPID, "tick-topic", node)

	if id == 0 {
		t.Error("expected non-zero ticker ID")
	}
	if r.count() != 1 {
		t.Errorf("expected 1 ticker, got %d", r.count())
	}

	received := waitForPackages(t, node, 2, 100*time.Millisecond)
	if received < 2 {
		t.Errorf("expected at least 2 tick packages, got %d", received)
	}
}

func TestTickerRegistry_Stop(t *testing.T) {
	r := newTickerRegistry()
	defer r.close()

	ctx := context.Background()
	node := &tickerMockNode{}
	testPID := pid.PID{Node: "test-node", Host: "test-host", UniqID: "test-id"}

	id := r.start(ctx, time.Hour, testPID, "tick-topic", node)

	err := r.stop(id)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if r.count() != 0 {
		t.Errorf("expected 0 tickers, got %d", r.count())
	}
}

func TestTickerRegistry_StopNotFound(t *testing.T) {
	r := newTickerRegistry()
	defer r.close()

	err := r.stop(999)
	if !errors.Is(err, clockapi.ErrTickerNotFound) {
		t.Errorf("expected ErrTickerNotFound, got %v", err)
	}
}

func TestTickerRegistry_StopTwice(t *testing.T) {
	r := newTickerRegistry()
	defer r.close()

	ctx := context.Background()
	node := &tickerMockNode{}
	testPID := pid.PID{Node: "test-node", Host: "test-host", UniqID: "test-id"}

	id := r.start(ctx, time.Hour, testPID, "tick-topic", node)

	err := r.stop(id)
	if err != nil {
		t.Errorf("first stop: unexpected error: %v", err)
	}

	err = r.stop(id)
	if !errors.Is(err, clockapi.ErrTickerNotFound) {
		t.Errorf("second stop: expected ErrTickerNotFound, got %v", err)
	}
}

func TestTickerRegistry_Close(t *testing.T) {
	r := newTickerRegistry()

	ctx := context.Background()
	node := &tickerMockNode{}
	testPID := pid.PID{Node: "test-node", Host: "test-host", UniqID: "test-id"}

	for i := 0; i < 10; i++ {
		r.start(ctx, time.Hour, testPID, "tick-topic", node)
	}

	if r.count() != 10 {
		t.Errorf("expected 10 tickers, got %d", r.count())
	}

	r.close()

	if r.count() != 0 {
		t.Errorf("expected 0 tickers after close, got %d", r.count())
	}
}

func TestTickerRegistry_Concurrent(t *testing.T) {
	r := newTickerRegistry()
	defer r.close()

	ctx := context.Background()
	testPID := pid.PID{Node: "test-node", Host: "test-host", UniqID: "test-id"}

	const goroutines = 10
	const tickersPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			node := &tickerMockNode{}
			for j := 0; j < tickersPerGoroutine; j++ {
				id := r.start(ctx, time.Hour, testPID, "tick-topic", node)
				_ = r.stop(id)
			}
		}()
	}

	assert.NotPanics(t, func() {
		wg.Wait()
	})
}

func TestTickerRegistry_GetShard(t *testing.T) {
	r := newTickerRegistry()

	shard1 := r.getShard(1)
	shard2 := r.getShard(65)

	if shard1 != shard2 {
		t.Error("expected same shard for IDs 1 and 65 (mod 64)")
	}

	shard3 := r.getShard(2)
	if shard1 == shard3 {
		t.Error("expected different shards for IDs 1 and 2")
	}
}

func TestTickerRegistry_ContextCancel(t *testing.T) {
	r := newTickerRegistry()
	defer r.close()

	ctx, cancel := context.WithCancel(context.Background())
	node := &tickerMockNode{}
	testPID := pid.PID{Node: "test-node", Host: "test-host", UniqID: "test-id"}

	id := r.start(ctx, 10*time.Millisecond, testPID, "tick-topic", node)

	// Wait for first tick deterministically to avoid scheduler jitter flakiness.
	beforeCancel := waitForPackages(t, node, 1, 200*time.Millisecond)
	if beforeCancel == 0 {
		t.Error("expected at least one tick before cancel")
	}

	// Cancel context
	cancel()

	// Allow at most one in-flight tick, then require count to stabilize.
	baseline := node.packagesReceived()
	last := baseline
	stableSince := time.Now()
	const stableWindow = 40 * time.Millisecond
	deadline := time.Now().Add(250 * time.Millisecond)

	for time.Now().Before(deadline) {
		current := node.packagesReceived()
		if current != last {
			last = current
			stableSince = time.Now()
		}
		if time.Since(stableSince) >= stableWindow {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	if time.Since(stableSince) < stableWindow {
		t.Fatalf("ticker did not stabilize after cancel; last=%d baseline=%d", last, baseline)
	}
	if last > baseline+1 {
		t.Errorf("expected at most one in-flight tick after cancel, got %d", last-baseline)
	}

	// Entry still exists in registry (context cancel doesn't remove it)
	if r.count() != 1 {
		t.Errorf("expected 1 ticker still in registry, got %d", r.count())
	}

	// Stop should still work
	err := r.stop(id)
	if err != nil {
		t.Errorf("unexpected error on stop: %v", err)
	}
}

func TestTickerRegistry_MultipleTickers(t *testing.T) {
	r := newTickerRegistry()
	defer r.close()

	ctx := context.Background()
	testPID := pid.PID{Node: "test-node", Host: "test-host", UniqID: "test-id"}

	var ids []uint64
	var nodes []*tickerMockNode

	for i := 0; i < 5; i++ {
		node := &tickerMockNode{}
		nodes = append(nodes, node)
		id := r.start(ctx, 10*time.Millisecond, testPID, "tick-topic", node)
		ids = append(ids, id)
	}

	if r.count() != 5 {
		t.Errorf("expected 5 tickers, got %d", r.count())
	}

	// Each ticker should have received ticks independently
	for i, node := range nodes {
		received := waitForPackages(t, node, 2, 100*time.Millisecond)
		if received < 2 {
			t.Errorf("ticker %d: expected at least 2 ticks, got %d", i, received)
		}
	}

	// Stop each ticker
	for _, id := range ids {
		if err := r.stop(id); err != nil {
			t.Errorf("unexpected error stopping ticker: %v", err)
		}
	}

	if r.count() != 0 {
		t.Errorf("expected 0 tickers after stopping all, got %d", r.count())
	}
}

func TestTickerRegistry_TickPackageContents(t *testing.T) {
	r := newTickerRegistry()
	defer r.close()

	ctx := context.Background()
	node := &tickerMockNode{}
	testPID := pid.PID{Node: "test-node", Host: "test-host", UniqID: "test-id"}
	topic := "test-tick-topic"

	r.start(ctx, 10*time.Millisecond, testPID, topic, node)

	waitForPackages(t, node, 1, 100*time.Millisecond)

	node.mu.Lock()
	defer node.mu.Unlock()

	if len(node.packages) == 0 {
		t.Fatal("expected at least one package")
	}

	pkg := node.packages[0]
	if pkg.Target != testPID {
		t.Errorf("expected package to PID %v, got %v", testPID, pkg.Target)
	}
	if len(pkg.Messages) == 0 || pkg.Messages[0].Topic != topic {
		t.Errorf("expected topic %q in messages", topic)
	}
}

func TestTickerRegistry_ForwardTicksStopsOnClose(t *testing.T) {
	r := newTickerRegistry()

	ctx := context.Background()
	node := &tickerMockNode{}
	testPID := pid.PID{Node: "test-node", Host: "test-host", UniqID: "test-id"}

	var tickCount atomic.Int32
	wrappedNode := &tickerCountingNode{
		node:   node,
		counts: &tickCount,
	}

	r.start(ctx, 5*time.Millisecond, testPID, "tick-topic", wrappedNode)

	// Wait for some ticks
	time.Sleep(20 * time.Millisecond)
	countBefore := tickCount.Load()

	// Close the registry
	r.close()

	// Wait and verify no more ticks
	time.Sleep(20 * time.Millisecond)
	countAfter := tickCount.Load()

	// Allow for at most 1 additional tick that might have been in flight
	if countAfter > countBefore+1 {
		t.Errorf("expected ticks to stop after close, got %d more ticks", countAfter-countBefore)
	}
}

type tickerCountingNode struct {
	node   *tickerMockNode
	counts *atomic.Int32
}

func (c *tickerCountingNode) Send(pkg *relay.Package) error {
	c.counts.Add(1)
	return c.node.Send(pkg)
}

func (c *tickerCountingNode) ID() pid.NodeID                                    { return "" }
func (c *tickerCountingNode) RegisterHost(_ pid.HostID, _ relay.Receiver) error { return nil }
func (c *tickerCountingNode) UnregisterHost(_ pid.HostID)                       {}
func (c *tickerCountingNode) GetHost(_ pid.HostID) (relay.Receiver, bool)       { return nil, false }
func (c *tickerCountingNode) Attach(_ pid.PID, _ chan *relay.Package) (context.CancelFunc, error) {
	return func() {}, nil
}
func (c *tickerCountingNode) Detach(_ pid.PID) {}
