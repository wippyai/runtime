// SPDX-License-Identifier: MPL-2.0

package clock

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	clockapi "github.com/wippyai/runtime/api/clock"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

// Tests covering the router-aware additions to the clock dispatcher:
//
//   - TimerStart/TickerStart with ChID populate a reverse map so the
//     corresponding StopByChID command can cancel them.
//   - A StopByChID arriving before the matching Start tombstones the
//     (pid, epoch, chID) key; the late Start consumes the tombstone and
//     completes its yield with ErrStoppedBeforeStart instead of
//     scheduling the timer.
//   - The FireBuilder closure reads the live GenRef so a timer that
//     fires after BumpGen carries the updated gen.
//   - Reverse map entries are removed when the timer fires naturally
//     (auto-cleanup) and when the user issues an explicit StopCmd.

// mockReceiver captures the (data, err) tuple of CompleteYield calls so
// tests can assert dispatcher behavior without spinning up the whole
// process scheduler.
type mockReceiver struct {
	mu     sync.Mutex
	tag    uint64
	data   any
	err    error
	called chan struct{}
}

func newMockReceiver() *mockReceiver {
	return &mockReceiver{called: make(chan struct{}, 1)}
}

func (m *mockReceiver) CompleteYield(tag uint64, data any, err error) {
	m.mu.Lock()
	m.tag = tag
	m.data = data
	m.err = err
	m.mu.Unlock()
	select {
	case m.called <- struct{}{}:
	default:
	}
}

func (m *mockReceiver) await(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-m.called:
	case <-time.After(timeout):
		t.Fatal("CompleteYield never invoked")
	}
}

func (m *mockReceiver) snapshot() (any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.data, m.err
}

// capturingNode is a relay.Node that records every Send so tests can
// assert on the emitted packages.
type capturingNode struct {
	mu       sync.Mutex
	packages []*relay.Package
}

func newCapturingNode() *capturingNode { return &capturingNode{} }

func (n *capturingNode) Send(pkg *relay.Package) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.packages = append(n.packages, pkg)
	return nil
}

func (n *capturingNode) ID() pid.NodeID                                    { return "test-node" }
func (n *capturingNode) RegisterHost(_ pid.HostID, _ relay.Receiver) error { return nil }
func (n *capturingNode) UnregisterHost(_ pid.HostID)                       {}
func (n *capturingNode) GetHost(_ pid.HostID) (relay.Receiver, bool)       { return nil, false }
func (n *capturingNode) Attach(_ pid.PID, _ chan *relay.Package) (context.CancelFunc, error) {
	return func() {}, nil
}
func (n *capturingNode) Detach(_ pid.PID) {}

func (n *capturingNode) snapshot() []*relay.Package {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]*relay.Package, len(n.packages))
	copy(out, n.packages)
	return out
}

func newDispatcherWithCtx(t *testing.T) (*Dispatcher, *capturingNode, context.Context, context.CancelFunc) {
	t.Helper()
	d := NewDispatcher()
	node := newCapturingNode()
	appCtx := ctxapi.NewAppContext()
	ctx, cancel := context.WithCancel(ctxapi.WithAppContext(context.Background(), appCtx))
	ctx = relay.WithNode(ctx, node)
	if err := d.Start(ctx); err != nil {
		t.Fatalf("dispatcher Start: %v", err)
	}
	return d, node, ctx, cancel
}

func stopDispatcher(t *testing.T, d *Dispatcher, cancel context.CancelFunc) {
	t.Helper()
	cancel()
	if err := d.Stop(context.Background()); err != nil {
		t.Fatalf("dispatcher Stop: %v", err)
	}
}

func samplePID(uniq string) pid.PID {
	return pid.PID{Node: "test-node", Host: "test-host", UniqID: uniq}
}

