package clock

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	clockapi "github.com/wippyai/runtime/api/clock"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
)

type mockNode struct {
	mu       sync.Mutex
	packages []*relay.Package
}

func (m *mockNode) Send(pkg *relay.Package) error {
	m.mu.Lock()
	m.packages = append(m.packages, pkg)
	m.mu.Unlock()
	return nil
}

func (m *mockNode) ID() relay.NodeID                                { return "" }
func (m *mockNode) RegisterHost(_ relay.HostID, _ relay.Host) error { return nil }
func (m *mockNode) UnregisterHost(_ relay.HostID)                   {}
func (m *mockNode) GetHost(_ relay.HostID) (relay.Host, bool)       { return nil, false }
func (m *mockNode) Attach(_ relay.PID, _ chan *relay.Package) (context.CancelFunc, error) {
	return func() {}, nil
}
func (m *mockNode) Detach(_ relay.PID) {}

func (m *mockNode) getPackages() []*relay.Package {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.packages
}

func TestTickerRegistry(t *testing.T) {
	r := NewTickerRegistry()
	defer r.Close()

	ctx := context.Background()
	node := &mockNode{}
	pid := relay.PID{}

	id := r.Start(ctx, 10*time.Millisecond, pid, "test", node)
	if id != 1 {
		t.Errorf("expected first ID to be 1, got %d", id)
	}

	id2 := r.Start(ctx, 20*time.Millisecond, pid, "test", node)
	if id2 != 2 {
		t.Errorf("expected second ID to be 2, got %d", id2)
	}
}

func TestTickerRegistrySendsTicks(t *testing.T) {
	r := NewTickerRegistry()
	defer r.Close()

	ctx := context.Background()
	node := &mockNode{}
	targetPID := relay.PID{Host: "test", UniqID: "1"}
	topic := "ticker@1"

	r.Start(ctx, 5*time.Millisecond, targetPID, topic, node)

	time.Sleep(20 * time.Millisecond)

	packages := node.getPackages()
	if len(packages) == 0 {
		t.Error("expected at least one tick package")
	}

	for _, pkg := range packages {
		if pkg.Target != targetPID {
			t.Errorf("expected Target=%v, got %v", targetPID, pkg.Target)
		}
		if len(pkg.Messages) != 1 {
			t.Errorf("expected 1 message, got %d", len(pkg.Messages))
			continue
		}
		if pkg.Messages[0].Topic != relay.Topic(topic) {
			t.Errorf("expected topic=%q, got %q", topic, pkg.Messages[0].Topic)
		}
		if len(pkg.Messages[0].Payloads) != 1 {
			t.Errorf("expected 1 payload, got %d", len(pkg.Messages[0].Payloads))
			continue
		}
		p := pkg.Messages[0].Payloads[0]
		if p.Format() != payload.Golang {
			t.Errorf("expected Golang format, got %v", p.Format())
		}
		nsec, ok := p.Data().(int64)
		if !ok {
			t.Errorf("expected int64 data, got %T", p.Data())
		}
		if nsec <= 0 {
			t.Error("expected positive nanoseconds")
		}
	}
}

func TestTickerRegistryStop(t *testing.T) {
	r := NewTickerRegistry()

	ctx := context.Background()
	node := &mockNode{}
	pid := relay.PID{}

	id := r.Start(ctx, 10*time.Millisecond, pid, "test", node)

	err := r.Stop(id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = r.Stop(id)
	if !errors.Is(err, ErrTickerNotFound) {
		t.Errorf("expected ErrTickerNotFound on double stop, got %v", err)
	}
}

func TestTickerRegistryClose(t *testing.T) {
	r := NewTickerRegistry()

	ctx := context.Background()
	node := &mockNode{}
	pid := relay.PID{}

	r.Start(ctx, 10*time.Millisecond, pid, "test", node)
	r.Start(ctx, 10*time.Millisecond, pid, "test", node)
	r.Start(ctx, 10*time.Millisecond, pid, "test", node)

	r.Close()

	if count := r.Count(); count != 0 {
		t.Errorf("expected 0 tickers after close, got %d", count)
	}
}

func getTickerHandlers(_ *testing.T) (start, stop dispatcher.Handler, cleanup func()) {
	d := NewDispatcher()
	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})
	return handlers[clockapi.TickerStart],
		handlers[clockapi.TickerStop],
		func() { _ = d.Stop(context.Background()) }
}

