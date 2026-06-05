// SPDX-License-Identifier: MPL-2.0

package global

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	clusterapi "github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

// TestDissem_LWWByRaftIndex_MonotonicMerge asserts the cache only installs an
// entry when the incoming raftIndex is strictly higher than the cached entry.
// An older-index frame arriving after a newer one must be a no-op.
func TestDissem_LWWByRaftIndex_MonotonicMerge(t *testing.T) {
	d := NewDissem("node-a", nil)
	p1 := makePID("node-a", "h", "p1")
	p2 := makePID("node-a", "h", "p2")

	d.merge(bindingDelta{Name: "svc.x", PID: p1, RaftIndex: 10, Wall: time.Now().UnixNano()})
	d.merge(bindingDelta{Name: "svc.x", PID: p2, RaftIndex: 20, Wall: time.Now().UnixNano()})
	got, ok := d.Lookup("svc.x")
	require.True(t, ok)
	assert.Equal(t, p2, got, "newer index installs")

	// Older index after newer is a no-op.
	d.merge(bindingDelta{Name: "svc.x", PID: p1, RaftIndex: 15, Wall: time.Now().UnixNano()})
	got, ok = d.Lookup("svc.x")
	require.True(t, ok)
	assert.Equal(t, p2, got, "stale older-index is dropped")

	// Same index re-arriving with a different pid is also dropped (LWW is by
	// strictly-greater raftIndex; same-index ties stick to the first install).
	d.merge(bindingDelta{Name: "svc.x", PID: p1, RaftIndex: 20, Wall: time.Now().UnixNano()})
	got, ok = d.Lookup("svc.x")
	require.True(t, ok)
	assert.Equal(t, p2, got)
}

// TestDissem_TombstoneSurfacesNotFound asserts a deleted=true frame removes a
// name from Lookup, returning the same not-found shape as a never-seen name.
func TestDissem_TombstoneSurfacesNotFound(t *testing.T) {
	d := NewDissem("node-a", nil)
	p := makePID("node-a", "h", "p1")

	d.merge(bindingDelta{Name: "svc.gone", PID: p, RaftIndex: 5})
	_, ok := d.Lookup("svc.gone")
	require.True(t, ok)

	d.merge(bindingDelta{Name: "svc.gone", PID: p, RaftIndex: 9, Deleted: true})
	_, ok = d.Lookup("svc.gone")
	assert.False(t, ok, "tombstone surfaces as not-found")
}

// TestDissem_TombstoneGC_DefaultDisabled asserts the default does not age out
// tombstones without an explicit operator retention window.
func TestDissem_TombstoneGC_DefaultDisabled(t *testing.T) {
	d := NewDissem("node-a", nil)
	p := makePID("node-a", "h", "p1")

	now := time.Now().UnixNano()
	d.merge(bindingDelta{Name: "old.ts", PID: p, RaftIndex: 9, Wall: now - (365 * 24 * time.Hour).Nanoseconds(), Deleted: true})
	d.merge(bindingDelta{Name: "fresh.ts", PID: p, RaftIndex: 2, Wall: now, Deleted: true})

	removed := d.sweepTombstones(now)
	assert.Equal(t, 0, removed, "default retention must not age out delete fences")
	assert.Equal(t, 2, d.CacheSize(), "tombstones retained")
}

func TestDissem_TombstoneRetentionOption(t *testing.T) {
	d := NewDissem("node-a", nil, WithTombstoneRetention(time.Hour))
	p := makePID("node-a", "h", "p1")

	now := time.Now().UnixNano()
	d.merge(bindingDelta{Name: "old.ts", PID: p, RaftIndex: 9, Wall: now - (2 * time.Hour).Nanoseconds(), Deleted: true})
	d.merge(bindingDelta{Name: "fresh.ts", PID: p, RaftIndex: 10, Wall: now - (30 * time.Minute).Nanoseconds(), Deleted: true})

	removed := d.sweepTombstones(now)
	assert.Equal(t, 1, removed, "custom retention should expire only entries past the configured floor")
	assert.Equal(t, 1, d.CacheSize())
}

