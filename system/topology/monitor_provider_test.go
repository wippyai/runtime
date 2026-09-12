// SPDX-License-Identifier: MPL-2.0
package topology

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	topapi "github.com/wippyai/runtime/api/topology"
	sysrelay "github.com/wippyai/runtime/system/relay"
)

type fixtureMonitorProvider struct {
	set      *remoteMonitorSet
	notices  map[string]topapi.MonitorCompletion
	requests []topapi.NativeMonitor
	early    bool
}

func (p *fixtureMonitorProvider) Send(*relay.Package) error {
	panic("monitor must use native provider capability")
}
func (p *fixtureMonitorProvider) AdmitMonitor(ctx context.Context, request topapi.NativeMonitor, notify topapi.MonitorCompletion) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := p.set.establish(request.Caller, request.Previous, request.Reference); err != nil {
		return err
	}
	p.requests = append(p.requests, request)
	p.notices[request.Reference] = notify
	if p.early {
		p.set.close()
		return notify(ctx, &runtime.Result{})
	}
	return nil
}
func (p *fixtureMonitorProvider) ReleaseMonitor(ctx context.Context, request topapi.NativeMonitor) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return p.set.release(request.Caller, request.Reference)
}

func TestNativeMonitorProviderBoundToRegistration(t *testing.T) {
	node := sysrelay.NewNode("local")
	router := sysrelay.NewRouter(node, nil)
	local := NewTopology(router, "local")
	stop, err := local.StartRemoteMonitoring(context.Background(), node, MonitorConfig{MaxPending: 2, RequestTimeout: time.Second})
	require.NoError(t, err)
	defer stop()
	caller := pid.PID{Node: "local", Host: "app", UniqID: "caller"}
	target := pid.PID{Node: "provider", Host: "queue", UniqID: "target"}
	require.NoError(t, local.Register(caller))
	deliveries := 0
	require.NoError(t, node.RegisterHost("app", monitorContextSender(func(_ context.Context, p *relay.Package) error {
		require.Empty(t, p.ReceivedFrom, "local provider must not fabricate physical network ingress")
		require.True(t, p.Source.Equal(target))
		deliveries++
		relay.ReleasePackage(p)
		return nil
	})))
	set, err := newRemoteMonitorSet(2)
	require.NoError(t, err)
	provider := &fixtureMonitorProvider{set: set, notices: make(map[string]topapi.MonitorCompletion)}
	release, err := router.RegisterOwnedPeer("provider", provider)
	require.NoError(t, err)
	require.NoError(t, local.Monitor(caller, target))
	original := provider.requests[0].Reference
	require.NoError(t, local.Demonitor(caller, target))
	require.NoError(t, local.Monitor(caller, target))
	next := provider.requests[1].Reference
	require.Equal(t, original, provider.requests[1].Previous)
	require.NoError(t, provider.notices[original](context.Background(), &runtime.Result{}))
	require.Zero(t, deliveries, "stale callback must not reach application")
	// Registration replacement does not inherit the old provider's authority.
	release()
	replacementRelease, err := router.RegisterOwnedPeer("provider", provider)
	require.NoError(t, err)
	defer replacementRelease()
	require.Error(t, provider.notices[next](context.Background(), &runtime.Result{}))
	require.Error(t, local.Monitor(caller, target))
	require.Zero(t, deliveries)
}

func TestNativeMonitorProviderCompletionBeforeAdmissionReturns(t *testing.T) {
	node := sysrelay.NewNode("local")
	router := sysrelay.NewRouter(node, nil)
	local := NewTopology(router, "local")
	stop, err := local.StartRemoteMonitoring(context.Background(), node, MonitorConfig{MaxPending: 1, RequestTimeout: time.Second})
	require.NoError(t, err)
	defer stop()
	caller := pid.PID{Node: "local", Host: "app", UniqID: "caller"}
	target := pid.PID{Node: "provider", Host: "queue", UniqID: "target"}
	require.NoError(t, local.Register(caller))
	delivered := 0
	require.NoError(t, node.RegisterHost("app", monitorContextSender(func(_ context.Context, p *relay.Package) error { delivered++; relay.ReleasePackage(p); return nil })))
	set, err := newRemoteMonitorSet(1)
	require.NoError(t, err)
	provider := &fixtureMonitorProvider{set: set, notices: make(map[string]topapi.MonitorCompletion), early: true}
	release, err := router.RegisterOwnedPeer("provider", provider)
	require.NoError(t, err)
	defer release()
	require.NoError(t, local.Monitor(caller, target))
	require.Equal(t, 1, delivered)
	require.Error(t, local.Monitor(caller, target), "provider owns decision whether a completed target can be observed anew")
}

