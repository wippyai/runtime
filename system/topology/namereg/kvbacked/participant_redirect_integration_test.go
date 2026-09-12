// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"context"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	"testing"
	"time"
)

type hintedParticipantSource struct {
	participantSnapshotSource
	peer string
}

func (s hintedParticipantSource) SnapshotAuthority() (string, error) { return s.peer, nil }

func TestParticipantRedirectReachesDirectAuthority(t *testing.T) {
	for _, limit := range []int{0, 1} {
		t.Run(string(rune('0'+limit)), func(t *testing.T) {
			ctx := context.Background()
			inventory, engine := newParticipantTestInventory(t, 4)
			followerInventory, followerEngine := newParticipantTestInventory(t, 4)
			limits := participantSnapshotLimits{MaxEntries: 4, MaxValueBytes: 4096}
			mesh := &participantTestMesh{nodes: make(map[pid.NodeID]relay.ContextSender)}
			authority, err := newParticipantAuthority(ctx, "leader", inventory, engine, limits, 1)
			require.NoError(t, err)
			follower, err := newParticipantAuthority(ctx, "follower", followerInventory, hintedParticipantSource{followerEngine, "leader"}, limits, 1)
			require.NoError(t, err)
			leaderReceiver, err := newParticipantReceiver(authority, mesh, 8192, 4, time.Second)
			require.NoError(t, err)
			followerReceiver, err := newParticipantReceiver(follower, mesh, 8192, 4, time.Second)
			require.NoError(t, err)
			client, err := newParticipantClient(ctx, "client", "one", func(context.Context) (pid.NodeID, error) { return "follower", nil }, mesh, 8192, 4, time.Second)
			require.NoError(t, err)
			client.maxRedirects = limit
			mesh.nodes["leader"], mesh.nodes["follower"], mesh.nodes["client"] = leaderReceiver, followerReceiver, client
			t.Cleanup(func() {
				require.NoError(t, client.stop(ctx))
				require.NoError(t, followerReceiver.stop(ctx))
				require.NoError(t, leaderReceiver.stop(ctx))
			})
			snapshot, err := inventory.captureSnapshot(ctx, client, "client", "one", limits)
			if limit == 0 {
				require.Error(t, err)
				require.Nil(t, snapshot)
			} else {
				require.NoError(t, err)
				require.NotZero(t, snapshot.Revision)
				members, _, err := inventory.readSnapshot()
				require.NoError(t, err)
				require.Equal(t, "one", members["client"])
				require.NotContains(t, members, "follower")
			}
			_, _, err = followerInventory.readSnapshot()
			require.ErrorIs(t, err, kvapi.ErrKeyNotFound, "redirect must precede enrollment")
		})
	}
}

// Redirect responses remain subject to the same authenticated peer binding as
// snapshots. Repeated targets cannot consume the entire configured retry budget.
type loopingParticipantRedirects struct {
	client *participantClient
	t      *testing.T
	calls  int
}

func (r *loopingParticipantRedirects) SendContext(ctx context.Context, pkg *relay.Package) error {
	defer relay.ReleasePackage(pkg)
	r.calls++
	request, err := decodeParticipantRequest(pkg.Messages[0].Payloads[0].Data().([]byte), 8192)
	require.NoError(r.t, err)
	next := pid.NodeID("second")
	if pkg.Target.Node == "second" {
		next = "first"
	}
	body, err := encodeParticipantRedirect(request.Correlation, request.Incarnation, next, 8192)
	require.NoError(r.t, err)
	spoof := relay.NewServicePackage(pkg.Target.Node, RegistryHostID, "client", RegistryHostID, participantSnapshotRedirectTopic, payload.New(body))
	spoof.ReceivedFrom = "intruder"
	require.ErrorIs(r.t, r.client.SendContext(ctx, spoof), errParticipantPeerMismatch)
	relay.ReleasePackage(spoof)
	reply := relay.NewServicePackage(pkg.Target.Node, RegistryHostID, "client", RegistryHostID, participantSnapshotRedirectTopic, payload.New(body))
	reply.ReceivedFrom = pkg.Target.Node
	return r.client.SendContext(ctx, reply)
}
func TestParticipantRedirectCycleStops(t *testing.T) {
	ctx := context.Background()
	router := &loopingParticipantRedirects{t: t}
	client, err := newParticipantClient(ctx, "client", "one", func(context.Context) (pid.NodeID, error) { return "first", nil }, router, 8192, 4, time.Second)
	require.NoError(t, err)
	client.maxRedirects = 100
	router.client = client
	defer client.stop(ctx)
	index, err := client.ScanAtIndex(registryPrefix, func(kvapi.Entry) bool { t.Fatal("redirect exposed data"); return true })
	require.ErrorIs(t, err, errParticipantUnavailable)
	require.Zero(t, index)
	require.Equal(t, 2, router.calls)
}