func TestDissem_TombstoneRetentionOptionRejectsNonPositive(t *testing.T) {
	d := NewDissem("node-a", nil, WithTombstoneRetention(-time.Hour))
	p := makePID("node-a", "h", "p1")

	now := time.Now().UnixNano()
	d.merge(bindingDelta{Name: "old.ts", PID: p, RaftIndex: 9, Wall: now - (365 * 24 * time.Hour).Nanoseconds(), Deleted: true})

	removed := d.sweepTombstones(now)
	assert.Equal(t, 0, removed, "bad retention values must keep the correctness-first default")
	assert.Equal(t, 1, d.CacheSize())
}

func TestDissem_TombstoneFencesStaleLiveAfterSweep(t *testing.T) {
	d := NewDissem("node-a", nil)
	p := makePID("node-a", "h", "p1")

	now := time.Now().UnixNano()
	d.merge(bindingDelta{Name: "svc.gone", PID: p, RaftIndex: 10, Wall: now, Deleted: true})
	assert.Equal(t, 0, d.sweepTombstones(now))

	d.NotifyMsg(encodeBindingFrame([]bindingDelta{{
		Name:      "svc.gone",
		PID:       p,
		RaftIndex: 7,
		Wall:      now,
	}}))

	_, ok := d.Lookup("svc.gone")
	assert.False(t, ok, "retained tombstone must reject stale lower-index live gossip")
}

func TestDissem_DigestSnapshotIncludesTombstones(t *testing.T) {
	d := NewDissem("node-a", nil)
	p := makePID("node-a", "h", "p1")
	d.merge(bindingDelta{Name: "svc.live", PID: p, RaftIndex: 7})
	d.merge(bindingDelta{Name: "svc.dead", PID: p, RaftIndex: 9, Deleted: true})

	snap := d.DigestSnapshot()
	seen := map[string]DigestEntry{}
	for _, e := range snap.Entries {
		seen[e.Name] = e
	}
	require.Contains(t, seen, "svc.live")
	require.Contains(t, seen, "svc.dead")
	assert.False(t, seen["svc.live"].Deleted)
	assert.True(t, seen["svc.dead"].Deleted)
	assert.Equal(t, uint64(9), seen["svc.dead"].RaftIndex)
}

func TestService_DigestExchangeRepairsMissedTombstone(t *testing.T) {
	router := &capturingRouter{}
	fsm := NewFSM()
	svc := NewService(newDirectApplyRaft(fsm, true), fsm, &nopBus{}, nil, router, &fakeMembership{local: "member", ids: []string{"member"}}, "member", noopLogger(), nil, nil, nil)
	d := NewDissem("member", nil)
	svc.SetDissem(d)
	d.merge(bindingDelta{Name: "svc.dead", RaftIndex: 10, Deleted: true})

	body, err := marshalMsgpack(digestEnvelope{
		Origin: "peer",
		Entries: []digestEntry{{
			Name:      "svc.dead",
			RaftIndex: 7,
		}},
		CorrID: 99,
	})
	require.NoError(t, err)

	svc.handleDigestExchange(&relay.Message{
		Topic:    topicDigestExchange,
		Payloads: payload.Payloads{payload.New(body)},
	})

	sent := router.byTopic(topicDigestDelta)
	require.Len(t, sent, 1)
	assert.Equal(t, pid.NodeID("peer"), sent[0].target)

	deltas, err := decodeBindingFrame(sent[0].body)
	require.NoError(t, err)
	require.Len(t, deltas, 1)
	assert.Equal(t, "svc.dead", deltas[0].Name)
	assert.Equal(t, uint64(10), deltas[0].RaftIndex)
	assert.True(t, deltas[0].Deleted, "missed delete must be repaired as a tombstone delta")
}

