// SPDX-License-Identifier: MPL-2.0
package eventual

import (
	"fmt"
	"sync"
	"testing"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

type revokeRecorder struct{ packages []*relay.Package }

func (r *revokeRecorder) Send(p *relay.Package) error {
	r.packages = append(r.packages, p)
	return nil
}

func TestRevokeForStrongTargetsHiddenLocalOrigin(t *testing.T) {
	for _, mode := range []string{"third-owner", "remote-winner", "same-owner", "same-owner-cached"} {
		t.Run(mode, func(t *testing.T) {
			recorder := &revokeRecorder{}
			s := NewService(Config{LocalNodeID: "local", Revoker: recorder})
			owner := makePID("local", "process", "owner")
			remote := makePID("remote", "process", "winner")
			keep := pid.PID{Node: "other", Host: "process", UniqID: "strong"}
			if mode == "remote-winner" {
				keep = remote
			}
			if mode == "same-owner" {
				keep = owner
			}
			if mode == "same-owner-cached" {
				keep = owner.Precomputed()
			}
			same := keep.Equal(owner)
			if _, err := s.Register("name", owner); err != nil {
				t.Fatal(err)
			}
			// The visible remote winner must not hide this node's weaker claim.
			s.state.Apply(&Entry{Name: "name", PID: remote, Node: s.state.internNode("remote"), Counter: 1, Priority: 100})
			if got, ok := s.state.Lookup("name"); !ok || !got.Equal(remote) {
				t.Fatal("fixture requires a remote visible winner")
			}
			if got := s.RevokeForStrong("name", keep); got == same {
				t.Fatalf("revoked=%v, sameOwner=%v", got, same)
			}
			if _, owned := s.owned["name"]; owned != same {
				t.Fatalf("local intent preserved=%v, sameOwner=%v", owned, same)
			}
			if got, found := s.state.Lookup("name"); !found || !got.Equal(remote) {
				t.Fatal("strong withdrawal rewrote the unrelated remote winner")
			}
			if !same {
				if len(recorder.packages) != 1 || !recorder.packages[0].Target.Equal(owner) {
					t.Fatal("must signal only the hidden local owner")
				}
				if s.RevokeForStrong("name", keep) {
					t.Fatal("duplicate revocation")
				}
				if len(recorder.packages) != 1 {
					t.Fatal("duplicate notification")
				}
			} else if len(recorder.packages) != 0 {
				t.Fatal("same owner must survive without a revoke")
			}
			s.state.Apply(&Entry{Name: "name", Node: s.state.internNode("remote"), Counter: 2, Deleted: true})
			got, found := s.state.Lookup("name")
			if found != same || (found && !got.Equal(owner)) {
				t.Fatalf("local claim after remote removal: %v, %v", got, found)
			}
		})
	}
}

func TestWithdrawalDisarmsTombstonedOwnedIntent(t *testing.T) {
	for _, strong := range []bool{false, true} {
		s := NewService(Config{LocalNodeID: "local"})
		owner := makePID("local", "process", "owner")
		if _, err := s.Register("name", owner); err != nil {
			t.Fatal(err)
		}
		s.state.Unregister("name", 1)
		if strong {
			s.RevokeForStrong("name", makePID("remote", "process", "strong"))
		} else {
			s.Unregister("name")
		}
		s.reassertOwned("name")
		if _, found := s.state.Lookup("name"); found {
			t.Fatalf("withdrawn intent resurrected: strong=%v", strong)
		}
	}
}

func TestSameOwnerIntentSurvivesRevocationWithoutLiveDot(t *testing.T) {
	s := NewService(Config{LocalNodeID: "local"})
	owner := makePID("local", "process", "owner")
	if _, err := s.Register("name", owner); err != nil {
		t.Fatal(err)
	}
	s.state.Unregister("name", 1)
	if s.RevokeForStrong("name", owner.Precomputed()) {
		t.Fatal("same owner must not be revoked")
	}
	s.reassertOwned("name")
	if got, found := s.state.Lookup("name"); !found || !got.Equal(owner) {
		t.Fatal("same owner could not recover its own dot")
	}
}

func TestReassertAndRevokeDoNotResurrectWithdrawnIntent(t *testing.T) {
	for i := 0; i < 100; i++ {
		s := NewService(Config{LocalNodeID: "local"})
		owner := makePID("local", "process", "owner")
		if _, err := s.Register("name", owner); err != nil {
			t.Fatal(err)
		}
		s.state.Unregister("name", 1)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); s.reassertOwned("name") }()
		go func() { defer wg.Done(); s.RevokeForStrong("name", makePID("other", "process", "strong")) }()
		wg.Wait()
		if _, found := s.state.Lookup("name"); found {
			t.Fatal("reassertion raced after revoked intent")
		}
		s.ownedMu.Lock()
		_, owned := s.owned["name"]
		s.ownedMu.Unlock()
		if owned {
			t.Fatal("revoked intent retained")
		}
	}
}

func TestDistinctNameWithdrawalsRunConcurrently(t *testing.T) {
	s := NewService(Config{LocalNodeID: "local"})
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("name-%d", i)
			owner := makePID("local", "process", name)
			if _, err := s.Register(name, owner); err != nil {
				t.Error(err)
				return
			}
			s.state.Unregister(name, 1) // simulates a stale same-origin echo
			s.reassertOwned(name)
			s.RevokeForStrong(name, makePID("other", "process", "strong"))
			if _, found := s.state.Lookup(name); found {
				t.Errorf("%s survived withdrawal", name)
			}
		}(i)
	}
	wg.Wait()
	s.ownedMu.Lock()
	remaining := len(s.owned)
	s.ownedMu.Unlock()
	if remaining != 0 {
		t.Fatalf("%d withdrawn owner intents remain", remaining)
	}
}
