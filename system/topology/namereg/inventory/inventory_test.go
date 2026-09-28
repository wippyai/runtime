// SPDX-License-Identifier: MPL-2.0

package inventory

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

type fakeEngine struct {
	entries map[string]kvapi.Entry
	version kvapi.Version
}

type interceptTxnEngine struct {
	*fakeEngine
	before  func()
	lostAck bool
}

func (e *interceptTxnEngine) Txn(ops []kvapi.TxnOp) (bool, error) {
	if e.before != nil {
		before := e.before
		e.before = nil
		before()
	}
	ok, err := e.fakeEngine.Txn(ops)
	if ok && err == nil && e.lostAck {
		return false, errors.New("response lost after commit")
	}
	return ok, err
}

// staleForwardEngine models a follower whose local snapshot has not applied a
// forwarded write yet. GetViaLeader is the read-your-write path used only to
// report the committed transition result.
type staleForwardEngine struct {
	*fakeEngine
	entry kvapi.Entry
	stale bool
}

func (e *staleForwardEngine) Get(key string) (kvapi.Entry, error) {
	if e.stale && key == Key {
		v := e.entry
		v.Value = append([]byte(nil), v.Value...)
		return v, nil
	}
	return e.fakeEngine.Get(key)
}

func (e *staleForwardEngine) GetViaLeader(key string) (kvapi.Entry, error) {
	return e.fakeEngine.Get(key)
}

type staleLocalEngine struct {
	*fakeEngine
	entry kvapi.Entry
	stale bool
}

func (e *staleLocalEngine) Get(key string) (kvapi.Entry, error) {
	if e.stale && key == Key {
		v := e.entry
		v.Value = append([]byte(nil), v.Value...)
		return v, nil
	}
	return e.fakeEngine.Get(key)
}

func newFakeEngine() *fakeEngine { return &fakeEngine{entries: make(map[string]kvapi.Entry)} }
func (e *fakeEngine) Get(key string) (kvapi.Entry, error) {
	v, ok := e.entries[key]
	if !ok {
		return kvapi.Entry{}, kvapi.ErrKeyNotFound
	}
	v.Value = append([]byte(nil), v.Value...)
	return v, nil
}
func (e *fakeEngine) Set(key string, value []byte) (kvapi.Version, error) {
	e.version++
	e.entries[key] = kvapi.Entry{Key: key, Value: append([]byte(nil), value...), Version: e.version}
	return e.version, nil
}
func (e *fakeEngine) Delete(key string) error {
	if _, ok := e.entries[key]; !ok {
		return kvapi.ErrKeyNotFound
	}
	delete(e.entries, key)
	e.version++
	return nil
}
func (e *fakeEngine) SetIfAbsent(key string, value []byte) (kvapi.Version, bool, error) {
	if v, ok := e.entries[key]; ok {
		return v.Version, false, nil
	}
	ver, err := e.Set(key, value)
	return ver, true, err
}
func (e *fakeEngine) CompareAndSwap(key string, expect kvapi.Version, value []byte) (kvapi.Version, bool, error) {
	v, _ := e.Get(key)
	if v.Version != expect {
		return v.Version, false, nil
	}
	ver, err := e.Set(key, value)
	return ver, err == nil, err
}
func (e *fakeEngine) CompareAndDelete(key string, expect kvapi.Version) (bool, error) {
	v, ok := e.entries[key]
	if !ok || v.Version != expect {
		return false, nil
	}
	delete(e.entries, key)
	e.version++
	return true, nil
}
func (e *fakeEngine) Txn(ops []kvapi.TxnOp) (bool, error) {
	for _, op := range ops {
		v, ok := e.entries[op.Key]
		switch op.Cond {
		case kvapi.CondAny:
		case kvapi.CondAbsent:
			if ok {
				return false, nil
			}
		case kvapi.CondExists:
			if !ok {
				return false, nil
			}
		case kvapi.CondVersion:
			if !ok || v.Version != op.Expect {
				return false, nil
			}
		default:
			return false, errors.New("bad condition")
		}
	}
	for _, op := range ops {
		switch op.Kind {
		case kvapi.TxnPut:
			e.version++
			e.entries[op.Key] = kvapi.Entry{Key: op.Key, Value: append([]byte(nil), op.Value...), Version: e.version}
		case kvapi.TxnDelete:
			delete(e.entries, op.Key)
			e.version++
		}
	}
	return true, nil
}
func (e *fakeEngine) Scan(prefix string, fn func(kvapi.Entry) bool) error {
	for _, v := range e.entries {
		if len(v.Key) >= len(prefix) && v.Key[:len(prefix)] == prefix && !fn(v) {
			break
		}
	}
	return nil
}
func (e *fakeEngine) Watch(context.Context, string) (kvapi.Watcher, error) {
	return nil, errors.New("not implemented")
}
func (e *fakeEngine) GrantLease(context.Context, time.Duration) (kvapi.Lease, error) {
	return nil, errors.New("not implemented")
}
func (e *fakeEngine) SetWithLease(key string, value []byte, _ kvapi.LeaseID) (kvapi.Version, error) {
	return e.Set(key, value)
}
func (e *fakeEngine) SetIfAbsentWithLease(key string, value []byte, _ kvapi.LeaseID) (kvapi.Version, bool, error) {
	return e.SetIfAbsent(key, value)
}

