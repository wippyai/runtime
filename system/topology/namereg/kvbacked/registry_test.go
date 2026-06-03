// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/wippyai/runtime/api/pid"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	"github.com/wippyai/runtime/system/eventbus"
	systemkv "github.com/wippyai/runtime/system/kv"
)

func newReg(t *testing.T) *Service {
	t.Helper()
	eng := systemkv.NewService("reg", eventbus.NewBus(), nil)
	if _, err := eng.Start(context.Background()); err != nil {
		t.Fatalf("engine start: %v", err)
	}
	t.Cleanup(func() { _ = eng.Stop(context.Background()) })
	return NewService(eng, "node-1", nil, nil)
}

func mkPID(node, uniq string) pid.PID {
	return pid.PID{Node: node, Host: "h", UniqID: uniq}
}

func TestConsistent_RegisterLookup(t *testing.T) {
	r := newReg(t)
	p := mkPID("node-1", "a")

	out, err := r.RegisterScope(context.Background(), "svc", p, globalapi.Consistent)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	// Epoch is the raft log index, 0 on the in-memory backend; proven non-zero
	// and replicated in the cluster E2E.
	if out.PID.String() != p.String() {
		t.Fatalf("outcome: %+v", out)
	}

	res, err := r.Lookup(context.Background(), "svc")
	if err != nil || !res.Found || res.PID.String() != p.String() {
		t.Fatalf("lookup: res=%+v err=%v", res, err)
	}
}

func TestConsistent_DedupeSamePID(t *testing.T) {
	r := newReg(t)
	p := mkPID("node-1", "a")
	if _, err := r.Register(context.Background(), "svc", p); err != nil {
		t.Fatalf("first: %v", err)
	}
	got, err := r.Register(context.Background(), "svc", p)
	if err != nil {
		t.Fatalf("re-register same pid must succeed: %v", err)
	}
	if got.String() != p.String() {
		t.Fatalf("dedupe winner = %s, want %s", got, p)
	}
}

func TestConsistent_ConflictFirstWriteWins(t *testing.T) {
	r := newReg(t)
	p1 := mkPID("node-1", "a")
	p2 := mkPID("node-2", "b")
	if _, err := r.Register(context.Background(), "svc", p1); err != nil {
		t.Fatalf("first: %v", err)
	}
	got, err := r.Register(context.Background(), "svc", p2)
	if !errors.Is(err, globalapi.ErrNameAlreadyRegistered) {
		t.Fatalf("want ErrNameAlreadyRegistered, got %v", err)
	}
	if got.String() != p1.String() {
		t.Fatalf("existing owner = %s, want %s", got, p1)
	}
	res, _ := r.Lookup(context.Background(), "svc")
	if res.PID.String() != p1.String() {
		t.Fatalf("owner changed on conflict: %s", res.PID)
	}
}

func TestConsistent_OverrideResolveSwaps(t *testing.T) {
	eng := systemkv.NewService("reg", eventbus.NewBus(), nil)
	if _, err := eng.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = eng.Stop(context.Background()) })
	p1 := mkPID("node-1", "a")
	p2 := mkPID("node-2", "b")
	// Resolve awards the name to the incoming claimant.
	r := NewService(eng, "node-1", func(_ string, _, incoming pid.PID) pid.PID { return incoming }, nil)

	if _, err := r.Register(context.Background(), "svc", p1); err != nil {
		t.Fatalf("first: %v", err)
	}
	out, err := r.RegisterScope(context.Background(), "svc", p2, globalapi.Consistent)
	if err != nil {
		t.Fatalf("override register: %v", err)
	}
	if out.PID.String() != p2.String() {
		t.Fatalf("incoming should win: %+v", out)
	}
	res, _ := r.Lookup(context.Background(), "svc")
	if res.PID.String() != p2.String() {
		t.Fatalf("owner after swap = %s, want %s", res.PID, p2)
	}
	// Old owner's reverse index must be gone.
	if names := r.namesForPID(p1); len(names) != 0 {
		t.Fatalf("old owner still indexed: %v", names)
	}
	if names := r.namesForPID(p2); len(names) != 1 || names[0] != "svc" {
		t.Fatalf("new owner reverse index = %v", names)
	}
}

func TestConsistent_ByPIDAndUnregister(t *testing.T) {
	r := newReg(t)
	p := mkPID("node-1", "a")
	for _, n := range []string{"x", "y:with:colons", "z"} {
		if _, err := r.Register(context.Background(), n, p); err != nil {
			t.Fatalf("register %s: %v", n, err)
		}
	}
	res, err := r.Lookup(context.Background(), "", globalapi.ByPID(p))
	if err != nil {
		t.Fatalf("bypid: %v", err)
	}
	got := append([]string(nil), res.NamesForPID...)
	sort.Strings(got)
	want := []string{"x", "y:with:colons", "z"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("names = %v, want %v", got, want)
	}

	ok, err := r.Unregister(context.Background(), "y:with:colons")
	if err != nil || !ok {
		t.Fatalf("unregister: ok=%v err=%v", ok, err)
	}
	if res, _ := r.Lookup(context.Background(), "y:with:colons"); res.Found {
		t.Fatalf("name must be gone after unregister")
	}
	if names := r.namesForPID(p); len(names) != 2 {
		t.Fatalf("reverse index not cleaned: %v", names)
	}
}

func TestConsistent_RemoveAndRemoveNode(t *testing.T) {
	r := newReg(t)
	p1 := mkPID("node-1", "a")
	p2 := mkPID("node-2", "b")
	if _, err := r.Register(context.Background(), "n1", p1); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Register(context.Background(), "n2", p1); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Register(context.Background(), "n3", p2); err != nil {
		t.Fatal(err)
	}

	if err := r.Remove(context.Background(), p1); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if res, _ := r.Lookup(context.Background(), "n1"); res.Found {
		t.Fatalf("n1 must be gone after Remove(p1)")
	}
	if res, _ := r.Lookup(context.Background(), "n3"); !res.Found {
		t.Fatalf("n3 must survive Remove(p1)")
	}

	if err := r.RemoveNode(context.Background(), "node-2"); err != nil {
		t.Fatalf("removenode: %v", err)
	}
	if res, _ := r.Lookup(context.Background(), "n3"); res.Found {
		t.Fatalf("n3 must be gone after RemoveNode(node-2)")
	}
}
