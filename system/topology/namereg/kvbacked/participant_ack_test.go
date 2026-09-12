// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

func pendingParticipantForTest(t *testing.T, r *Service, incarnation string) (pendingHeader, kvapi.Entry) {
	t.Helper()
	owner := mkPID("node-1", "owner")
	hdr := pendingHeader{Name: "claim", PID: owner.String(), NodeID: owner.Node, RequiredNodes: []string{owner.Node}, RequiredIncarnations: map[string]string{owner.Node: incarnation}, DeadlineUnixNano: time.Now().Add(time.Minute).UnixNano()}
	return seedStrongPending(t, r, hdr)
}

func TestParticipantWrongIncarnationCannotAcknowledge(t *testing.T) {
	r := newStrongReg(t, []string{"node-1"}, 0, nil)
	r.strong.incarnation = "old"
	hdr, entry := pendingParticipantForTest(t, r, "replacement")
	owner := mkPID("node-1", "owner")
	err := r.strong.attestHeader(hdr.Name, entry.Epoch, owner, hdr)
	require.ErrorIs(t, err, ErrParticipantIncarnationConflict)
	_, err = r.engine.Get(hdr.ackKey(hdr.Name, entry.Epoch, "node-1"))
	require.ErrorIs(t, err, kvapi.ErrKeyNotFound)
	_, held := r.IsStrongReserved(hdr.Name)
	require.False(t, held)
}

func TestParticipantOldVotesCannotPromoteOrRejectReplacement(t *testing.T) {
	r := newStrongReg(t, []string{"node-1"}, 0, nil)
	r.strong.incarnation = "replacement"
	hdr, entry := pendingParticipantForTest(t, r, "replacement")
	old := hdr
	old.RequiredIncarnations = map[string]string{"node-1": "old"}
	_, err := r.engine.Set(old.ackKey(hdr.Name, entry.Epoch, "node-1"), []byte("node-1"))
	require.NoError(t, err)
	_, err = r.engine.Set(old.rejectKey(hdr.Name, entry.Epoch, "node-1"), []byte(strongRejectConflict))
	require.NoError(t, err)
	_, err = r.engine.Set(ackKey(hdr.Name, entry.Epoch, "node-1"), []byte("node-1"))
	require.NoError(t, err)
	require.False(t, r.strong.complete(hdr.Name, entry.Epoch, hdr))
	r.strong.leaderPromote(hdr.Name, entry.Epoch, entry.Version, hdr)
	_, err = r.engine.Get(activeKey(hdr.Name))
	require.ErrorIs(t, err, kvapi.ErrKeyNotFound)
	owner := mkPID("node-1", "owner")
	require.NoError(t, r.strong.attestHeader(hdr.Name, entry.Epoch, owner, hdr))
	require.True(t, r.strong.complete(hdr.Name, entry.Epoch, hdr))
	r.strong.leaderDrive(hdr.Name, entry.Epoch, entry.Version, hdr)
	active, err := r.engine.Get(activeKey(hdr.Name))
	require.NoError(t, err)
	stored, err := decodeActive(active.Value)
	require.NoError(t, err)
	require.Equal(t, hdr.RequiredIncarnations, stored.RequiredIncarnations)
	_, err = r.engine.Get(hdr.ackKey(hdr.Name, entry.Epoch, "node-1"))
	require.ErrorIs(t, err, kvapi.ErrKeyNotFound, "promotion must clean only its vote")
	r.DropNode("node-1")
	// A receiver without local participation configuration must also preserve
	// bound claims based on the record, rather than its own process role.
	observer := NewService(r.engine, "observer", nil, nil)
	observer.DropNode("node-1")
	_, err = r.engine.Get(activeKey(hdr.Name))
	require.NoError(t, err, "gossip departure cannot remove enrolled claims")
}