func setupTestContext() context.Context {
	ctx := ctxapi.NewRootContext()
	node := &mockNode{}
	return relay.WithNode(ctx, node)
}

func TestTickerStartHandler(t *testing.T) {
	ctx := setupTestContext()
	startH, _, cleanup := getTickerHandlers(t)
	defer cleanup()

	var emitted any
	err := startH.Handle(ctx, clockapi.TickerStartCmd{
		Duration: 10 * time.Millisecond,
		PID:      relay.PID{Host: "test", UniqID: "1"},
		Topic:    "ticker@1",
	}, 0, &testReceiver{fn: func(data any, _ error) {
		emitted = data
	}})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, ok := emitted.(clockapi.TickerStartResult)
	if !ok {
		t.Fatalf("expected TickerStartResult, got %T", emitted)
	}
	if result.ID != 1 {
		t.Errorf("expected ID 1, got %d", result.ID)
	}
	if result.Stop == nil {
		t.Error("expected Stop callback to be set")
	}
}

func TestTickerStopHandler(t *testing.T) {
	ctx := setupTestContext()
	startH, stopH, cleanup := getTickerHandlers(t)
	defer cleanup()

	var tickerID uint64
	_ = startH.Handle(ctx, clockapi.TickerStartCmd{
		Duration: 10 * time.Millisecond,
		PID:      relay.PID{Host: "test", UniqID: "1"},
		Topic:    "ticker@1",
	}, 0, &testReceiver{fn: func(data any, _ error) {
		tickerID = data.(clockapi.TickerStartResult).ID
	}})

	var emitted bool
	err := stopH.Handle(ctx, clockapi.TickerStopCmd{TickerID: tickerID}, 0, &testReceiver{fn: func(_ any, _ error) {
		emitted = true
	}})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !emitted {
		t.Error("expected emit on stop")
	}
}

func TestTickerStartHandlerInvalidDuration(t *testing.T) {
	ctx := setupTestContext()
	startH, _, cleanup := getTickerHandlers(t)
	defer cleanup()

	var emitted bool
	err := startH.Handle(ctx, clockapi.TickerStartCmd{Duration: 0}, 0, &testReceiver{fn: func(_ any, _ error) {
		emitted = true
	}})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if emitted {
		t.Error("expected no emit for zero duration")
	}

	err = startH.Handle(ctx, clockapi.TickerStartCmd{Duration: -time.Second}, 0, &testReceiver{fn: func(_ any, _ error) {
		emitted = true
	}})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if emitted {
		t.Error("expected no emit for negative duration")
	}
}

func TestTickerRegistryScalability(t *testing.T) {
	const numTickers = 10000

	registry := NewTickerRegistry()
	defer registry.Close()

	ctx := context.Background()
	node := &mockNode{}
	pid := relay.PID{}

	ids := make([]uint64, numTickers)
	start := time.Now()
	for i := 0; i < numTickers; i++ {
		ids[i] = registry.Start(ctx, time.Hour, pid, "test", node)
	}
	createTime := time.Since(start)
	t.Logf("Created %d tickers in %v", numTickers, createTime)

	if count := registry.Count(); count != numTickers {
		t.Errorf("expected %d tickers, got %d", numTickers, count)
	}

	start = time.Now()
	var wg sync.WaitGroup
	for i := 0; i < numTickers; i++ {
		wg.Add(1)
		go func(id uint64) {
			defer wg.Done()
			_ = registry.Stop(id)
		}(ids[i])
	}
	wg.Wait()
	stopTime := time.Since(start)
	t.Logf("Stopped %d tickers in %v", numTickers, stopTime)

	if remaining := registry.Count(); remaining != 0 {
		t.Errorf("expected 0 tickers after stop, got %d", remaining)
	}
}

func TestTickerRegistryConcurrentOperations(t *testing.T) {
	const goroutines = 100
	const opsPerGoroutine = 100

	registry := NewTickerRegistry()
	defer registry.Close()

	ctx := context.Background()
	node := &mockNode{}
	pid := relay.PID{}

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids := make([]uint64, 0, opsPerGoroutine)

			for i := 0; i < opsPerGoroutine; i++ {
				id := registry.Start(ctx, time.Hour, pid, "test", node)
				ids = append(ids, id)
			}

			for _, id := range ids {
				_ = registry.Stop(id)
			}
		}()
	}

	wg.Wait()

	if remaining := registry.Count(); remaining != 0 {
		t.Errorf("expected 0 tickers after concurrent ops, got %d", remaining)
	}
}
