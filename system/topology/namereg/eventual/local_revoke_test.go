// SPDX-License-Identifier: MPL-2.0
package eventual

import (
	"fmt"
	"sync"
	"testing"
)

func TestUnregisterTargetsHiddenLocalOrigin(t *testing.T) {
	s := NewService(Config{LocalNodeID: "local"})
	owner := makePID("local", "process", "owner")
	remote := makePID("remote", "process", "winner")
	if _, err := s.Register("name", owner); err != nil {
		t.Fatal(err)
	}
	s.state.Apply(&Entry{Name: "name", PID: remote, Node: s.state.internNode("remote"), Counter: 1, Priority: 100})
	if !s.Unregister("name") {
		t.Fatal("hidden local dot was not unregistered")
	}
	if _, owned := s.owned["name"]; owned {
		t.Fatal("unregistered intent retained")
	}
	if got, found := s.state.Lookup("name"); !found || !got.Equal(remote) {
		t.Fatal("local withdrawal changed remote winner")
	}
	if s.Unregister("name") {
		t.Fatal("duplicate unregister succeeded")
	}
	s.state.Apply(&Entry{Name: "name", Node: s.state.internNode("remote"), Counter: 2, Deleted: true})
	if _, found := s.state.Lookup("name"); found {
		t.Fatal("hidden withdrawn dot reappeared")
	}
}

func TestWithdrawalDisarmsTombstonedOwnedIntent(t *testing.T) {
	s := NewService(Config{LocalNodeID: "local"})
	owner := makePID("local", "process", "owner")
	if _, err := s.Register("name", owner); err != nil {
		t.Fatal(err)
	}
	s.state.Unregister("name", 1)
	s.Unregister("name")
	s.reassertOwned("name")
	if _, found := s.state.Lookup("name"); found {
		t.Fatal("withdrawn intent resurrected")
	}
}

func TestOwnedIntentRecoversAfterRemoteTombstone(t *testing.T) {
	s := NewService(Config{LocalNodeID: "local"})
	owner := makePID("local", "process", "owner")
	if _, err := s.Register("name", owner); err != nil {
		t.Fatal(err)
	}
	s.state.Unregister("name", 1)
	s.reassertOwned("name")
	if got, found := s.state.Lookup("name"); !found || !got.Equal(owner) {
		t.Fatal("owner could not recover its own dot")
	}
}

func TestReassertAndUnregisterDoNotResurrectWithdrawnIntent(t *testing.T) {
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
		go func() { defer wg.Done(); s.Unregister("name") }()
		wg.Wait()
		if _, found := s.state.Lookup("name"); found {
			t.Fatal("reassertion raced after withdrawn intent")
		}
		if _, owned := s.owned["name"]; owned {
			t.Fatal("withdrawn intent retained")
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
			s.state.Unregister(name, 1)
			s.reassertOwned(name)
			s.Unregister(name)
			if _, found := s.state.Lookup(name); found {
				t.Errorf("%s survived withdrawal", name)
			}
		}(i)
	}
	wg.Wait()
	if remaining := len(s.owned); remaining != 0 {
		t.Fatalf("%d withdrawn intents remain", remaining)
	}
}