func TestParticipantPendingCannotBePrunedFromDiscovery(t *testing.T) {
	r := newStrongReg(t, []string{"node-1"}, 0, nil)
	hdr, entry := pendingParticipantForTest(t, r, "current")
	hdr.RequiredNodes = append(hdr.RequiredNodes, "missing")
	hdr.RequiredIncarnations["missing"] = "still-live"
	body, err := encode(hdr)
	require.NoError(t, err)
	_, err = r.engine.Set(entry.Key, body)
	require.NoError(t, err)
	require.False(t, r.strong.pruneDepartedRequired(hdr.Name, hdr))
	r.strong.dropNodeFromPending(hdr.Name, "missing")
	unchanged, err := r.engine.Get(entry.Key)
	require.NoError(t, err)
	stored, err := decodePending(unchanged.Value)
	require.NoError(t, err)
	require.Equal(t, hdr.RequiredIncarnations, stored.RequiredIncarnations)
	require.Equal(t, hdr.RequiredNodes, stored.RequiredNodes)
}

type expireBeforeParticipantVote struct {
	kvapi.Engine
	pending string
}

func (e *expireBeforeParticipantVote) Txn(ops []kvapi.TxnOp) (bool, error) {
	for _, op := range ops {
		if op.Kind == kvapi.TxnPut && op.Key != e.pending {
			if err := e.Engine.Delete(e.pending); err != nil {
				return false, err
			}
			break
		}
	}
	return e.Engine.Txn(ops)
}

func TestParticipantLateVoteDoesNotSurviveReservation(t *testing.T) {
	r := newStrongReg(t, []string{"node-1"}, 0, nil)
	r.strong.incarnation = "current"
	hdr, entry := pendingParticipantForTest(t, r, "current")
	r.engine = &expireBeforeParticipantVote{Engine: r.engine, pending: entry.Key}
	owner := mkPID("node-1", "owner")
	require.NoError(t, r.strong.attestHeader(hdr.Name, entry.Epoch, owner, hdr), "an obsolete vote must not stop synchronization")
	_, err := r.engine.Get(hdr.ackKey(hdr.Name, entry.Epoch, "node-1"))
	require.ErrorIs(t, err, kvapi.ErrKeyNotFound, "expired pending must prevent delayed ACK creation")
	require.NoError(t, r.strong.reconcile(hdr.Name))
	_, held := r.IsStrongReserved(hdr.Name)
	require.False(t, held, "reconciliation releases conservative exclusion after failed vote")
}

func TestParticipantExpiryCleansItsVotes(t *testing.T) {
	r := newStrongReg(t, []string{"node-1"}, 0, nil)
	r.strong.incarnation = "current"
	hdr, entry := pendingParticipantForTest(t, r, "current")
	for _, key := range []string{hdr.ackKey(hdr.Name, entry.Epoch, "node-1"), hdr.rejectKey(hdr.Name, entry.Epoch, "node-1")} {
		_, err := r.engine.Set(key, []byte("vote"))
		require.NoError(t, err)
	}
	r.strong.leaderExpire(hdr.Name, entry.Epoch, entry.Version, hdr, "unreserve")
	for _, key := range []string{entry.Key, hdr.ackKey(hdr.Name, entry.Epoch, "node-1"), hdr.rejectKey(hdr.Name, entry.Epoch, "node-1")} {
		_, err := r.engine.Get(key)
		require.ErrorIs(t, err, kvapi.ErrKeyNotFound)
	}
}

func (e *expireBeforeParticipantVote) ReadLocalSnapshot(keys []string) (map[string]kvapi.Entry, uint64, error) {
	return e.Engine.(kvapi.LocalSnapshotReader).ReadLocalSnapshot(keys)
}

type participantNoScanEngine struct{ kvapi.Engine }