func participant(node string, seq byte, nonce string) Participant {
	return Participant{Node: node, Incarnation: Incarnation{Sequence: uint64(seq), BootNonce: []byte(nonce)}}
}

func TestInitializeAndHundredNodeSameEngineRestore(t *testing.T) {
	e := newFakeEngine()
	store, err := New(e, Options{MaxSlots: 100})
	require.NoError(t, err)
	snap, rev, err := store.Initialize("home-lan")
	require.NoError(t, err)
	require.Equal(t, uint64(1), snap.Generation)
	for i := 99; i >= 0; i-- {
		snap, rev, err = store.Register(rev, participant(nodeName(i), 1, "boot"))
		require.NoError(t, err)
	}
	restored, restoredRev, err := mustNew(e).ReadObserved()
	require.NoError(t, err)
	require.Equal(t, rev, restoredRev)
	require.Equal(t, snap, restored)
	require.Len(t, restored.Slots, 100)
	for i := 1; i < len(restored.Slots); i++ {
		require.Less(t, restored.Slots[i-1].Node, restored.Slots[i].Node)
	}
}

func TestCompetingRevisionAndIncarnationFence(t *testing.T) {
	e := newFakeEngine()
	store, err := New(e, Options{})
	require.NoError(t, err)
	_, rev, err := store.Initialize("cluster")
	require.NoError(t, err)
	_, staleRev := rev, rev
	_, rev, err = store.Register(rev, participant("n", 1, "a"))
	require.NoError(t, err)
	_, _, err = store.Register(rev, participant("n", 2, "b"))
	require.ErrorIs(t, err, ErrRetireRequired)
	_, _, err = store.Register(staleRev, participant("m", 1, "b"))
	require.ErrorIs(t, err, ErrRevisionConflict)
	snap, rev, err := store.Retire(rev, participant("n", 1, "a"))
	require.NoError(t, err)
	require.True(t, snap.Slots[0].Retired)
	_, _, err = store.Register(rev, participant("n", 1, "b"))
	require.ErrorIs(t, err, ErrStaleIncarnation)
	_, _, err = store.Register(rev, participant("n", 3, "b"))
	require.ErrorIs(t, err, ErrStaleIncarnation)
	snap, rev, err = store.Register(rev, participant("n", 2, "b"))
	require.NoError(t, err)
	require.Equal(t, uint64(1), snap.Slots[0].HighWater)
	_, _, err = store.Retire(rev, participant("n", 1, "a"))
	require.ErrorIs(t, err, ErrStaleIncarnation)
	_, _, err = store.Register(rev, participant("n", 2, "b"))
	require.ErrorIs(t, err, ErrAlreadyObserved)
}

func TestRetiredMaximumSequenceCannotRejoin(t *testing.T) {
	e := newFakeEngine()
	store, err := New(e, Options{})
	require.NoError(t, err)
	data, err := store.encode(Snapshot{
		Domain:     "cluster",
		Generation: 2,
		Slots: []Slot{{
			Participant: Participant{Node: "n", Incarnation: Incarnation{Sequence: ^uint64(0), BootNonce: []byte("old")}},
			Retired:     true,
			HighWater:   ^uint64(0),
		}},
	})
	require.NoError(t, err)
	_, err = e.Set(Key, data)
	require.NoError(t, err)
	_, _, err = store.Register(1, participant("n", 1, "new"))
	require.ErrorIs(t, err, ErrStaleIncarnation)
}

