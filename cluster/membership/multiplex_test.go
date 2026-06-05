// SPDX-License-Identifier: MPL-2.0

package membership

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// captureDelegate records received user-broadcasts so the test can assert
// the multiplex routing actually delivered. pushBody is armed by Arm() AFTER
// membership convergence and stays set until Disarm — every gossip cycle
// returns it, so a single UDP loss under stress is recovered by the next
// transmission instead of being terminal.
type captureDelegate struct {
	pushBody []byte
	rx       atomic.Int64
	rxBytes  atomic.Int64
	mergeRx  atomic.Int64
	getCalls atomic.Int64
	mu       sync.Mutex
	kind     byte
}

func (c *captureDelegate) Kind() byte { return c.kind }

// Arm enables the broadcast body. Safe to call concurrently with GetBroadcasts.
func (c *captureDelegate) Arm(body []byte) {
	c.mu.Lock()
	c.pushBody = body
	c.mu.Unlock()
}

// Disarm clears the body so memberlist stops queuing the broadcast on
// subsequent gossip cycles once delivery is confirmed.
func (c *captureDelegate) Disarm() {
	c.mu.Lock()
	c.pushBody = nil
	c.mu.Unlock()
}

func (c *captureDelegate) GetBroadcasts(_, _ int) [][]byte {
	c.getCalls.Add(1)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pushBody == nil {
		return nil
	}
	return [][]byte{c.pushBody}
}

func (c *captureDelegate) NotifyMsg(payload []byte) {
	c.rx.Add(1)
	c.rxBytes.Add(int64(len(payload)))
}

func (c *captureDelegate) LocalState(_ bool) []byte { return nil }

func (c *captureDelegate) MergeRemoteState(_ []byte, _ bool) {
	c.mergeRx.Add(1)
}

type oneShotDelegate struct {
	body []byte
	sent atomic.Bool
	kind byte
}

func (d *oneShotDelegate) Kind() byte { return d.kind }

func (d *oneShotDelegate) GetBroadcasts(_, _ int) [][]byte {
	if d.sent.Swap(true) {
		return nil
	}
	return [][]byte{d.body}
}

func (d *oneShotDelegate) NotifyMsg(_ []byte)                {}
func (d *oneShotDelegate) LocalState(_ bool) []byte          { return nil }
func (d *oneShotDelegate) MergeRemoteState(_ []byte, _ bool) {}

type stickyDelegate struct {
	body  []byte
	calls atomic.Int64
	kind  byte
}

func (d *stickyDelegate) Kind() byte { return d.kind }
func (d *stickyDelegate) GetBroadcasts(_, _ int) [][]byte {
	d.calls.Add(1)
	return [][]byte{d.body}
}
func (d *stickyDelegate) NotifyMsg(_ []byte)                {}
func (d *stickyDelegate) LocalState(_ bool) []byte          { return nil }
func (d *stickyDelegate) MergeRemoteState(_ []byte, _ bool) {}

func TestDelegate_GetBroadcasts_RetransmitsOneShotUserFrame(t *testing.T) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	svc := NewService(Config{NodeName: "node-a"}, bus, logger, nil, nil, nil)
	ud := &oneShotDelegate{kind: 0xE1, body: []byte("delta")}
	if err := svc.RegisterUserDelegate(ud); err != nil {
		t.Fatalf("register delegate: %v", err)
	}

	d := newDelegate(svc, 3)
	first := d.GetBroadcasts(10, 512)
	if len(first) != 1 {
		t.Fatalf("first GetBroadcasts returned %d frames, want 1", len(first))
	}
	second := d.GetBroadcasts(10, 512)
	if len(second) != 1 {
		t.Fatalf("second GetBroadcasts returned %d frames, want retransmit of queued frame", len(second))
	}
	if string(first[0]) != string(second[0]) {
		t.Fatalf("retransmit frame mismatch: %q != %q", string(first[0]), string(second[0]))
	}
}

