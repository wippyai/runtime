// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/cluster/internode"
)

func TestReaperHandlesNativeCodecExit(t *testing.T) {
	_, engine := newParticipantTestInventory(t, 4)
	registry := NewService(engine, "receiver", nil, nil)
	owner := mkPID("sender", "process")
	_, err := registry.Register(context.Background(), "wire-owned", owner)
	require.NoError(t, err)
	pkg := relay.NewPackage(owner, registry.self, topology.TopicEvents, payload.New(&topology.ExitEvent{From: owner, Kind: topology.Exit}))
	codec := internode.NewMessageCodec(nil)
	body, err := codec.Encode(pkg)
	relay.ReleasePackage(pkg)
	require.NoError(t, err)
	decoded, err := codec.Decode(body)
	require.NoError(t, err)
	decoded.ReceivedFrom = owner.Node
	t.Logf("native event: %#v", decoded.Messages[0].Payloads[0].Data())
	require.NoError(t, registry.Send(decoded))
	found, err := registry.Lookup(context.Background(), "wire-owned")
	require.NoError(t, err)
	require.False(t, found.Found)
}

func TestReaperRejectsNonExitEvidence(t *testing.T) {
	owner := mkPID("sender", "process")
	for _, value := range []any{
		(*topology.ExitEvent)(nil),
		&topology.ExitEvent{From: owner, Kind: topology.LinkDown},
		map[string]any{"kind": topology.LinkDown, "from": owner},
		map[string]any{"kind": topology.Exit, "from": owner.String()},
		map[string]any{"from": owner},
	} {
		_, ok := registryExitOwner(value)
		require.False(t, ok, "not process-exit evidence: %#v", value)
	}
}

func TestReaperExitMustMatchSenderAndAuthenticatedPeer(t *testing.T) {
	for _, mode := range []string{"different-source-process", "different-connection-peer"} {
		t.Run(mode, func(t *testing.T) {
			_, engine := newParticipantTestInventory(t, 4)
			registry := NewService(engine, "receiver", nil, nil)
			owner := mkPID("owner-node", "victim")
			_, err := registry.Register(context.Background(), "protected-name", owner)
			require.NoError(t, err)
			source := owner
			ingress := owner.Node
			if mode == "different-source-process" {
				source = mkPID(owner.Node, "another-process")
			}
			if mode == "different-connection-peer" {
				ingress = "another-node"
			}
			original := relay.NewPackage(source, registry.self, topology.TopicEvents, payload.New(&topology.ExitEvent{From: owner, Kind: topology.Exit}))
			codec := internode.NewMessageCodec(nil)
			wire, err := codec.Encode(original)
			relay.ReleasePackage(original)
			require.NoError(t, err)
			decoded, err := codec.Decode(wire)
			require.NoError(t, err)
			decoded.ReceivedFrom = ingress // native transport owns this field
			require.NoError(t, registry.Send(decoded))
			found, err := registry.Lookup(context.Background(), "protected-name")
			require.NoError(t, err)
			require.True(t, found.Found, "payload must not grant authority to reap another process or node")
		})
	}
}
