// SPDX-License-Identifier: MPL-2.0
package client

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	api "github.com/wippyai/runtime/api/service/temporal"
	relayimpl "github.com/wippyai/runtime/system/relay"
)

func TestManagerPeerEventsRetainExactLifetime(t *testing.T) {
	manager, bus := setupManager(t)
	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	relay.WithRouter(ctx, relayimpl.NewRouter(relayimpl.NewNode("local"), nil))
	id := registry.NewID("test", "peer")
	cfg := &api.ClientConfig{Address: "localhost:7233", Namespace: "default", Auth: api.AuthConfig{Type: api.AuthTypeNone}}
	require.NoError(t, manager.AddClient(ctx, id, cfg))
	first := manager.peerRegistrations[id]
	require.NotNil(t, first)
	require.False(t, first.Retired())
	require.NoError(t, manager.DeleteClient(ctx, id))
	require.True(t, first.Retired(), "producer must fence a registration still queued at teardown")
	require.Empty(t, manager.peerRegistrations)
	require.NoError(t, manager.AddClient(ctx, id, cfg))
	second := manager.peerRegistrations[id]
	require.NotSame(t, first, second)
	require.False(t, second.Retired())
	require.NoError(t, manager.DeleteClient(ctx, id))
	var registrations, deletions []*relay.PeerInfo
	for _, evt := range bus.events {
		switch evt.Kind {
		case relay.PeerRegister:
			registrations = append(registrations, evt.Data.(*relay.PeerInfo))
		case relay.PeerDelete:
			deletions = append(deletions, evt.Data.(*relay.PeerInfo))
		}
	}
	require.Len(t, registrations, 2)
	require.Len(t, deletions, 2)
	require.Same(t, first, registrations[0])
	require.Same(t, first, deletions[0])
	require.Same(t, second, registrations[1])
	require.Same(t, second, deletions[1])
}