func TestDelegate_GetBroadcasts_DuplicateFrameDoesNotResetTransmitLimit(t *testing.T) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	svc := NewService(Config{NodeName: "node-a"}, bus, logger, nil, nil, nil)
	ud := &stickyDelegate{kind: 0xE1, body: []byte("same-delta")}
	if err := svc.RegisterUserDelegate(ud); err != nil {
		t.Fatalf("register delegate: %v", err)
	}

	d := newDelegate(svc, 3)
	for i := 0; i < 3; i++ {
		frames := d.GetBroadcasts(10, 512)
		if len(frames) != 1 {
			t.Fatalf("GetBroadcasts round %d returned %d frames, want 1", i+1, len(frames))
		}
	}
	if queued := d.broadcasts.NumQueued(); queued != 0 {
		t.Fatalf("duplicate frame reset transmit limit; queued=%d want 0 after limit", queued)
	}
}

func TestDelegate_GetBroadcasts_DuplicateFrameDoesNotConsumeNewWorkBudget(t *testing.T) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	svc := NewService(Config{NodeName: "node-a"}, bus, logger, nil, nil, nil)
	dup := &stickyDelegate{kind: 0xE1, body: []byte("duplicate-delta")}
	next := &oneShotDelegate{kind: 0xE2, body: []byte("other-delta")}
	if err := svc.RegisterUserDelegate(dup); err != nil {
		t.Fatalf("register duplicate delegate: %v", err)
	}
	if err := svc.RegisterUserDelegate(next); err != nil {
		t.Fatalf("register second delegate: %v", err)
	}

	d := newDelegate(svc, 3)
	first := d.GetBroadcasts(10, 512)
	if len(first) != 2 {
		t.Fatalf("first GetBroadcasts returned %d frames, want 2", len(first))
	}
	second := d.GetBroadcasts(10, 512)
	if len(second) != 2 {
		t.Fatalf("duplicate should not consume new-work budget; second returned %d frames, want 2", len(second))
	}
}

func TestDelegate_GetBroadcasts_RetainsFrameTooLargeForCurrentBudget(t *testing.T) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	svc := NewService(Config{NodeName: "node-a"}, bus, logger, nil, nil, nil)
	ud := &oneShotDelegate{kind: 0xE1, body: make([]byte, 1024)}
	if err := svc.RegisterUserDelegate(ud); err != nil {
		t.Fatalf("register delegate: %v", err)
	}

	d := newDelegate(svc, 3)
	if frames := d.GetBroadcasts(10, 128); len(frames) != 0 {
		t.Fatalf("oversized frame returned %d frames, want 0", len(frames))
	}
	if queued := d.broadcasts.NumQueued(); queued != 1 {
		t.Fatalf("frame too large for current budget queued=%d want 1", queued)
	}
	if frames := d.GetBroadcasts(10, 2048); len(frames) != 1 {
		t.Fatalf("retained frame returned %d frames once budget fits, want 1", len(frames))
	}
}

func TestDelegate_GetBroadcasts_DropsFrameTooLargeForMemberlistPacket(t *testing.T) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	svc := NewService(Config{NodeName: "node-a"}, bus, logger, nil, nil, nil)
	ud := &oneShotDelegate{kind: 0xE1, body: make([]byte, userBroadcastMaxPacketBytes)}
	if err := svc.RegisterUserDelegate(ud); err != nil {
		t.Fatalf("register delegate: %v", err)
	}

	d := newDelegate(svc, 3)
	if frames := d.GetBroadcasts(10, userBroadcastMaxPacketBytes*2); len(frames) != 0 {
		t.Fatalf("impossible-size frame returned %d frames, want 0", len(frames))
	}
	if queued := d.broadcasts.NumQueued(); queued != 0 {
		t.Fatalf("impossible-size frame queued=%d want 0", queued)
	}
}

