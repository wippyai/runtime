// SPDX-License-Identifier: MPL-2.0
package relay

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/pid"
	api "github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/system/eventbus"
)

type bindingReceiver struct{ calls int }

func (r *bindingReceiver) Send(p *api.Package) error { r.calls++; api.ReleasePackage(p); return nil }
func (r *bindingReceiver) SendContext(ctx context.Context, p *api.Package) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.Send(p)
}

func TestOwnedPeerBindingDoesNotRevokeReplacement(t *testing.T) {
	router := NewRouter(NewNode("local"), nil)
	receiver := &bindingReceiver{}
	release, err := router.RegisterOwnedPeer("provider", receiver)
	require.NoError(t, err)
	original, ok := router.LookupLocalPeer("provider")
	require.True(t, ok)
	require.Same(t, receiver, original.Receiver())
	require.True(t, original.Current())
	_, err = router.RegisterOwnedPeer("provider", &bindingReceiver{})
	require.Error(t, err)
	require.True(t, original.Current())
	require.True(t, router.UnregisterPeer("provider"))
	require.False(t, original.Current())
	// Even the same receiver object has a different registration lifetime.
	replacementRelease, err := router.RegisterOwnedPeer("provider", receiver)
	require.NoError(t, err)
	replacement, ok := router.LookupLocalPeer("provider")
	require.True(t, ok)
	require.NotSame(t, original, replacement)
	release()
	release()
	require.True(t, replacement.Current())
	p := api.NewPackage(pid.PID{Node: "local"}, pid.PID{Node: "provider"}, "data")
	require.NoError(t, router.SendContext(context.Background(), p))
	require.Equal(t, 1, receiver.calls, "registration must preserve cancellable receiver capability")
	replacementRelease()
	require.False(t, replacement.Current())
}

func TestLocalPeerLookupDoesNotAuthorizeInternodeFallback(t *testing.T) {
	router := NewRouter(NewNode("local"), &bindingReceiver{})
	for _, node := range []string{"local", "remote", ""} {
		binding, ok := router.LookupLocalPeer(node)
		require.False(t, ok)
		require.Nil(t, binding)
	}
}

func TestPeerManagerOnlyReleasesOwnedRegistrations(t *testing.T) {
	router := NewRouter(NewNode("local"), nil)
	manager := NewPeerManager(router, eventbus.NewBus(), nil)
	manager.ctx = context.Background()
	info := &api.PeerInfo{NodeID: "provider", Receiver: &bindingReceiver{}}
	manager.handleRegister(event.Event{Kind: api.PeerRegister, Path: "provider", Data: info})
	old, ok := router.LookupLocalPeer("provider")
	require.True(t, ok)
	require.True(t, router.UnregisterPeer("provider"))
	replacementRelease, err := router.RegisterOwnedPeer("provider", &bindingReceiver{})
	require.NoError(t, err)
	defer replacementRelease()
	replacement, ok := router.LookupLocalPeer("provider")
	require.True(t, ok)
	manager.handleDelete(event.Event{Kind: api.PeerDelete, Path: "provider", Data: info})
	require.False(t, old.Current())
	require.True(t, replacement.Current(), "manager delete must not remove a foreign replacement")
	manager.handleRegister(event.Event{Kind: api.PeerRegister, Path: "owned", Data: &api.PeerInfo{NodeID: "owned", Receiver: &bindingReceiver{}}})
	owned, ok := router.LookupLocalPeer("owned")
	require.True(t, ok)
	require.NoError(t, manager.Stop())
	require.False(t, owned.Current(), "Stop must release manager-owned routes")
	require.True(t, replacement.Current())
	require.Error(t, manager.Start(context.Background()), "stopped manager cannot reopen with spent stopOnce")
}

