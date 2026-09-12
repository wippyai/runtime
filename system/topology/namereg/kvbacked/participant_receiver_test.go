// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
)

type participantReplyRouter struct{ replies chan *relay.Package }

func (r participantReplyRouter) SendContext(ctx context.Context, pkg *relay.Package) error {
	select {
	case r.replies <- pkg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func participantRequestPackage(t *testing.T, correlation uint64, incarnation string) *relay.Package {
	t.Helper()
	body, err := encodeParticipantRequest(&participantWireRequest{Version: participantWireVersion, Correlation: correlation, Incarnation: incarnation}, 4096)
	require.NoError(t, err)
	pkg := relay.NewServicePackage("client", RegistryHostID, "authority", RegistryHostID, participantSnapshotRequestTopic, payload.New(body))
	pkg.ReceivedFrom = "client"
	return pkg
}

func TestParticipantReceiverServesSnapshotAndIncarnationFailure(t *testing.T) {
	inventory, engine := newParticipantTestInventory(t, 4)
	authority, err := newParticipantAuthority(context.Background(), "authority", inventory, engine, participantSnapshotLimits{MaxEntries: 4, MaxValueBytes: 4096}, 1)
	require.NoError(t, err)
	router := participantReplyRouter{replies: make(chan *relay.Package, 1)}
	receiver, err := newParticipantReceiver(authority, router, 8192, 4, time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, receiver.stop(context.Background())) })
	request := participantRequestPackage(t, 10, "one")
	require.NoError(t, receiver.Send(request))
	var reply *relay.Package
	select {
	case reply = <-router.replies:
	case <-time.After(time.Second):
		t.Fatal("no snapshot reply")
	}
	require.Equal(t, "client", reply.Target.Node)
	require.Equal(t, "authority", reply.Source.Node)
	require.Equal(t, participantSnapshotResponseTopic, reply.Messages[0].Topic)
	response, err := decodeParticipantResponse(reply.Messages[0].Payloads[0].Data().([]byte), 8192, 4)
	relay.ReleasePackage(reply)
	require.NoError(t, err)
	require.EqualValues(t, 10, response.Correlation)
	require.Len(t, response.Entries, 1)
	// Wait for the first accepted request to release its full reply-admission slot.
	require.Eventually(t, func() bool { return len(receiver.slots) == 0 }, time.Second, time.Millisecond)
	require.NoError(t, receiver.Send(participantRequestPackage(t, 11, "replacement")))
	select {
	case reply = <-router.replies:
	case <-time.After(time.Second):
		t.Fatal("no conflict reply")
	}
	require.Equal(t, participantSnapshotFailureTopic, reply.Messages[0].Topic)
	correlation, incarnation, err := decodeParticipantFailure(reply.Messages[0].Payloads[0].Data().([]byte), 8192)
	relay.ReleasePackage(reply)
	require.EqualValues(t, 11, correlation)
	require.Equal(t, "replacement", incarnation)
	require.ErrorIs(t, err, ErrParticipantIncarnationConflict)
}

func TestParticipantReceiverStopCancelsBlockedReplyAdmission(t *testing.T) {
	inventory, engine := newParticipantTestInventory(t, 4)
	authority, err := newParticipantAuthority(context.Background(), "authority", inventory, engine, participantSnapshotLimits{MaxEntries: 4, MaxValueBytes: 4096}, 1)
	require.NoError(t, err)
	receiver, err := newParticipantReceiver(authority, participantReplyRouter{replies: make(chan *relay.Package)}, 8192, 4, time.Hour)
	require.NoError(t, err)
	require.NoError(t, receiver.Send(participantRequestPackage(t, 1, "one")))
	require.Eventually(t, func() bool {
		members, _, err := inventory.readSnapshot()
		return err == nil && members["client"] == "one"
	}, time.Second, time.Millisecond)
	require.NoError(t, receiver.Send(participantRequestPackage(t, 2, "one")))
	refused := participantRequestPackage(t, 3, "one")
	require.ErrorIs(t, receiver.Send(refused), errParticipantAuthorityBusy)
	// Refusal did not transfer ownership or clear the caller's package.
	require.Equal(t, "client", refused.Source.Node)
	relay.ReleasePackage(refused)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, receiver.stop(ctx))
	require.Empty(t, receiver.slots)
	require.Empty(t, receiver.refusals)
}

func TestParticipantReceiverReturnsCorrelatedBusyWhileCaptureBlocked(t *testing.T) {
	inventory, engine := newParticipantTestInventory(t, 4)
	source := &gatedParticipantAuthoritySource{participantSnapshotSource: engine, entered: make(chan struct{}), release: make(chan struct{})}
	authority, err := newParticipantAuthority(context.Background(), "authority", inventory, source, participantSnapshotLimits{MaxEntries: 4, MaxValueBytes: 4096}, 1)
	require.NoError(t, err)
	router := participantReplyRouter{replies: make(chan *relay.Package, 2)}
	receiver, err := newParticipantReceiver(authority, router, 8192, 4, time.Second)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, receiver.stop(context.Background()))
		for {
			select {
			case reply := <-router.replies:
				relay.ReleasePackage(reply)
			default:
				return
			}
		}
	})
	defer close(source.release)
	require.NoError(t, receiver.Send(participantRequestPackage(t, 1, "one")))
	select {
	case <-source.entered:
	case <-time.After(time.Second):
		t.Fatal("capture not entered")
	}
	request := participantRequestPackage(t, 2, "replacement")
	err = receiver.Send(request)
	if err != nil {
		relay.ReleasePackage(request)
	}
	require.NoError(t, err, "accept bounded refusal work so native callers receive a correlated busy reply")
	select {
	case reply := <-router.replies:
		defer relay.ReleasePackage(reply)
		require.Equal(t, participantSnapshotFailureTopic, reply.Messages[0].Topic)
		correlation, incarnation, err := decodeParticipantFailure(reply.Messages[0].Payloads[0].Data().([]byte), 8192)
		require.EqualValues(t, 2, correlation)
		require.Equal(t, "replacement", incarnation)
		require.ErrorIs(t, err, errParticipantAuthorityBusy)
	case <-time.After(time.Second):
		t.Fatal("busy reply was held behind snapshot capture")
	}
	members, _, err := inventory.readSnapshot()
	require.NoError(t, err)
	require.Equal(t, "one", members["client"], "refusal must not attempt enrollment")
}