// TestClock_StartInstallsReverseMap: a router-tagged TimerStart installs
// an entry in the dispatcher's reverse map keyed by (pid, epoch, chID).
func TestClock_StartInstallsReverseMap(t *testing.T) {
	d, _, ctx, cancel := newDispatcherWithCtx(t)
	defer stopDispatcher(t, d, cancel)

	rcv := newMockReceiver()
	build := func(at time.Time, gen uint64) payload.Payload {
		return payload.NewPayload(at.UnixNano(), payload.Golang)
	}
	cmd := clockapi.TimerStartCmd{
		PID:      samplePID("p1"),
		Topic:    "@pid/route",
		Duration: 24 * time.Hour, // never fires during test
		ChID:     7,
		Epoch:    3,
		Build:    build,
	}
	if err := d.handleTimerStart(ctx, cmd, 1, rcv); err != nil {
		t.Fatal(err)
	}
	rcv.await(t, time.Second)

	timers, _ := d.ReverseMapSize()
	if timers != 1 {
		t.Fatalf("expected 1 reverse-map entry after start, got %d", timers)
	}
}

// TestClock_StopByChIDCancelsTimer: with a Start already issued, a
// StopByChIDCmd cancels the timer and removes the reverse-map entry.
func TestClock_StopByChIDCancelsTimer(t *testing.T) {
	d, _, ctx, cancel := newDispatcherWithCtx(t)
	defer stopDispatcher(t, d, cancel)

	rcv := newMockReceiver()
	if err := d.handleTimerStart(ctx, clockapi.TimerStartCmd{
		PID:      samplePID("p1"),
		Topic:    "@pid/route",
		Duration: 24 * time.Hour,
		ChID:     11,
		Epoch:    1,
		Build:    func(at time.Time, gen uint64) payload.Payload { return nil },
	}, 1, rcv); err != nil {
		t.Fatal(err)
	}
	rcv.await(t, time.Second)

	stopRcv := newMockReceiver()
	if err := d.handleTimerStopByChID(ctx, clockapi.TimerStopByChIDCmd{
		TargetPID: samplePID("p1"),
		Epoch:     1,
		ChID:      11,
	}, 2, stopRcv); err != nil {
		t.Fatal(err)
	}
	stopRcv.await(t, time.Second)
	data, err := stopRcv.snapshot()
	if err != nil {
		t.Fatalf("stop returned error: %v", err)
	}
	if data != true {
		t.Errorf("stop should report stopped=true, got %v", data)
	}

	timers, _ := d.ReverseMapSize()
	if timers != 0 {
		t.Fatalf("reverse map should be empty after stop, got %d entries", timers)
	}
	pendingT, _ := d.PendingStopsSize()
	if pendingT != 0 {
		t.Fatalf("no tombstone should remain after a matched stop, got %d", pendingT)
	}
}

// TestClock_StopByChIDBeforeStartTombstones: a stop arriving before the
// matching start is tombstoned. The late start consumes the tombstone
// and completes with ErrStoppedBeforeStart.
func TestClock_StopByChIDBeforeStartTombstones(t *testing.T) {
	d, _, ctx, cancel := newDispatcherWithCtx(t)
	defer stopDispatcher(t, d, cancel)

	stopRcv := newMockReceiver()
	if err := d.handleTimerStopByChID(ctx, clockapi.TimerStopByChIDCmd{
		TargetPID: samplePID("late"),
		Epoch:     5,
		ChID:      99,
	}, 1, stopRcv); err != nil {
		t.Fatal(err)
	}
	stopRcv.await(t, time.Second)
	data, err := stopRcv.snapshot()
	if err != nil {
		t.Fatalf("stop returned error: %v", err)
	}
	if data != false {
		t.Errorf("stop with no live entry should report stopped=false, got %v", data)
	}
	pendingT, _ := d.PendingStopsSize()
	if pendingT != 1 {
		t.Fatalf("tombstone should be installed, got %d", pendingT)
	}

	// Now the late start arrives.
	startRcv := newMockReceiver()
	if err := d.handleTimerStart(ctx, clockapi.TimerStartCmd{
		PID:      samplePID("late"),
		Topic:    "@pid/route",
		Duration: time.Hour,
		ChID:     99,
		Epoch:    5,
		Build:    func(at time.Time, gen uint64) payload.Payload { return nil },
	}, 2, startRcv); err != nil {
		t.Fatal(err)
	}
	startRcv.await(t, time.Second)
	_, startErr := startRcv.snapshot()
	if !errors.Is(startErr, clockapi.ErrStoppedBeforeStart) {
		t.Fatalf("late start should return ErrStoppedBeforeStart, got %v", startErr)
	}

	pendingT, _ = d.PendingStopsSize()
	if pendingT != 0 {
		t.Fatalf("tombstone should be consumed by the late start, got %d", pendingT)
	}
	timers, _ := d.ReverseMapSize()
	if timers != 0 {
		t.Fatalf("no reverse-map entry should be installed for a refused start, got %d", timers)
	}
	if d.TimerCount() != 0 {
		t.Fatalf("no Go timer should be created for a refused start, got TimerCount=%d", d.TimerCount())
	}
}