func TestService_DigestExchangeRepairsSameIndexDeleteMismatch(t *testing.T) {
	router := &capturingRouter{}
	fsm := NewFSM()
	svc := NewService(newDirectApplyRaft(fsm, true), fsm, &nopBus{}, nil, router, &fakeMembership{local: "member", ids: []string{"member"}}, "member", noopLogger(), nil, nil, nil)
	d := NewDissem("member", nil)
	svc.SetDissem(d)
	d.merge(bindingDelta{Name: "svc.dead", RaftIndex: 10, Deleted: true})

	body, err := marshalMsgpack(digestEnvelope{
		Origin: "peer",
		Entries: []digestEntry{{
			Name:      "svc.dead",
			RaftIndex: 10,
			Deleted:   false,
		}},
		CorrID: 99,
	})
	require.NoError(t, err)

	svc.handleDigestExchange(&relay.Message{
		Topic:    topicDigestExchange,
		Payloads: payload.Payloads{payload.New(body)},
	})

	sent := router.byTopic(topicDigestDelta)
	require.Len(t, sent, 1)
	deltas, err := decodeBindingFrame(sent[0].body)
	require.NoError(t, err)
	require.Len(t, deltas, 1)
	assert.True(t, deltas[0].Deleted)
	assert.Equal(t, uint64(10), deltas[0].RaftIndex)
}

func TestEncodeBindingFramesBounded(t *testing.T) {
	var deltas []bindingDelta
	for i := 0; i < 20; i++ {
		deltas = append(deltas, bindingDelta{
			Name:      fmt.Sprintf("svc.%02d.%s", i, strings.Repeat("x", 40)),
			PID:       makePID("node", "host", strings.Repeat("u", 20)),
			RaftIndex: uint64(i + 1),
			Wall:      1,
		})
	}

	frames := encodeBindingFramesBounded(deltas, 256)
	require.Greater(t, len(frames), 1)
	total := 0
	for _, frame := range frames {
		require.LessOrEqual(t, len(frame), 256)
		decoded, err := decodeBindingFrame(frame)
		require.NoError(t, err)
		total += len(decoded)
	}
	assert.Equal(t, len(deltas), total)
}

func TestEncodeBindingFramesBoundedSkipsImpossibleDelta(t *testing.T) {
	frames := encodeBindingFramesBounded([]bindingDelta{
		{
			Name:      "svc.ok",
			PID:       makePID("node", "host", "uniq"),
			RaftIndex: 1,
			Wall:      1,
		},
		{
			Name:      "svc." + strings.Repeat("x", 512),
			PID:       makePID("node", "host", "uniq"),
			RaftIndex: 2,
			Wall:      1,
		},
	}, 128)

	require.Len(t, frames, 1)
	require.LessOrEqual(t, len(frames[0]), 128)
	decoded, err := decodeBindingFrame(frames[0])
	require.NoError(t, err)
	require.Len(t, decoded, 1)
	assert.Equal(t, "svc.ok", decoded[0].Name)
}

// TestDissem_RoundTripEncoding asserts the binary frame format round-trips
// through encode + decode (no data loss). Covers the gossip wire path.
func TestDissem_RoundTripEncoding(t *testing.T) {
	deltas := []bindingDelta{
		{Name: "svc.a", PID: makePID("node-1", "host", "uniq-1"), RaftIndex: 42, Wall: 1_700_000_000_000_000_000, Deleted: false},
		{Name: "svc.b", PID: makePID("node-2", "host", "uniq-2"), RaftIndex: 99, Wall: 1_700_000_000_500_000_000, Deleted: true},
		{Name: "with.special-chars_3", PID: makePID("n", "h", "u"), RaftIndex: 1, Wall: 1, Deleted: false},
	}
	frame := encodeBindingFrame(deltas)
	require.NotEmpty(t, frame)

	got, err := decodeBindingFrame(frame)
	require.NoError(t, err)
	require.Len(t, got, len(deltas))
	for i := range deltas {
		assert.Equal(t, deltas[i].Name, got[i].Name)
		assert.Equal(t, deltas[i].PID, got[i].PID)
		assert.Equal(t, deltas[i].RaftIndex, got[i].RaftIndex)
		assert.Equal(t, deltas[i].Wall, got[i].Wall)
		assert.Equal(t, deltas[i].Deleted, got[i].Deleted)
	}
}