func TestDelegate_GetBroadcasts_BackpressureSkipsDelegatePolling(t *testing.T) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	svc := NewService(Config{NodeName: "node-a"}, bus, logger, nil, nil, nil)
	ud := &stickyDelegate{kind: 0xE1, body: []byte("new-delta")}
	if err := svc.RegisterUserDelegate(ud); err != nil {
		t.Fatalf("register delegate: %v", err)
	}

	d := newDelegate(svc, 3)
	for i := 0; i < userBroadcastBackpressure; i++ {
		frame := []byte{0xE1, 2, 0, 0, 0, byte(i), byte(i >> 8)}
		if !d.queueBroadcast(frame) {
			t.Fatalf("queue prefill %d was rejected", i)
		}
	}

	frames := d.GetBroadcasts(10, 512)
	if len(frames) == 0 {
		t.Fatalf("expected queued frames to drain under backpressure")
	}
	if got := ud.calls.Load(); got != 0 {
		t.Fatalf("delegate polled under backpressure: calls=%d", got)
	}
}

// TestMultiplex_TwoNodes_UserBroadcastDelivers verifies that when node A
// produces a user-broadcast via a registered UserDelegate, node B's
// matching UserDelegate (same Kind byte) receives it through memberlist.
// This is the core convergence path used by eventual.
func TestMultiplex_TwoNodes_UserBroadcastDelivers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger := zap.NewNop()
	bus := eventbus.NewBus()

	// Node A — bind 0 (auto-assign port). Arm pushBody AFTER convergence so
	// memberlist cannot drain the one-shot before B is in A's member set.
	cfgA := Config{NodeName: "node-a", BindAddr: "127.0.0.1", BindPort: 0}
	svcA := NewService(cfgA, bus, logger.Named("a"), nil, nil, nil)
	delA := &captureDelegate{kind: 0xE1}
	if err := svcA.RegisterUserDelegate(delA); err != nil {
		t.Fatalf("register a: %v", err)
	}
	if err := svcA.Start(ctx); err != nil {
		t.Fatalf("start a: %v", err)
	}
	defer func() { _ = svcA.Stop() }()

	addrA := fmt.Sprintf("%s:%d", svcA.memberlist.LocalNode().Addr, svcA.memberlist.LocalNode().Port)

	// Node B — joins A.
	cfgB := Config{NodeName: "node-b", BindAddr: "127.0.0.1", BindPort: 0, JoinAddrs: []string{addrA}}
	svcB := NewService(cfgB, bus, logger.Named("b"), nil, nil, nil)
	delB := &captureDelegate{kind: 0xE1}
	if err := svcB.RegisterUserDelegate(delB); err != nil {
		t.Fatalf("register b: %v", err)
	}
	if err := svcB.Start(ctx); err != nil {
		t.Fatalf("start b: %v", err)
	}
	defer func() { _ = svcB.Stop() }()

	// Wait for membership convergence (both nodes see each other) before
	// arming the broadcast. Without this, memberlist may call GetBroadcasts
	// on A before B is a peer, drain the one-shot, and gossip to zero peers.
	convergeDeadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(convergeDeadline) {
		if svcA.memberlist.NumMembers() >= 2 && svcB.memberlist.NumMembers() >= 2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if svcA.memberlist.NumMembers() < 2 || svcB.memberlist.NumMembers() < 2 {
		t.Fatalf("membership did not converge: A=%d B=%d",
			svcA.memberlist.NumMembers(), svcB.memberlist.NumMembers())
	}
	delA.Arm([]byte("hello-from-a"))

	// Wait up to 10s for B to receive the broadcast from A's delegate.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if delB.rx.Load() > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if delB.rx.Load() == 0 {
		t.Fatalf("node B never received the user-broadcast from A; getCalls on A=%d, members=%d",
			delA.getCalls.Load(), svcA.memberlist.NumMembers())
	}
	// Stop queuing the body so further gossip cycles don't re-deliver.
	delA.Disarm()
	if delB.rxBytes.Load() < int64(len("hello-from-a")) {
		t.Errorf("rx bytes = %d want >= %d", delB.rxBytes.Load(), len("hello-from-a"))
	}
}