// TestClock_FireUsesBuilder: when a router-tagged timer fires, the
// dispatcher invokes Build(at, gen) and sends the returned payload to
// the relay node.
func TestClock_FireUsesBuilder(t *testing.T) {
	d, node, ctx, cancel := newDispatcherWithCtx(t)
	defer stopDispatcher(t, d, cancel)

	var captured atomic.Uint64
	captured.Store(42)
	rcv := newMockReceiver()
	cmd := clockapi.TimerStartCmd{
		PID:      samplePID("fire"),
		Topic:    "@pid/route",
		Duration: 10 * time.Millisecond,
		ChID:     1,
		Epoch:    1,
		GenRef:   &captured,
		Build: func(at time.Time, gen uint64) payload.Payload {
			return payload.NewPayload(struct {
				At  int64
				Gen uint64
			}{at.UnixNano(), gen}, payload.Golang)
		},
	}
	if err := d.handleTimerStart(ctx, cmd, 1, rcv); err != nil {
		t.Fatal(err)
	}
	rcv.await(t, time.Second)

	time.Sleep(60 * time.Millisecond)
	pkgs := node.snapshot()
	if len(pkgs) == 0 {
		t.Fatal("timer never fired or never sent a package")
	}
	last := pkgs[len(pkgs)-1]
	if len(last.Messages) == 0 {
		t.Fatal("fired package has no messages")
	}
	if got := last.Messages[0].Topic; got != "@pid/route" {
		t.Errorf("expected topic '@pid/route', got %q", got)
	}
	if len(last.Messages[0].Payloads) == 0 {
		t.Fatal("fired message has no payloads")
	}
	dataAny := last.Messages[0].Payloads[0].Data()
	dat, ok := dataAny.(struct {
		At  int64
		Gen uint64
	})
	if !ok {
		t.Fatalf("expected builder struct payload, got %T", dataAny)
	}
	if dat.Gen != 42 {
		t.Errorf("Build should observe genRef.Load()=42, got %d", dat.Gen)
	}

	// Reverse map entry must be cleaned after natural fire.
	timers, _ := d.ReverseMapSize()
	if timers != 0 {
		t.Errorf("reverse map should be empty after natural fire, got %d", timers)
	}
}

// TestClock_LegacyStartWithoutChIDStillWorks: a TimerStartCmd without
// router fields (ChID == 0) goes through the legacy int64-nanos path
// and never installs a reverse-map entry. Backward compat for workflow
// timers.
func TestClock_LegacyStartWithoutChIDStillWorks(t *testing.T) {
	d, node, ctx, cancel := newDispatcherWithCtx(t)
	defer stopDispatcher(t, d, cancel)

	rcv := newMockReceiver()
	if err := d.handleTimerStart(ctx, clockapi.TimerStartCmd{
		PID:      samplePID("legacy"),
		Topic:    "legacy-topic",
		Duration: 10 * time.Millisecond,
	}, 1, rcv); err != nil {
		t.Fatal(err)
	}
	rcv.await(t, time.Second)

	time.Sleep(60 * time.Millisecond)
	pkgs := node.snapshot()
	if len(pkgs) == 0 {
		t.Fatal("legacy timer should still fire")
	}
	dataAny := pkgs[0].Messages[0].Payloads[0].Data()
	if _, ok := dataAny.(int64); !ok {
		t.Errorf("legacy fire should send int64 ns, got %T", dataAny)
	}

	timers, _ := d.ReverseMapSize()
	if timers != 0 {
		t.Errorf("legacy start must not install reverse-map entries, got %d", timers)
	}
}