// TestDissem_NotifyMsgMergesIntoCache asserts an inbound gossip frame folds
// into the cache so a subsequent Lookup serves the binding.
func TestDissem_NotifyMsgMergesIntoCache(t *testing.T) {
	d := NewDissem("node-a", nil)
	owner := makePID("node-b", "h", "p1")

	delta := bindingDelta{Name: "svc.gossiped", PID: owner, RaftIndex: 7, Wall: time.Now().UnixNano()}
	frame := encodeBindingFrame([]bindingDelta{delta})

	d.NotifyMsg(frame)

	got, ok := d.Lookup("svc.gossiped")
	require.True(t, ok)
	assert.Equal(t, owner, got)
	assert.Equal(t, uint64(7), d.CachedIndex("svc.gossiped"))
}

// TestDissem_GetBroadcasts_PacksAndDrains asserts queued deltas are packed
// into frames and removed from the queue, ready for memberlist's UDP path.
func TestDissem_GetBroadcasts_PacksAndDrains(t *testing.T) {
	d := NewDissem("node-a", nil)
	for i := 0; i < 4; i++ {
		d.enqueue(bindingDelta{
			Name: "svc.x", PID: makePID("n", "h", "u"), RaftIndex: uint64(i + 1),
			Wall: time.Now().UnixNano(),
		})
	}
	frames := d.GetBroadcasts(40, dissemMaxFrameBytes)
	require.NotEmpty(t, frames)
	// Drained.
	require.Equal(t, 0, len(d.queue))

	// All four deltas should round-trip.
	count := 0
	for _, f := range frames {
		decoded, err := decodeBindingFrame(f)
		require.NoError(t, err)
		count += len(decoded)
	}
	assert.Equal(t, 4, count)
}

// TestService_Lookup_DissemCacheServesFSMMiss asserts a Service whose FSM does
// NOT carry a name serves the lookup from the dissem cache. This is the
// non-member resolution gap the dissem plane closes: gossip deposits the
// binding into the cache, FSM-miss falls back to the cache, Lookup returns the
// PID without touching the leader.
func TestService_Lookup_DissemCacheServesFSMMiss(t *testing.T) {
	svc := newJoinTestService(t)
	d := NewDissem(svc.localNode, nil)
	svc.SetDissem(d)

	owner := makePID("node-2", "host", "active")
	d.merge(bindingDelta{Name: "system.consistent", PID: owner, RaftIndex: 11})

	res, err := svc.Lookup(context.Background(), "system.consistent")
	require.NoError(t, err)
	require.True(t, res.Found, "lookup served from dissem cache on FSM miss")
	assert.Equal(t, owner, res.PID)
}

// TestService_Lookup_TombstoneNotFound asserts a cached tombstone yields a
// not-found Lookup result so unregister propagation removes the binding.
func TestService_Lookup_TombstoneNotFound(t *testing.T) {
	svc := newJoinTestService(t)
	d := NewDissem(svc.localNode, nil)
	svc.SetDissem(d)

	owner := makePID("node-2", "host", "gone")
	d.merge(bindingDelta{Name: "system.bye", PID: owner, RaftIndex: 5})
	d.merge(bindingDelta{Name: "system.bye", PID: owner, RaftIndex: 9, Deleted: true})

	res, err := svc.Lookup(context.Background(), "system.bye")
	require.NoError(t, err)
	assert.False(t, res.Found, "tombstoned binding is not found")
}

