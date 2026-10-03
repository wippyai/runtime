// SPDX-License-Identifier: MPL-2.0

package core

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
	contextapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	securityapi "github.com/wippyai/runtime/api/security"
	policyapi "github.com/wippyai/runtime/api/service/security/policy"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/system/eventbus"
	syspayload "github.com/wippyai/runtime/system/payload"
)

func TestSecurityPolicyBootAcknowledgementIncludesOwnerVisibility(t *testing.T) {
	ctx := contextapi.NewRootContext()
	bus := eventbus.NewBus()
	t.Cleanup(bus.Stop)
	ctx = event.WithBus(ctx, bus)
	dtt := bootpkg.ConfigureTranscoder(ctx, syspayload.NewTranscoder())
	ctx = payload.WithTranscoder(ctx, dtt)
	handlers := bootpkg.NewHandlerRegistry()
	ctx = bootpkg.WithHandlerRegistry(ctx, handlers)
	component := Security()
	ctx, err := component.Load(ctx)
	require.NoError(t, err)
	require.NoError(t, component.(boot.Starter).Start(ctx))
	t.Cleanup(func() { require.NoError(t, component.(boot.Stopper).Stop(ctx)) })
	ctx, err = SecurityPolicy().Load(ctx)
	require.NoError(t, err)
	require.Len(t, handlers.Handlers(), 2)
	owner, ok := securityapi.GetRegistry(ctx)
	require.True(t, ok)
	replies := make(chan event.Event, 3)
	sub, err := bus.Subscribe(ctx, registry.System, replies)
	require.NoError(t, err)
	t.Cleanup(func() { bus.Unsubscribe(ctx, sub) })
	entry := registry.Entry{
		ID: registry.NewID("test", "boot_policy"), Kind: policyapi.Policy,
		Data: payload.New(map[string]any{"policy": map[string]any{"actions": "*", "resources": "*", "effect": "allow"}, "groups": []string{"boot_group"}}),
	}
	for _, kind := range []event.Kind{registry.EntryCreate, registry.EntryUpdate, registry.EntryDelete} {
		require.NoError(t, handlers.Handlers()[0].Handle(ctx, event.Event{System: registry.System, Kind: kind, Path: entry.ID.String(), Data: entry}))
		select {
		case reply := <-replies:
			require.Equal(t, registry.EntryAccept, reply.Kind)
			require.Equal(t, entry.ID.String(), reply.Path)
		case <-time.After(time.Second):
			t.Fatal("boot handler did not acknowledge applied policy")
		}
		_, err := owner.GetPolicy(entry.ID)
		_, groupErr := owner.GetPolicyGroup(registry.NewID("test", "boot_group"))
		if kind == registry.EntryDelete {
			require.ErrorIs(t, err, securityapi.ErrPolicyNotFound)
			require.ErrorIs(t, groupErr, securityapi.ErrGroupNotFound)
		} else {
			require.NoError(t, err)
			require.NoError(t, groupErr)
		}
	}
	require.NoError(t, handlers.Handlers()[0].Handle(ctx, event.Event{System: registry.System, Kind: registry.EntryUpdate, Path: entry.ID.String(), Data: entry}))
	select {
	case reply := <-replies:
		require.Equal(t, registry.EntryReject, reply.Kind)
		require.ErrorIs(t, reply.Data.(error), securityapi.ErrPolicyNotFound)
	case <-time.After(time.Second):
		t.Fatal("boot handler did not reject the failed owner mutation")
	}
}

func TestSecurityPolicyBootRejectsMissingOwner(t *testing.T) {
	_, err := SecurityPolicy().Load(contextapi.NewRootContext())
	require.ErrorIs(t, err, securityapi.ErrRegistryNotFound)
}
