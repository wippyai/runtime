// SPDX-License-Identifier: MPL-2.0

package eventual

import (
	"github.com/wippyai/runtime/api/pid"
	"testing"
)

type reassertScope struct {
	owner pid.PID
	held  bool
}

func (c *reassertScope) LookupOther(string) (pid.PID, bool) { return c.owner, c.held }
func (*reassertScope) NameReady() bool                      { return true }

func TestReassertHonorsExistingOtherScopeOwner(t *testing.T) {
	for _, same := range []bool{false, true} {
		scope := &reassertScope{}
		s := NewService(Config{LocalNodeID: "local", CrossScope: scope})
		owner := makePID("local", "h", "owner")
		if _, err := s.Register("name", owner); err != nil {
			t.Fatal(err)
		}
		// A locally absent dot permits a Strong reservation to be established.
		// Reassertion must then obey that reservation instead of reminting blindly.
		s.state.Unregister("name", 1)
		scope.owner, scope.held = makePID("remote", "h", "strong"), true
		if same {
			scope.owner = owner.Precomputed()
		}
		s.reassertOwned("name")
		got, found := s.state.Lookup("name")
		if found != same || (found && got != owner) {
			t.Fatalf("same=%v: binding=%v found=%v", same, got, found)
		}
		// Once ownership was lost, releasing the reservation must not resurrect
		// the previous process without another explicit registration.
		scope.held = false
		s.reassertOwned("name")
		if _, found := s.state.Lookup("name"); found != same {
			t.Fatal("lost ownership intent resurrected")
		}
	}
}
