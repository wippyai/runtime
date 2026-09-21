// SPDX-License-Identifier: MPL-2.0

package global

import (
	"testing"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
)

func TestRegistryLinkDownClearsInactiveMonitor(t *testing.T) {
	fsm := NewFSM()
	svc := NewService(newDirectApplyRaft(fsm, true), fsm, &nopBus{}, nil,
		&nopRouter{}, nil, "local", noopLogger(), nil, nil, nil)
	owner := pid.PID{Node: "remote", Host: "app", UniqID: "running"}
	if _, err := svc.Register(t.Context(), "shared", owner); err != nil {
		t.Fatal(err)
	}
	svc.monitoredPIDs.Store(owner.String(), struct{}{})
	pkg := relay.NewPackage(owner, pid.PID{Node: "local", Host: HostID},
		topology.TopicEvents, payload.New(&topology.ExitEvent{From: owner, Kind: topology.LinkDown}))
	if err := svc.Send(pkg); err != nil {
		t.Fatal(err)
	}
	if _, ok := svc.monitoredPIDs.Load(owner.String()); ok {
		t.Fatal("LinkDown retained an inactive monitor")
	}
	if result, err := svc.Lookup(t.Context(), "shared"); err != nil || !result.Found || !result.PID.Equal(owner) {
		t.Fatalf("LinkDown removed ownership: result=%+v err=%v", result, err)
	}
}