// TestFSM_OnBinding_HookedForConsistentRegister asserts applyRegister emits a
// BindingEvent with Deleted=false and the apply index as RaftIndex.
func TestFSM_OnBinding_HookedForConsistentRegister(t *testing.T) {
	fsm := NewFSM()
	var got []BindingEvent
	fsm.SetOnBinding(func(ev BindingEvent) {
		got = append(got, ev)
	})

	owner := makePID("node-1", "h", "p")
	applyAt(t, fsm, &Command{Type: CmdRegister, Name: "svc.x", PID: owner, NodeID: "node-1"}, 42)

	require.Len(t, got, 1)
	assert.Equal(t, "svc.x", got[0].Name)
	assert.Equal(t, owner, got[0].PID)
	assert.Equal(t, uint64(42), got[0].RaftIndex)
	assert.False(t, got[0].Deleted)
}

// TestFSM_OnBinding_TombstoneOnUnregister asserts applyUnregister emits a
// BindingEvent with Deleted=true and the unregister index.
func TestFSM_OnBinding_TombstoneOnUnregister(t *testing.T) {
	fsm := NewFSM()
	var got []BindingEvent
	fsm.SetOnBinding(func(ev BindingEvent) {
		got = append(got, ev)
	})

	owner := makePID("node-1", "h", "p")
	applyAt(t, fsm, &Command{Type: CmdRegister, Name: "svc.x", PID: owner, NodeID: "node-1"}, 10)
	applyAt(t, fsm, &Command{Type: CmdUnregister, Name: "svc.x"}, 20)

	require.Len(t, got, 2)
	assert.False(t, got[0].Deleted)
	assert.True(t, got[1].Deleted, "unregister emits a tombstone")
	assert.Equal(t, uint64(20), got[1].RaftIndex)
}

// TestFSM_OnBinding_StrongPromote asserts the FSM emits a BindingEvent on
// STRONG promote-to-active (the ack that completes the set).
func TestFSM_OnBinding_StrongPromote(t *testing.T) {
	fsm := NewFSM()
	var got []BindingEvent
	fsm.SetOnBinding(func(ev BindingEvent) {
		got = append(got, ev)
	})

	owner := makePID("node-1", "host", "strong-p")
	required := []pid.NodeID{"node-1"}
	openPending(t, fsm, "svc.s", owner, "node-1", required, 100)
	applyAt(t, fsm, &Command{Type: CmdRegisterAck, Name: "svc.s", Epoch: 100, AckerNode: "node-1"}, 101)

	require.NotEmpty(t, got)
	last := got[len(got)-1]
	assert.Equal(t, "svc.s", last.Name)
	assert.Equal(t, owner, last.PID)
	assert.False(t, last.Deleted)
	assert.Equal(t, uint64(101), last.RaftIndex, "binding uses the promotion index")
}

// TestService_LeaderOnly_Broadcast asserts a follower's FSM.Apply seeds the
// cache locally (LocalApply) but does NOT enqueue a broadcast. Only the
// leader injects into the gossip plane.
func TestService_LeaderOnly_Broadcast(t *testing.T) {
	fsm := NewFSM()
	mem := &fakeMembership{local: "node-1", ids: []string{"node-1"}}
	raft := newDirectApplyRaft(fsm, false) // follower
	svc := NewService(raft, fsm, &nopBus{}, nil, &nopRouter{}, mem, "node-1", noopLogger(), nil, nil, nil)
	d := NewDissem("node-1", nil)
	svc.SetDissem(d)

	// Trigger an active-binding emission directly (bypass leader gate on Apply).
	owner := makePID("node-1", "h", "p")
	svc.handleBindingEvent(BindingEvent{Name: "svc.local", PID: owner, RaftIndex: 5})

	// Cache seeded.
	got, ok := d.Lookup("svc.local")
	require.True(t, ok)
	assert.Equal(t, owner, got)

	// Broadcast queue is empty — follower does not broadcast.
	frames := d.GetBroadcasts(40, dissemMaxFrameBytes)
	assert.Empty(t, frames, "follower must not enqueue a broadcast")
}

