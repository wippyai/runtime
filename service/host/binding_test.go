// SPDX-License-Identifier: MPL-2.0
package host

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	sysrelay "github.com/wippyai/runtime/system/relay"
)

func TestActorBindingThroughNativeRouter(t *testing.T) {
	th := newTestHost()
	th.start(t)
	defer th.stop()
	node := sysrelay.NewNode("test-node")
	router := sysrelay.NewRouter(node, nil)
	release, err := node.RegisterOwnedHost("test:host", th.host)
	require.NoError(t, err)
	defer release()
	target := pid.PID{Node: "test-node", Host: "test:host", UniqID: "bound-actor"}
	proc := &contextMessageProcess{ready: make(chan struct{}), received: make(chan struct{})}
	_, err = th.scheduler.Submit(context.Background(), target, proc, "", nil)
	require.NoError(t, err)
	select {
	case <-proc.ready:
	case <-time.After(time.Second):
		t.Fatal("actor did not start")
	}
	bound, err := router.BindLocal(target)
	require.NoError(t, err)
	require.NoError(t, bound.SendContext(context.Background(), relay.NewPackage(pid.PID{}, target, "application")))
	select {
	case <-proc.received:
	case <-time.After(time.Second):
		t.Fatal("bound package did not reach actor")
	}
	release()
	pkg := relay.NewPackage(pid.PID{}, target, "application")
	require.ErrorIs(t, bound.SendContext(context.Background(), pkg), relay.ErrBindingRetired)
	require.Len(t, pkg.Messages, 1)
	relay.ReleasePackage(pkg)
}

func TestHostBindingRejectsWrongHostAndStoppedHost(t *testing.T) {
	th := newTestHost()
	target := pid.PID{Node: "test-node", Host: "test:host", UniqID: "actor"}
	_, err := th.host.BindLocal(target)
	require.ErrorIs(t, err, ErrHostNotRunning)
	_, err = th.host.BindLocal(pid.PID{Host: "other", UniqID: "actor"})
	require.ErrorIs(t, err, relay.ErrBindingTarget)
	th.start(t)
	th.stop()
	_, err = th.host.BindLocal(target)
	require.ErrorIs(t, err, ErrHostShuttingDown)
}
