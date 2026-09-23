// SPDX-License-Identifier: MPL-2.0

package eventual

import (
	"errors"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/system/topology/namereg/admission"
)

type reassertChecker struct {
	err               error
	owner             pid.PID
	calls             int
	ready             bool
	dropReadyOnLookup bool
}

func (c *reassertChecker) LookupOther(string, pid.PID) (pid.PID, bool, error) {
	c.calls++
	if c.dropReadyOnLookup {
		c.ready = false
	}
	return c.owner, c.owner != (pid.PID{}), c.err
}
func (c *reassertChecker) NameReady() bool { return c.ready }

func TestRegisterReadinessLostDuringLookupLeavesNoIntent(t *testing.T) {
	check := &reassertChecker{ready: true, dropReadyOnLookup: true}
	s := NewService(Config{LocalNodeID: "node-A", Admission: &admission.Coordinator{}, CrossScope: check})
	p := pid.PID{Node: "node-A", Host: "host", UniqID: "owner"}
	if _, err := s.Register("claim", p); !errors.Is(err, ErrNameServiceNotReady) {
		t.Fatalf("register after readiness loss: %v", err)
	}
	if _, found := s.state.Lookup("claim"); found || len(s.owned) != 0 {
		t.Fatalf("readiness loss published a binding or intent: found=%v owned=%d", found, len(s.owned))
	}
	check.ready = true
	check.dropReadyOnLookup = false
	if _, err := s.Register("claim", p); err != nil {
		t.Fatalf("name gate was not released: %v", err)
	}
}

func TestReassertCannotRestoreOwnedDotAcrossStrongExclusion(t *testing.T) {
	check := &reassertChecker{ready: true}
	s := NewService(Config{LocalNodeID: "node-A", Admission: &admission.Coordinator{}, CrossScope: check})
	owner := pid.PID{Node: "node-A", Host: "host", UniqID: "old"}
	if _, err := s.Register("claim", owner); err != nil {
		t.Fatal(err)
	}
	if event := s.state.Unregister("claim", time.Now().UnixMilli()); event == nil {
		t.Fatal("failed to model stale same-origin deletion")
	}
	check.owner = pid.PID{Node: "node-A", Host: "host", UniqID: "strong"}
	s.reassertOwned("claim")
	if p, found := s.state.Lookup("claim"); found {
		t.Fatalf("reassert restored conflicting EVENTUAL owner %v", p)
	}
	check.owner = pid.PID{}
	check.err = errors.New("cross-scope read unavailable")
	s.reassertOwned("claim")
	if _, found := s.state.Lookup("claim"); found {
		t.Fatal("reassert used a failed cross-scope read as absence")
	}
	check.ready = false
	check.err = nil
	before := check.calls
	s.reassertOwned("claim")
	if check.calls != before {
		t.Fatal("reassert forwarded a cross-scope read while admission is not ready")
	}
	check.ready = true
	s.Unregister("claim")
	s.reassertOwned("claim")
	if _, found := s.state.Lookup("claim"); found {
		t.Fatal("reassert restored explicitly unregistered ownership")
	}
}