// TestService_LeaderBroadcasts asserts a leader's FSM.Apply both seeds the
// local cache AND queues a broadcast frame for gossip dispatch.
func TestService_LeaderBroadcasts(t *testing.T) {
	fsm := NewFSM()
	mem := &fakeMembership{local: "node-1", ids: []string{"node-1"}}
	raft := newDirectApplyRaft(fsm, true)
	svc := NewService(raft, fsm, &nopBus{}, nil, &nopRouter{}, mem, "node-1", noopLogger(), nil, nil, nil)
	d := NewDissem("node-1", nil)
	svc.SetDissem(d)

	owner := makePID("node-1", "h", "p")
	svc.handleBindingEvent(BindingEvent{Name: "svc.lead", PID: owner, RaftIndex: 7})

	got, ok := d.Lookup("svc.lead")
	require.True(t, ok)
	assert.Equal(t, owner, got)

	frames := d.GetBroadcasts(40, dissemMaxFrameBytes)
	require.NotEmpty(t, frames, "leader queues a broadcast")
	decoded, err := decodeBindingFrame(frames[0])
	require.NoError(t, err)
	require.Len(t, decoded, 1)
	assert.Equal(t, "svc.lead", decoded[0].Name)
	assert.Equal(t, uint64(7), decoded[0].RaftIndex)
}

// TestJoinBarrier_SeedsConsistentEntriesIntoDissem asserts the join-epoch
// snapshot installs active CONSISTENT bindings into the joining node's dissem
// cache. After the barrier a fresh non-member resolves every pre-existing
// CONSISTENT name locally.
func TestJoinBarrier_SeedsConsistentEntriesIntoDissem(t *testing.T) {
	svc := newJoinTestService(t)
	fsm := svc.fsm

	// Seed several CONSISTENT names on the leader's FSM.
	cons1 := makePID("node-2", "h", "c1")
	cons2 := makePID("node-3", "h", "c2")
	applyAt(t, fsm, &Command{Type: CmdRegister, Name: "alpha", PID: cons1, NodeID: "node-2"}, 100)
	applyAt(t, fsm, &Command{Type: CmdRegister, Name: "beta", PID: cons2, NodeID: "node-3"}, 200)

	// Joining node side: empty dissem, run the barrier, then probe the cache.
	d := NewDissem(svc.localNode, nil)
	svc.SetDissem(d)
	require.NoError(t, svc.runJoinBarrier(svc.nodeEpoch.Load()))

	got, ok := d.Lookup("alpha")
	require.True(t, ok, "consistent name seeded from join snapshot")
	assert.Equal(t, cons1, got)
	got, ok = d.Lookup("beta")
	require.True(t, ok)
	assert.Equal(t, cons2, got)
}

