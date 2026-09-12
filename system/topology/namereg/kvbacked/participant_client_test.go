// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	localrelay "github.com/wippyai/runtime/system/relay"
)

type participantTestMesh struct {
	nodes map[pid.NodeID]relay.ContextSender
}

func (r *participantTestMesh) SendContext(ctx context.Context, pkg *relay.Package) error {
	pkg.ReceivedFrom = pkg.Source.Node // simulated native connection, not wire metadata
	return r.nodes[pkg.Target.Node].SendContext(ctx, pkg)
}
func TestParticipantClientCapturesPeerBoundAuthoritySnapshot(t *testing.T) {
	inventory, engine := newParticipantTestInventory(t, 4)
	ctx := context.Background()
	limits := participantSnapshotLimits{MaxEntries: 4, MaxValueBytes: 4096}
	authority, err := newParticipantAuthority(ctx, "authority", inventory, engine, limits, 1)
	require.NoError(t, err)
	mesh := &participantTestMesh{nodes: make(map[pid.NodeID]relay.ContextSender)}
	receiver, err := newParticipantReceiver(authority, mesh, 8192, 4, time.Second)
	require.NoError(t, err)
	client, err := newParticipantClient(ctx, "client", "one", func(context.Context) (pid.NodeID, error) { return "authority", nil }, mesh, 8192, 4, time.Second)
	require.NoError(t, err)
	authorityNode, clientNode := localrelay.NewNode("authority"), localrelay.NewNode("client")
	require.NoError(t, authorityNode.RegisterHost(RegistryHostID, &participantHost{registry: NewService(engine, "authority", nil, nil), authority: receiver}))
	require.NoError(t, clientNode.RegisterHost(RegistryHostID, &participantHost{registry: NewService(engine, "client", nil, nil), client: client}))
	mesh.nodes["authority"], mesh.nodes["client"] = authorityNode, clientNode
	t.Cleanup(func() { require.NoError(t, client.stop(ctx)); require.NoError(t, receiver.stop(ctx)) })
	snapshot, err := inventory.captureSnapshot(ctx, client, "client", "one", limits)
	require.NoError(t, err)
	require.NotZero(t, snapshot.Revision)
	require.Contains(t, snapshot.Entries, participantsKey)
}

type injectingParticipantReplies struct {
	unchanged bool
	client    *participantClient
	t         *testing.T
}

func (r *injectingParticipantReplies) SendContext(ctx context.Context, pkg *relay.Package) error {
	defer relay.ReleasePackage(pkg)
	req, err := decodeParticipantRequest(pkg.Messages[0].Payloads[0].Data().([]byte), 8192)
	require.NoError(r.t, err)
	for _, candidate := range []struct {
		peer, incarnation string
		correlation       uint64
		valid             bool
	}{
		{"attacker", "one", req.Correlation, false},
		{"authority", "old-incarnation", req.Correlation, false},
		{"authority", "one", req.Correlation + 1, false},
		{"authority", "one", req.Correlation, true},
	} {
		body, err := encodeParticipantResponse(&participantWireResponse{Lifetime: "fixture-authority", Version: participantWireVersion, Correlation: candidate.correlation, Incarnation: candidate.incarnation, Revision: 7, Unchanged: r.unchanged, Entries: []kvapi.Entry{}}, 8192, 4)
		require.NoError(r.t, err)
		reply := relay.NewServicePackage("authority", RegistryHostID, "client", RegistryHostID, participantSnapshotResponseTopic, payload.New(body))
		reply.ReceivedFrom = candidate.peer
		err = r.client.SendContext(ctx, reply)
		if candidate.peer != "authority" {
			require.ErrorIs(r.t, err, errParticipantPeerMismatch)
			relay.ReleasePackage(reply)
		} else {
			require.NoError(r.t, err)
		}
		if !candidate.valid {
			r.client.mu.Lock()
			require.Empty(r.t, r.client.pending.result, "stale or wrong-peer reply completed current request")
			r.client.mu.Unlock()
		}
	}
	return nil
}
func TestParticipantClientRejectsWrongPeerIncarnationAndCorrelation(t *testing.T) {
	router := &injectingParticipantReplies{t: t}
	client, err := newParticipantClient(context.Background(), "client", "one", func(context.Context) (pid.NodeID, error) { return "authority", nil }, router, 8192, 4, time.Second)
	require.NoError(t, err)
	router.client = client
	defer client.stop(context.Background())
	revision, err := client.ScanAtIndexContext(context.Background(), registryPrefix, func(kvapi.Entry) bool { return true })
	require.NoError(t, err)
	require.EqualValues(t, 7, revision)
}

type droppedParticipantRequests struct{ entered chan struct{} }

func (r droppedParticipantRequests) SendContext(_ context.Context, pkg *relay.Package) error {
	relay.ReleasePackage(pkg)
	close(r.entered)
	return nil
}
func TestParticipantClientStopJoinsPendingReplyWait(t *testing.T) {
	router := droppedParticipantRequests{entered: make(chan struct{})}
	client, err := newParticipantClient(context.Background(), "client", "one", func(context.Context) (pid.NodeID, error) { return "authority", nil }, router, 8192, 4, time.Hour)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		_, err := client.ScanAtIndex(registryPrefix, func(kvapi.Entry) bool { return true })
		done <- err
	}()
	select {
	case <-router.entered:
	case <-time.After(time.Second):
		t.Fatal("request not admitted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, client.stop(ctx))
	require.ErrorIs(t, <-done, context.Canceled)
	client.mu.Lock()
	require.Nil(t, client.pending)
	client.mu.Unlock()
}

func TestParticipantClientRejectsUnchangedWithoutRetainedSnapshot(t *testing.T) {
	router := &injectingParticipantReplies{t: t, unchanged: true}
	ctx := context.Background()
	client, err := newParticipantClient(ctx, "client", "one", func(context.Context) (pid.NodeID, error) { return "authority", nil }, router, 8192, 4, time.Second)
	require.NoError(t, err)
	router.client = client
	defer client.stop(ctx)
	revision, err := client.ScanAtIndexContext(ctx, registryPrefix, func(kvapi.Entry) bool { t.Fatal("unchanged reply without cache visited entries"); return true })
	require.ErrorIs(t, err, errParticipantWireInvalid)
	require.Zero(t, revision)
}