// TestClock_TickerStopByChID covers the ticker analog of TimerStopByChID.
func TestClock_TickerStopByChID(t *testing.T) {
	d, node, ctx, cancel := newDispatcherWithCtx(t)
	defer stopDispatcher(t, d, cancel)

	var fired atomic.Int32
	build := func(at time.Time, gen uint64) payload.Payload {
		fired.Add(1)
		return payload.NewPayload(at.UnixNano(), payload.Golang)
	}
	rcv := newMockReceiver()
	if err := d.handleTickerStart(ctx, clockapi.TickerStartCmd{
		PID:      samplePID("tk"),
		Topic:    "@pid/route",
		Duration: 5 * time.Millisecond,
		ChID:     2,
		Epoch:    1,
		Build:    build,
	}, 1, rcv); err != nil {
		t.Fatal(err)
	}
	rcv.await(t, time.Second)

	time.Sleep(30 * time.Millisecond)
	if fired.Load() == 0 {
		t.Fatal("ticker should have fired at least once")
	}

	stopRcv := newMockReceiver()
	if err := d.handleTickerStopByChID(ctx, clockapi.TickerStopByChIDCmd{
		TargetPID: samplePID("tk"),
		Epoch:     1,
		ChID:      2,
	}, 2, stopRcv); err != nil {
		t.Fatal(err)
	}
	stopRcv.await(t, time.Second)

	_, tickers := d.ReverseMapSize()
	if tickers != 0 {
		t.Errorf("ticker reverse map should be empty after stop, got %d", tickers)
	}

	// After stop, no further fires reach the relay.
	stopCount := fired.Load()
	time.Sleep(30 * time.Millisecond)
	if got := fired.Load(); got != stopCount {
		t.Errorf("ticker should have stopped firing; pre=%d post=%d", stopCount, got)
	}
	_ = node
}

// TestClock_StopByChIDUnknownEpochTombstones: a stop with a non-matching
// epoch is tombstoned independently of any live entry. This is the
// process-pool-reuse case where a freshly-recycled router issues a
// stop for an entry created under the prior incarnation.
func TestClock_StopByChIDUnknownEpochTombstones(t *testing.T) {
	d, _, ctx, cancel := newDispatcherWithCtx(t)
	defer stopDispatcher(t, d, cancel)

	// Live entry under epoch=1.
	if err := d.handleTimerStart(ctx, clockapi.TimerStartCmd{
		PID: samplePID("e"), Topic: "@pid/route", Duration: time.Hour,
		ChID: 5, Epoch: 1,
		Build: func(at time.Time, gen uint64) payload.Payload { return nil },
	}, 1, newMockReceiver()); err != nil {
		t.Fatal(err)
	}

	// Stop targeting epoch=2 → no match, tombstone installed under epoch=2.
	stopRcv := newMockReceiver()
	if err := d.handleTimerStopByChID(ctx, clockapi.TimerStopByChIDCmd{
		TargetPID: samplePID("e"), Epoch: 2, ChID: 5,
	}, 2, stopRcv); err != nil {
		t.Fatal(err)
	}
	stopRcv.await(t, time.Second)

	timers, _ := d.ReverseMapSize()
	if timers != 1 {
		t.Errorf("epoch=1 live entry must remain, got %d", timers)
	}
	pendingT, _ := d.PendingStopsSize()
	if pendingT != 1 {
		t.Errorf("epoch=2 tombstone should be installed, got %d", pendingT)
	}
}

// Compile-time check that mockReceiver implements ResultReceiver.
var _ dispatcher.ResultReceiver = (*mockReceiver)(nil)