func TestCommittedTransitionUsesLeaderReadThrough(t *testing.T) {
	base := newFakeEngine()
	e := &staleForwardEngine{fakeEngine: base}
	store, err := New(e, Options{})
	require.NoError(t, err)
	_, rev, err := store.Initialize("cluster")
	require.NoError(t, err)
	e.entry, err = base.Get(Key)
	require.NoError(t, err)
	e.stale = true
	snap, nextRev, err := store.Register(rev, participant("n", 1, "a"))
	require.NoError(t, err)
	require.Equal(t, base.entries[Key].Version, nextRev)
	require.Len(t, snap.Slots, 1)
	local, err := e.Get(Key)
	require.NoError(t, err)
	localSnap, err := store.decode(local.Value)
	require.NoError(t, err)
	require.Equal(t, uint64(1), localSnap.Generation)
	require.Equal(t, uint64(2), snap.Generation)
}

func TestCommittedTransitionReportsUnavailableRevisionOnStaleLocalRead(t *testing.T) {
	base := newFakeEngine()
	e := &staleLocalEngine{fakeEngine: base}
	store, err := New(e, Options{})
	require.NoError(t, err)
	_, rev, err := store.Initialize("cluster")
	require.NoError(t, err)
	e.entry, err = base.Get(Key)
	require.NoError(t, err)
	e.stale = true
	snap, gotRev, err := store.Register(rev, participant("n", 1, "a"))
	require.ErrorIs(t, err, ErrRevisionUnavailable)
	require.Zero(t, gotRev)
	require.Equal(t, uint64(2), snap.Generation)
	e.stale = false
	observed, observedRev, err := store.ReadObserved()
	require.NoError(t, err)
	require.Equal(t, snap, observed)
	require.NotZero(t, observedRev)
}

func TestSameIncarnationDoesNotAdvanceGeneration(t *testing.T) {
	e := newFakeEngine()
	store, err := New(e, Options{})
	require.NoError(t, err)
	_, rev, err := store.Initialize("cluster")
	require.NoError(t, err)
	p := participant("n", 1, "a")
	_, rev, err = store.Register(rev, p)
	require.NoError(t, err)
	snap, nextRev, err := store.Register(rev, p)
	require.ErrorIs(t, err, ErrAlreadyObserved)
	require.Equal(t, rev, nextRev)
	require.Equal(t, uint64(2), snap.Generation)
}

func TestCorruptRecordFailsClosedAndReturnedDataIsImmutable(t *testing.T) {
	e := newFakeEngine()
	store, err := New(e, Options{})
	require.NoError(t, err)
	_, rev, err := store.Initialize("cluster")
	require.NoError(t, err)
	snap, _, err := store.Register(rev, participant("n", 1, "a"))
	require.NoError(t, err)
	snap.Slots[0].Incarnation.BootNonce[0] = 'x'
	snap.Slots[0].Node = "changed"
	got, _, err := store.ReadObserved()
	require.NoError(t, err)
	require.Equal(t, "n", got.Slots[0].Node)
	require.Equal(t, byte('a'), got.Slots[0].Incarnation.BootNonce[0])
	_, err = e.Set(Key, []byte("NINV\x01\x00"))
	require.NoError(t, err)
	_, _, err = store.ReadObserved()
	require.ErrorIs(t, err, ErrCorrupt)
}

func mustNew(e *fakeEngine) *Store {
	s, err := New(e, Options{})
	if err != nil {
		panic(err)
	}
	return s
}
func nodeName(i int) string { return string([]byte{'n', byte('a' + i/26), byte('a' + i%26)}) }

func TestInventoryWireFixture(t *testing.T) {
	store, err := New(newFakeEngine(), Options{})
	require.NoError(t, err)
	want := Snapshot{
		Domain: "d", Generation: 1,
		Slots: []Slot{{Participant: Participant{
			Node: "n", Incarnation: Incarnation{Sequence: 1, BootNonce: []byte("a")},
		}}},
	}
	encoded, err := store.encode(want)
	require.NoError(t, err)
	const fixture = "4e494e56010001640000000000000001000100016e0000000000000001000161000000000000000000"
	require.Equal(t, fixture, hex.EncodeToString(encoded))
	decoded, err := store.decode(encoded)
	require.NoError(t, err)
	require.Equal(t, want, decoded)
}

