// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"testing"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	systemkv "github.com/wippyai/runtime/system/kv"
)

func TestRegistryLinkDownDoesNotReapLiveOwner(t *testing.T) {
	engine := systemkv.NewService("registry-link-down", nil)
	if _, err := engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Stop(context.Background()) })
	reg := NewService(engine, "node-1", nil, nil)
	owner := pid.PID{Node: "node-2", Host: "app", UniqID: "running"}
	if _, err := reg.Register(context.Background(), "shared", owner); err != nil {
		t.Fatal(err)
	}

	send := func(kind topology.Kind) {
		t.Helper()
		pkg := relay.NewPackage(owner, reg.self, topology.TopicEvents,
			payload.New(&topology.ExitEvent{From: owner, Kind: kind}))
		if err := reg.Send(pkg); err != nil {
			t.Fatal(err)
		}
	}
	send(topology.LinkDown)
	if result, err := reg.Lookup(context.Background(), "shared"); err != nil || !result.Found || result.PID.String() != owner.String() {
		t.Fatalf("LinkDown revoked a live owner's name: result=%+v err=%v", result, err)
	}
	send(topology.Exit)
	if result, err := reg.Lookup(context.Background(), "shared"); err != nil || result.Found {
		t.Fatalf("confirmed exit did not revoke name: result=%+v err=%v", result, err)
	}
}
