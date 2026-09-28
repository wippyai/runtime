// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"testing"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	topapi "github.com/wippyai/runtime/api/topology"
	systopology "github.com/wippyai/runtime/system/topology"
)

type reconnectRouter func(*relay.Package) error

func (r reconnectRouter) Send(p *relay.Package) error { return r(p) }

func TestLockRearmsMonitorAfterLinkLoss(t *testing.T) {
	s := newLockSvc(t)
	requests := 0
	topo := systopology.NewTopology(reconnectRouter(func(pkg *relay.Package) error {
		if pkg.Target.Equal(s.self) {
			return s.Send(pkg)
		}
		defer relay.ReleasePackage(pkg)
		for _, message := range pkg.Messages {
			for _, value := range message.Payloads {
				if _, ok := value.Data().(*topapi.MonitorRequestEvent); ok {
					requests++
				}
			}
		}
		return nil
	}), "n1")
	s.topo = topo
	if err := topo.Register(s.self); err != nil {
		t.Fatal(err)
	}
	owner := pid.PID{Node: "remote", Host: "app", UniqID: "owner"}
	mustAcquire(t, s, "before", owner)
	if requests != 1 {
		t.Fatalf("initial monitor requests: %d", requests)
	}
	topo.HandleNodeExit("remote", nil)
	if _, held, err := s.Holder("before"); err != nil || !held {
		t.Fatalf("link loss released lock: held=%v err=%v", held, err)
	}
	mustAcquire(t, s, "after", owner)
	if requests != 2 {
		t.Fatalf("new lock after reconnect did not rearm the removed monitor: requests=%d", requests)
	}
}