func (e participantNoScanEngine) Scan(string, func(kvapi.Entry) bool) error {
	return ErrParticipantInventoryBusy // a full scan is forbidden by this fixture
}
func (e participantNoScanEngine) ReadLocalSnapshot(keys []string) (map[string]kvapi.Entry, uint64, error) {
	return e.Engine.(kvapi.LocalSnapshotReader).ReadLocalSnapshot(keys)
}
func TestParticipantVoteEventReconcilesOnlyItsName(t *testing.T) {
	r := newStrongReg(t, []string{"node-1"}, 0, nil)
	r.strong.incarnation = "current"
	hdr, entry := pendingParticipantForTest(t, r, "current")
	r.engine = participantNoScanEngine{Engine: r.engine}
	err := r.handleWatchEvent(kvapi.WatchEvent{Type: kvapi.WatchPut, Current: &kvapi.Entry{Key: hdr.ackKey(hdr.Name, entry.Epoch, "node-1")}})
	require.NoError(t, err, "participant vote must not scan unrelated pending claims")
	_, err = r.engine.Get(activeKey(hdr.Name))
	require.NoError(t, err)
}

type unavailableParticipantLeaderRead struct{ calls int }

func (r *unavailableParticipantLeaderRead) GetViaLeader(string) (kvapi.Entry, error) {
	r.calls++
	return kvapi.Entry{}, errParticipantUnavailable
}
func TestParticipantMemberVoteReadsDoNotRequireLeaderRoundTrips(t *testing.T) {
	for _, nonMember := range []bool{false, true} {
		t.Run(map[bool]string{false: "replicated-member", true: "forwarding-client"}[nonMember], func(t *testing.T) {
			r := newStrongReg(t, []string{"node-1"}, 0, nil)
			r.strong.incarnation = "current"
			hdr, entry := pendingParticipantForTest(t, r, "current")
			leader := &unavailableParticipantLeaderRead{}
			r.leaderRead = leader
			r.SetNonMember(func() bool { return nonMember })
			err := r.strong.attestHeader(hdr.Name, entry.Epoch, mkPID("node-1", "owner"), hdr)
			if nonMember {
				require.ErrorIs(t, err, errParticipantUnavailable)
				require.Equal(t, 1, leader.calls)
				_, err = r.engine.Get(hdr.ackKey(hdr.Name, entry.Epoch, "node-1"))
				require.ErrorIs(t, err, kvapi.ErrKeyNotFound, "client must not substitute its local replica")
			} else {
				require.NoError(t, err)
				require.Zero(t, leader.calls)
				_, err = r.engine.Get(hdr.ackKey(hdr.Name, entry.Epoch, "node-1"))
				require.NoError(t, err)
			}
		})
	}
}

type uncertainParticipantVoteEngine struct {
	kvapi.Engine
	calls   int
	failure error
}

func (e *uncertainParticipantVoteEngine) Txn(ops []kvapi.TxnOp) (bool, error) {
	e.calls++
	committed, err := e.Engine.Txn(ops)
	if err != nil {
		return committed, err
	}
	return false, e.failure // result was lost after the transaction committed
}
func TestParticipantCommittedVoteErrorIsNotReplayed(t *testing.T) {
	r := newStrongReg(t, []string{"node-1"}, 0, nil)
	r.strong.incarnation = "current"
	hdr, entry := pendingParticipantForTest(t, r, "current")
	uncertain := &uncertainParticipantVoteEngine{Engine: r.engine, failure: errParticipantUnavailable}
	r.engine = uncertain
	owner := mkPID("node-1", "owner")
	require.ErrorIs(t, r.strong.attestHeader(hdr.Name, entry.Epoch, owner, hdr), errParticipantUnavailable)
	require.Equal(t, 1, uncertain.calls)
	held, ok := r.IsStrongReserved(hdr.Name)
	require.True(t, ok)
	require.True(t, held.Equal(owner), "uncertainty must retain exclusion")
	vote, err := r.engine.Get(hdr.ackKey(hdr.Name, entry.Epoch, "node-1"))
	require.NoError(t, err)
	// A new observation sees the committed vote and restores its exclusion;
	// it does not retry the previous mutation based on its ambiguous error.
	require.NoError(t, r.strong.attestHeader(hdr.Name, entry.Epoch, owner, hdr))
	require.Equal(t, 1, uncertain.calls)
	after, err := r.engine.Get(vote.Key)
	require.NoError(t, err)
	require.Equal(t, vote.Version, after.Version)
}
