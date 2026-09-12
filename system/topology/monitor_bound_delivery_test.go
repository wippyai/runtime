// SPDX-License-Identifier: MPL-2.0
package topology

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	sysrelay "github.com/wippyai/runtime/system/relay"
)

type boundMonitorFixtureRouter struct {
	mesh           relay.ContextSender
	local          *sysrelay.Node
	beforeDelivery func()
}

func (r boundMonitorFixtureRouter) Send(p *relay.Package) error {
	return r.SendContext(context.Background(), p)
}
func (r boundMonitorFixtureRouter) SendContext(ctx context.Context, p *relay.Package) error {
	if p.Target.Node == r.local.ID() {
		if r.beforeDelivery != nil {
			r.beforeDelivery()
		}
		return r.local.SendContext(ctx, p)
	}
	return r.mesh.SendContext(ctx, p)
}
func (r boundMonitorFixtureRouter) BindLocal(target pid.PID) (relay.ContextSender, error) {
	bound, err := r.local.BindLocal(target)
	if err != nil {
		return nil, err
	}
	return monitorContextSender(func(ctx context.Context, p *relay.Package) error {
		if r.beforeDelivery != nil {
			r.beforeDelivery()
		}
		return bound.SendContext(ctx, p)
	}), nil
}

func TestMonitorCompletionCannotReachReplacementMailbox(t *testing.T) {
	f := newMonitorSenderFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	node := sysrelay.NewNode(f.caller.Node)
	inbox := sysrelay.NewMailbox(ctx, sysrelay.WithBufferSize(1))
	require.NoError(t, node.RegisterHost(f.caller.Host, inbox))
	old, replacement := make(chan *relay.Package, 1), make(chan *relay.Package, 1)
	detach, err := inbox.Attach(f.caller, old)
	require.NoError(t, err)
	entered, resume := make(chan struct{}), make(chan struct{})
	f.local.router = boundMonitorFixtureRouter{
		mesh: f.local.router.(relay.ContextSender), local: node,
		beforeDelivery: func() { close(entered); <-resume },
	}
	require.NoError(t, f.local.Monitor(f.caller, f.target))
	pkg := completionPackage(t, f, f.controls[0].reference)
	done := make(chan error, 1)
	go func() { done <- f.local.monitorEndpoint.SendContext(ctx, pkg) }()
	<-entered
	f.local.Remove(f.caller)
	detach()
	detachNext, attachErr := inbox.Attach(f.caller, replacement)
	registerErr := f.local.Register(f.caller)
	close(resume)
	deliveryErr := <-done
	if deliveryErr != nil {
		relay.ReleasePackage(pkg)
	}
	require.NoError(t, attachErr)
	defer detachNext()
	require.NoError(t, registerErr)
	require.Error(t, deliveryErr, "old caller's admission must be canceled or retired")
	require.Empty(t, old)
	require.Empty(t, replacement, "old completion must not cross into a new attachment")
	// Positive control proves the replacement itself is usable.
	fresh, err := node.BindLocal(f.caller)
	require.NoError(t, err)
	marker := relay.NewPackage(pid.PID{}, f.caller, "marker")
	require.NoError(t, fresh.SendContext(ctx, marker))
	received := <-replacement
	require.Same(t, marker, received)
	relay.ReleasePackage(received)
}

func TestMonitorCompletionRetainsRecipientWhenHostRouteChanges(t *testing.T) {
	f := newMonitorSenderFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	node := sysrelay.NewNode(f.caller.Node)
	first, next := sysrelay.NewMailbox(ctx), sysrelay.NewMailbox(ctx)
	detach, err := first.Attach(f.caller, make(chan *relay.Package, 1))
	require.NoError(t, err)
	defer detach()
	release, err := node.RegisterOwnedHost(f.caller.Host, first)
	require.NoError(t, err)
	f.local.router = boundMonitorFixtureRouter{mesh: f.local.router.(relay.ContextSender), local: node}
	require.NoError(t, f.local.Monitor(f.caller, f.target))
	release()
	replacement := make(chan *relay.Package, 1)
	detachNext, err := next.Attach(f.caller, replacement)
	require.NoError(t, err)
	defer detachNext()
	releaseNext, err := node.RegisterOwnedHost(f.caller.Host, next)
	require.NoError(t, err)
	defer releaseNext()
	pkg := completionPackage(t, f, f.controls[0].reference)
	require.ErrorIs(t, f.local.monitorEndpoint.SendContext(ctx, pkg), relay.ErrBindingRetired)
	relay.ReleasePackage(pkg)
	require.Empty(t, replacement)
}

func TestDemonitorAbsentWatchDoesNotRequireRecipientBinding(t *testing.T) {
	node := sysrelay.NewNode("local")
	topo := NewTopology(sysrelay.NewRouter(node, nil), "local")
	stop, err := topo.StartRemoteMonitoring(context.Background(), node, MonitorConfig{MaxPending: 1, RequestTimeout: time.Second})
	require.NoError(t, err)
	defer stop()
	caller := pid.PID{Node: "local", Host: "not-attached", UniqID: "caller"}
	require.NoError(t, topo.Register(caller))
	require.NoError(t, topo.Demonitor(caller, pid.PID{Node: "remote", Host: "app", UniqID: "target"}))
}