// TestNonMemberResolvesConsistentAfterBroadcast simulates a leader committing a
// CONSISTENT register and gossiping the binding to a non-member. The
// non-member's Service.Lookup must resolve the name from the dissem cache
// (its local FSM never sees the commit). This is the core gap the dissem
// plane closes.
func TestNonMemberResolvesConsistentAfterBroadcast(t *testing.T) {
	// Leader side: FSM + Service + Dissem.
	leaderFSM := NewFSM()
	leaderMem := &fakeMembership{local: "leader", ids: []string{"leader"}}
	leaderRaft := newDirectApplyRaft(leaderFSM, true)
	leaderSvc := NewService(leaderRaft, leaderFSM, &nopBus{}, nil, &nopRouter{}, leaderMem, "leader", noopLogger(), nil, nil, nil)
	leaderDissem := NewDissem("leader", nil)
	leaderSvc.SetDissem(leaderDissem)

	// Non-member side: empty FSM + Service + Dissem; raftSvc reports not-leader
	// AND no leader so isNonMember() is true (cold-miss path is gated by it).
	nonMemFSM := NewFSM()
	nonMemMem := &fakeMembership{local: "non-mem", ids: []string{"non-mem", "leader"}}
	nonMemRaft := newDirectApplyRaft(nonMemFSM, false)
	nonMemSvc := NewService(nonMemRaft, nonMemFSM, &nopBus{}, nil, &nopRouter{}, nonMemMem, "non-mem", noopLogger(), nil, nil, nil)
	nonMemDissem := NewDissem("non-mem", nil)
	nonMemSvc.SetDissem(nonMemDissem)

	owner := makePID("worker-1", "host", "alpha")
	_, err := leaderSvc.applyCommand(&Command{Type: CmdRegister, Name: "svc.alpha", PID: owner, NodeID: "worker-1"})
	require.NoError(t, err)

	// Drain leader broadcasts, deliver to non-member via NotifyMsg directly
	// (membership UDP path in production; bypassed in unit).
	frames := leaderDissem.GetBroadcasts(40, dissemMaxFrameBytes)
	require.NotEmpty(t, frames, "leader queued a broadcast")
	for _, f := range frames {
		nonMemDissem.NotifyMsg(f)
	}

	// Without dissem, this Lookup returns not-found (empty FSM). With dissem,
	// it returns the broadcast PID.
	res, err := nonMemSvc.Lookup(context.Background(), "svc.alpha")
	require.NoError(t, err)
	require.True(t, res.Found, "non-member resolves CONSISTENT name via dissem")
	assert.Equal(t, owner, res.PID)
}

// TestNonMemberResolvesActiveStrongAfterBroadcast asserts the dissem cache
// holds ACTIVE STRONG names so a non-member's Lookup returns the promoted pid
// after the promote-broadcast lands.
func TestNonMemberResolvesActiveStrongAfterBroadcast(t *testing.T) {
	leaderFSM := NewFSM()
	leaderMem := &fakeMembership{local: "leader", ids: []string{"leader"}}
	leaderRaft := newDirectApplyRaft(leaderFSM, true)
	leaderSvc := NewService(leaderRaft, leaderFSM, &nopBus{}, nil, &nopRouter{}, leaderMem, "leader", noopLogger(), nil, nil, nil)
	leaderDissem := NewDissem("leader", nil)
	leaderSvc.SetDissem(leaderDissem)

	// Open a Strong pending. On the leader the service's own evaluation
	// auto-acks and promotes it (the production flow) and queues the
	// promote-broadcast on a background goroutine; wait for that broadcast
	// instead of driving a second, racing ack by hand.
	owner := makePID("worker-1", "host", "strong")
	openPending(t, leaderFSM, "svc.strong", owner, "worker-1", []pid.NodeID{"leader"}, 10)

	// Non-member side.
	nonMemFSM := NewFSM()
	nonMemSvc := NewService(newDirectApplyRaft(nonMemFSM, false), nonMemFSM, &nopBus{}, nil, &nopRouter{}, &fakeMembership{local: "non-mem", ids: []string{"non-mem", "leader"}}, "non-mem", noopLogger(), nil, nil, nil)
	nonMemDissem := NewDissem("non-mem", nil)
	nonMemSvc.SetDissem(nonMemDissem)

	var frames [][]byte
	require.Eventually(t, func() bool {
		frames = leaderDissem.GetBroadcasts(40, dissemMaxFrameBytes)
		return len(frames) > 0
	}, 2*time.Second, 5*time.Millisecond, "leader auto-acks the strong pending and queues a broadcast")
	for _, f := range frames {
		nonMemDissem.NotifyMsg(f)
	}

	res, err := nonMemSvc.Lookup(context.Background(), "svc.strong")
	require.NoError(t, err)
	require.True(t, res.Found, "non-member resolves ACTIVE STRONG name via dissem")
	assert.Equal(t, owner, res.PID)
}

