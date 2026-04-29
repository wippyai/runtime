// SPDX-License-Identifier: MPL-2.0

package membership

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// captureDelegate records received user-broadcasts so the test can assert
// the multiplex routing actually delivered.
type captureDelegate struct {
	pushBody []byte
	rx       atomic.Int64
	rxBytes  atomic.Int64
	mergeRx  atomic.Int64
	getCalls atomic.Int64
	kind     byte
}

func (c *captureDelegate) Kind() byte { return c.kind }

func (c *captureDelegate) GetBroadcasts(_, _ int) [][]byte {
	c.getCalls.Add(1)
	if c.pushBody == nil {
		return nil
	}
	out := c.pushBody
	c.pushBody = nil // one-shot
	return [][]byte{out}
}

func (c *captureDelegate) NotifyMsg(payload []byte) {
	c.rx.Add(1)
	c.rxBytes.Add(int64(len(payload)))
}

func (c *captureDelegate) LocalState(_ bool) []byte { return nil }

func (c *captureDelegate) MergeRemoteState(_ []byte, _ bool) {
	c.mergeRx.Add(1)
}

// TestMultiplex_TwoNodes_UserBroadcastDelivers verifies that when node A
// produces a user-broadcast via a registered UserDelegate, node B's
// matching UserDelegate (same Kind byte) receives it through memberlist.
// This is the core convergence path used by eventualreg + kveventual.
func TestMultiplex_TwoNodes_UserBroadcastDelivers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger := zap.NewNop()
	bus := eventbus.NewBus()

	// Node A — bind 0 (auto-assign port).
	cfgA := Config{NodeName: "node-a", BindAddr: "127.0.0.1", BindPort: 0}
	svcA := NewService(cfgA, bus, logger.Named("a"), nil, nil, nil)
	delA := &captureDelegate{kind: 0xE1, pushBody: []byte("hello-from-a")}
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
	if delB.rxBytes.Load() != int64(len("hello-from-a")) {
		t.Errorf("rx bytes = %d want %d", delB.rxBytes.Load(), len("hello-from-a"))
	}
}