func TestPeerManagerDelayedDeletePreservesNewLifetime(t *testing.T) {
	router := NewRouter(NewNode("local"), nil)
	manager := NewPeerManager(router, eventbus.NewBus(), nil)
	manager.ctx = context.Background()
	defer manager.Stop()
	receiver := &bindingReceiver{}
	first := &api.PeerInfo{NodeID: "provider", Receiver: receiver}
	register := func(info *api.PeerInfo) {
		manager.handleRegister(event.Event{Kind: api.PeerRegister, Path: info.NodeID, Data: info})
	}
	remove := func(data any) {
		manager.handleDelete(event.Event{Kind: api.PeerDelete, Path: "provider", Data: data})
	}
	register(first)
	remove(first)
	second := &api.PeerInfo{NodeID: "provider", Receiver: receiver}
	register(second)
	binding, ok := router.LookupLocalPeer("provider")
	require.True(t, ok)
	for _, stale := range []any{first, nil, (*api.PeerInfo)(nil), api.PeerInfo{NodeID: "provider", Receiver: receiver}, &api.PeerInfo{NodeID: "provider", Receiver: receiver}, &api.PeerInfo{NodeID: "other", Receiver: receiver}} {
		remove(stale)
		require.True(t, binding.Current(), "only the exact registration can remove its route: %T", stale)
	}
	remove(second)
	require.False(t, binding.Current())
	remove(second) // exact deletion is idempotent
}

func TestPeerManagerRejectsMismatchedRegistrationPath(t *testing.T) {
	router := NewRouter(NewNode("local"), nil)
	manager := NewPeerManager(router, eventbus.NewBus(), nil)
	manager.ctx = context.Background()
	defer manager.Stop()
	manager.handleRegister(event.Event{Kind: api.PeerRegister, Path: "other", Data: &api.PeerInfo{NodeID: "provider", Receiver: &bindingReceiver{}}})
	_, ok := router.LookupLocalPeer("provider")
	require.False(t, ok)
}

func TestPeerManagerDeleteBeforeRegisterCannotResurrectLifetime(t *testing.T) {
	router := NewRouter(NewNode("local"), nil)
	manager := NewPeerManager(router, eventbus.NewBus(), nil)
	manager.ctx = context.Background()
	defer manager.Stop()
	info := &api.PeerInfo{NodeID: "provider", Receiver: &bindingReceiver{}}
	manager.handleDelete(event.Event{Kind: api.PeerDelete, Path: "provider", Data: info})
	manager.handleRegister(event.Event{Kind: api.PeerRegister, Path: "provider", Data: info})
	_, ok := router.LookupLocalPeer("provider")
	require.False(t, ok, "delayed registration must not resurrect a retired receiver")
	fresh := &api.PeerInfo{NodeID: "provider", Receiver: info.Receiver}
	manager.handleRegister(event.Event{Kind: api.PeerRegister, Path: "provider", Data: fresh})
	binding, ok := router.LookupLocalPeer("provider")
	require.True(t, ok)
	manager.handleDelete(event.Event{Kind: api.PeerDelete, Path: "provider", Data: info})
	require.True(t, binding.Current())
}

func TestRetiringPeerCannotFallThroughToPhysicalTransport(t *testing.T) {
	fallback := &bindingReceiver{}
	router := NewRouter(NewNode("local"), fallback)
	receiver := &bindingReceiver{}
	release, err := router.RegisterOwnedPeer("provider", receiver)
	require.NoError(t, err)
	binding, ok := router.LookupLocalPeer("provider")
	require.True(t, ok)
	require.NoError(t, binding.WithReceiver(context.Background(), func(ctx context.Context, actual api.Receiver) error {
		require.Same(t, receiver, actual)
		release() // retirement inside admitted code must not deadlock
		require.False(t, binding.Current())
		require.Error(t, router.RegisterPeer("provider", receiver), "reentrant retirement cannot enable early reuse")
		pkg := api.NewPackage(pid.PID{}, pid.PID{Node: "provider", Host: "app"}, "data")
		require.ErrorIs(t, router.SendContext(context.Background(), pkg), api.ErrBindingRetired)
		api.ReleasePackage(pkg)
		require.Zero(t, fallback.calls, "reserved local provider address must never become physical fallback")
		return nil
	}))
	require.NoError(t, router.RegisterPeer("provider", receiver))
	require.ErrorIs(t, binding.WithReceiver(context.Background(), func(context.Context, api.Receiver) error {
		t.Fatal("retired registration admitted callback")
		return nil
	}), api.ErrBindingRetired)
}