// relayPipe is a minimal in-memory relay router that ferries packages between
// attached Services by direct method call. Each attached service registers a
// (Send) endpoint keyed by its node ID; pipe.routerFor(node) returns a router
// that delivers to the target Service's Send. Mirrors the production path
// without exercising real serialization or memberlist.
type relayPipe struct {
	endpoints map[pid.NodeID]*Service
}

func newRelayPipe() *relayPipe {
	return &relayPipe{endpoints: map[pid.NodeID]*Service{}}
}

func (p *relayPipe) attach(node pid.NodeID, svc *Service) {
	p.endpoints[node] = svc
}

// routerFor returns a relay.Receiver/Sender that forwards every outgoing
// package to the attached target service's Send. The source field of the
// package is left intact so the target can address its reply.
func (p *relayPipe) routerFor(_ pid.NodeID) *pipeRouter {
	return &pipeRouter{pipe: p}
}

type pipeRouter struct {
	pipe *relayPipe
}

func (r *pipeRouter) Send(pkg *relay.Package) error {
	target := pkg.Target.Node
	svc, ok := r.pipe.endpoints[target]
	if !ok {
		relay.ReleasePackage(pkg)
		return nil
	}
	// Hand off in a goroutine so Send is non-blocking from the sender's view
	// — the production relay is asynchronous too.
	go func() { _ = svc.Send(pkg) }()
	return nil
}

// TestNonMember_ColdMissForwardResolve asserts a non-member that has not
// received a gossip broadcast yet falls back to a forward-resolve Lookup RPC
// to a member, returns the same PID, and caches it.
func TestNonMember_ColdMissForwardResolve(t *testing.T) {
	// Pipe ferries packages between the member and non-member services.
	pipe := newRelayPipe()

	// Member side: FSM holds the binding; router goes through the pipe so the
	// member's response actually reaches the non-member.
	memberFSM := NewFSM()
	memberSvc := NewService(newDirectApplyRaft(memberFSM, true), memberFSM, &nopBus{}, nil, pipe.routerFor("member"), &fakeMembership{local: "member", ids: []string{"member"}}, "member", noopLogger(), nil, nil, nil)
	memberDissem := NewDissem("member", nil)
	memberSvc.SetDissem(memberDissem)

	owner := makePID("worker", "host", "cold")
	_, err := memberSvc.applyCommand(&Command{Type: CmdRegister, Name: "svc.cold", PID: owner, NodeID: "worker"})
	require.NoError(t, err)
	pipe.attach("member", memberSvc)

	// Non-member: empty FSM and dissem; deriver picks "member" as the only
	// candidate. Router goes through the same pipe.
	nonMemFSM := NewFSM()
	nonMemRaft := newDirectApplyRaft(nonMemFSM, false)
	nonMemSvc := NewService(nonMemRaft, nonMemFSM, &nopBus{}, nil, pipe.routerFor("non-mem"), &fakeMembership{local: "non-mem", ids: []string{"non-mem", "member"}}, "non-mem", noopLogger(), nil, nil, nil)
	nonMemSvc.SetMemberDeriver(func(_ []clusterapi.NodeInfo) []clusterapi.NodeID {
		return []clusterapi.NodeID{"member"}
	})
	nonMemDissem := NewDissem("non-mem", nil)
	nonMemSvc.SetDissem(nonMemDissem)
	pipe.attach("non-mem", nonMemSvc)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	res, err := nonMemSvc.Lookup(ctx, "svc.cold")
	require.NoError(t, err)
	require.True(t, res.Found, "cold-miss forward-resolve returned the name")
	assert.Equal(t, owner, res.PID)

	// And the dissem cache should now hold it (subsequent lookups skip the
	// forward-resolve round trip).
	got, ok := nonMemDissem.Lookup("svc.cold")
	require.True(t, ok)
	assert.Equal(t, owner, got)
}