func TestInventorySequenceAndGenerationOverflowFailClosed(t *testing.T) {
	e := newFakeEngine()
	store, err := New(e, Options{})
	require.NoError(t, err)
	_, rev, err := store.Initialize("cluster")
	require.NoError(t, err)
	_, _, err = store.Register(rev, participant("first", 2, "nonce"))
	require.ErrorIs(t, err, ErrStaleIncarnation)
	data, err := store.encode(Snapshot{Domain: "cluster", Generation: ^uint64(0)})
	require.NoError(t, err)
	rev, err = e.Set(Key, data)
	require.NoError(t, err)
	_, _, err = store.Register(rev, participant("first", 1, "nonce"))
	require.ErrorIs(t, err, ErrCapacity)
	got, _, err := store.ReadObserved()
	require.NoError(t, err)
	require.Equal(t, ^uint64(0), got.Generation)
	require.Empty(t, got.Slots)
}

func TestCompetingCommitBetweenReadAndTransaction(t *testing.T) {
	base := newFakeEngine()
	store, err := New(base, Options{})
	require.NoError(t, err)
	_, rev, err := store.Initialize("cluster")
	require.NoError(t, err)
	competing := Snapshot{Domain: "cluster", Generation: 2, Slots: []Slot{{
		Participant: participant("winner", 1, "w"),
	}}}
	data, err := store.encode(competing)
	require.NoError(t, err)
	e := &interceptTxnEngine{fakeEngine: base, before: func() {
		_, writeErr := base.Set(Key, data)
		require.NoError(t, writeErr)
	}}
	contender, err := New(e, Options{})
	require.NoError(t, err)
	_, _, err = contender.Register(rev, participant("loser", 1, "l"))
	require.ErrorIs(t, err, ErrRevisionConflict)
	got, _, err := store.ReadObserved()
	require.NoError(t, err)
	require.Equal(t, competing, got)
}

func TestLostResponseCannotRebaseOriginalRevision(t *testing.T) {
	base := newFakeEngine()
	store, err := New(base, Options{})
	require.NoError(t, err)
	_, originalRev, err := store.Initialize("cluster")
	require.NoError(t, err)
	p := participant("node", 1, "first")
	uncertain, err := New(&interceptTxnEngine{fakeEngine: base, lostAck: true}, Options{})
	require.NoError(t, err)
	_, _, err = uncertain.Register(originalRev, p)
	require.Error(t, err)
	observed, newRev, err := store.ReadObserved()
	require.NoError(t, err)
	require.Equal(t, uint64(2), observed.Generation)
	_, _, err = store.Register(originalRev, p)
	require.ErrorIs(t, err, ErrRevisionConflict)
	_, _, err = store.Register(newRev, p)
	require.ErrorIs(t, err, ErrAlreadyObserved)
	got, _, err := store.ReadObserved()
	require.NoError(t, err)
	require.Equal(t, observed, got)
}

func TestEncodeRespectsSmallerDeploymentByteLimit(t *testing.T) {
	base := newFakeEngine()
	store, err := New(base, Options{MaxEncodedBytes: 32})
	require.NoError(t, err)
	_, rev, err := store.Initialize("d")
	require.NoError(t, err)
	_, _, err = store.Register(rev, participant("node", 1, "nonce"))
	require.ErrorIs(t, err, ErrCapacity)
	got, _, err := store.ReadObserved()
	require.NoError(t, err)
	require.Empty(t, got.Slots)
}

func TestDecodeRejectsCountLargerThanRecordBeforeAllocation(t *testing.T) {
	e := newFakeEngine()
	store, err := New(e, Options{MaxSlots: 65535})
	require.NoError(t, err)
	data, err := store.encode(Snapshot{Domain: "d", Generation: 1})
	require.NoError(t, err)
	data[len(data)-2], data[len(data)-1] = 0xff, 0xff
	_, err = e.Set(Key, data)
	require.NoError(t, err)
	_, _, err = store.ReadObserved()
	require.ErrorIs(t, err, ErrCorrupt)
}