func TestNativeProviderCallerCompletionReleasesExactReference(t *testing.T) {
	node := sysrelay.NewNode("local")
	router := sysrelay.NewRouter(node, nil)
	local := NewTopology(router, "local")
	stop, err := local.StartRemoteMonitoring(context.Background(), node, MonitorConfig{MaxPending: 1, RequestTimeout: time.Second})
	require.NoError(t, err)
	defer stop()
	caller := pid.PID{Node: "local", Host: "app", UniqID: "caller"}
	target := pid.PID{Node: "provider", Host: "queue", UniqID: "target"}
	require.NoError(t, local.Register(caller))
	require.NoError(t, node.RegisterHost("app", monitorContextSender(func(_ context.Context, p *relay.Package) error { relay.ReleasePackage(p); return nil })))
	set, err := newRemoteMonitorSet(1)
	require.NoError(t, err)
	provider := &fixtureMonitorProvider{set: set, notices: make(map[string]topapi.MonitorCompletion)}
	release, err := router.RegisterOwnedPeer("provider", provider)
	require.NoError(t, err)
	defer release()
	require.NoError(t, local.Monitor(caller, target))
	reference := provider.requests[0].Reference
	local.Complete(caller, &runtime.Result{})
	require.ErrorIs(t, set.establish(caller, "", reference), errRemoteMonitorConflict, "caller completion must release native reference without sending wire control")
	require.NoError(t, provider.notices[reference](context.Background(), &runtime.Result{}), "departed caller drops stale result")
}

// This host captures the real mailbox attachment but deliberately holds an
// admitted completion after cancellation to expose the provider reuse window.
type heldProviderCompletionHost struct {
	mailbox                   relay.LocalBinder
	entered, canceled, finish chan struct{}
}

func (*heldProviderCompletionHost) Send(*relay.Package) error { panic("bound completion required") }
func (h *heldProviderCompletionHost) BindLocal(target pid.PID) (relay.ContextSender, error) {
	bound, err := h.mailbox.BindLocal(target)
	if err != nil {
		return nil, err
	}
	return monitorContextSender(func(ctx context.Context, p *relay.Package) error {
		close(h.entered)
		<-ctx.Done()
		close(h.canceled)
		<-h.finish
		return bound.SendContext(ctx, p)
	}), nil
}

func TestNativeProviderRetirementPinsCompletionUntilAdmissionReturns(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	node := sysrelay.NewNode("local")
	router := sysrelay.NewRouter(node, nil)
	topo := NewTopology(router, "local")
	stop, err := topo.StartRemoteMonitoring(ctx, node, MonitorConfig{MaxPending: 2, RequestTimeout: time.Second})
	require.NoError(t, err)
	defer stop()
	caller := pid.PID{Node: "local", Host: "app", UniqID: "caller"}
	target := pid.PID{Node: "provider", Host: "queue", UniqID: "target"}
	inbox := sysrelay.NewMailbox(ctx, sysrelay.WithBufferSize(1))
	delivered := make(chan *relay.Package, 1)
	detach, err := inbox.Attach(caller, delivered)
	require.NoError(t, err)
	defer detach()
	host := &heldProviderCompletionHost{mailbox: inbox, entered: make(chan struct{}), canceled: make(chan struct{}), finish: make(chan struct{})}
	require.NoError(t, node.RegisterHost(caller.Host, host))
	require.NoError(t, topo.Register(caller))
	set, err := newRemoteMonitorSet(2)
	require.NoError(t, err)
	provider := &fixtureMonitorProvider{set: set, notices: make(map[string]topapi.MonitorCompletion)}
	release, err := router.RegisterOwnedPeer(target.Node, provider)
	require.NoError(t, err)
	require.NoError(t, topo.Monitor(caller, target))
	notify := provider.notices[provider.requests[0].Reference]
	completed := make(chan error, 1)
	go func() { completed <- notify(ctx, &runtime.Result{}) }()
	<-host.entered
	release()
	<-host.canceled
	replacementRelease, replacementErr := router.RegisterOwnedPeer(target.Node, provider)
	close(host.finish)
	completionErr := <-completed
	if replacementRelease != nil {
		replacementRelease()
	}
	require.Error(t, replacementErr, "replacement must wait for old completion admission to return")
	require.ErrorIs(t, completionErr, context.Canceled)
	require.Empty(t, delivered, "retired completion must not enter the mailbox")
	replacementRelease, err = router.RegisterOwnedPeer(target.Node, provider)
	require.NoError(t, err)
	defer replacementRelease()
	require.ErrorIs(t, notify(ctx, &runtime.Result{}), relay.ErrBindingRetired, "old callback must never borrow replacement authority")
	binding, ok := router.LookupLocalPeer(target.Node)
	require.True(t, ok)
	require.True(t, binding.Current())
}
