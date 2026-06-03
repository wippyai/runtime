// SPDX-License-Identifier: MPL-2.0

package eventual_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/system/topology/namereg/eventual"
	"go.uber.org/zap"
)

const bigBudget = 1 << 20

func newSvc(t *testing.T, name string, sender eventual.MessageSender) *eventual.Service {
	t.Helper()
	es := eventual.NewService(eventual.Config{
		LocalNodeID: name,
		Sender:      sender,
		Logger:      zap.NewNop(),
	})
	if err := es.Start(context.Background()); err != nil {
		t.Fatalf("start %s: %v", name, err)
	}
	t.Cleanup(func() { _ = es.Stop() })
	return es
}

// routerSender routes shard frames to the registered peer's OnFrame. An
// unregistered target returns an error (models an unreachable node).
type routerSender struct {
	reg map[string]*eventual.Service
}

func (r *routerSender) Send(target string, payload []byte) error {
	svc, ok := r.reg[target]
	if !ok {
		return fmt.Errorf("unreachable: %s", target)
	}
	svc.OnFrame(payload)
	return nil
}

func resolved(es *eventual.Service, name string) bool {
	r, err := es.Lookup(context.Background(), name)
	return err == nil && r.Found
}

// TestEpidemicReBroadcast asserts a node re-broadcasts a delta it learned from
// a peer (so gossip floods the cluster), and does NOT re-broadcast an entry it
// already knew (loop-free via CRDT idempotency).
func TestEpidemicReBroadcast(t *testing.T) {
	a := newSvc(t, "A", nil)
	b := newSvc(t, "B", nil)

	if _, err := a.Register("x", pid.PID{Node: "A", Host: "h", UniqID: "x1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	framesA := a.DrainBroadcasts(0, bigBudget)
	if len(framesA) == 0 {
		t.Fatalf("A produced no delta frames")
	}

	for _, f := range framesA {
		b.OnFrame(f)
	}
	if !resolved(b, "x") {
		t.Fatalf("B did not learn x")
	}

	// B must re-broadcast the newly-learned entry (epidemic dissemination).
	framesB := b.DrainBroadcasts(0, bigBudget)
	if len(framesB) == 0 {
		t.Fatalf("B did not re-broadcast a newly-learned delta (not epidemic)")
	}

	// Re-applying the same delta is a no-op and must NOT re-broadcast (no loop).
	for _, f := range framesA {
		b.OnFrame(f)
	}
	if again := b.DrainBroadcasts(0, bigBudget); len(again) != 0 {
		t.Fatalf("B re-broadcast an already-known entry (would loop): %d frames", len(again))
	}
}

// TestAntiEntropyPeerAuthoritative asserts the pull request targets the actual
// pushing peer, not the highest-counter origin. srv-2 holds entries originated
// by a now-unreachable srv-1; a client pulling from srv-2 must request shards
// from srv-2 and converge.
func TestAntiEntropyPeerAuthoritative(t *testing.T) {
	router := &routerSender{reg: map[string]*eventual.Service{}}

	// srv-1 originates 5 names (high counter), seeds srv-2, then "leaves".
	srv1 := newSvc(t, "srv-1", nil)
	for i := 0; i < 5; i++ {
		if _, err := srv1.Register(fmt.Sprintf("a%d", i), pid.PID{Node: "srv-1", Host: "h", UniqID: fmt.Sprintf("a%d", i)}); err != nil {
			t.Fatalf("register: %v", err)
		}
	}
	srv2 := newSvc(t, "srv-2", router)
	for _, f := range srv1.DrainBroadcasts(0, bigBudget) {
		srv2.OnFrame(f)
	}
	router.reg["srv-2"] = srv2 // srv-1 deliberately NOT registered → unreachable

	client := newSvc(t, "client-1", router)
	router.reg["client-1"] = client

	d := eventual.NewDelegate(client, zap.NewNop())
	srv2d := eventual.NewDelegate(srv2, zap.NewNop())

	// Simulate one push/pull: client merges srv-2's bulk state.
	d.MergeRemoteState(srv2d.LocalState(false), false)

	for i := 0; i < 5; i++ {
		if !resolved(client, fmt.Sprintf("a%d", i)) {
			t.Fatalf("client did not converge on a%d (shard pull mis-targeted to unreachable origin)", i)
		}
	}
}
