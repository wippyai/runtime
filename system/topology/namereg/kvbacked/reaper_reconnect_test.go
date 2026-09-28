// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"testing"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	topapi "github.com/wippyai/runtime/api/topology"
	systemkv "github.com/wippyai/runtime/system/kv"
	systopology "github.com/wippyai/runtime/system/topology"
)

type reconnectRouter func(*relay.Package) error

func (r reconnectRouter) Send(p *relay.Package) error { return r(p) }

func TestRegistryRearmsMonitorAfterLinkLoss(t *testing.T) {
	engine := systemkv.NewService("reconnect", nil)
	if _, err := engine.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Stop(context.Background()) })
	s := NewService(engine, "n1", nil, nil)
	acquire := func(name string, owner pid.PID) {
		t.Helper()
		if _, err := s.Register(t.Context(), name, owner); err != nil {
			t.Fatal(err)
		}
	}
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
	acquire("before", owner)
	if requests != 1 {
		t.Fatalf("initial monitor requests: %d", requests)
	}
	topo.HandleNodeExit("remote", nil)
	if result, err := s.Lookup(t.Context(), "before"); err != nil || !result.Found || !result.PID.Equal(owner) {
		t.Fatalf("link loss removed name: result=%+v err=%v", result, err)
	}
	acquire("after", owner)
	if requests != 2 {
		t.Fatalf("new registration after reconnect did not rearm the removed monitor: requests=%d", requests)
	}
}
