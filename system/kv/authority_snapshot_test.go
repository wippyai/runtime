// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

func TestReadAuthoritySnapshotLeaderIsOneDetachedRevision(t *testing.T) {
	eng, _ := newEngine(t)
	if _, err := eng.Set("a", []byte("one")); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Set("b", []byte("two")); err != nil {
		t.Fatal(err)
	}
	snap, err := eng.ReadAuthoritySnapshot(context.Background(), []string{"b", "a"})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Revision == 0 || string(snap.Entries["a"].Value) != "one" || string(snap.Entries["b"].Value) != "two" {
		t.Fatalf("snapshot = %+v", snap)
	}
	snap.Entries["a"].Value[0] = 'X'
	got, err := eng.Get("a")
	if err != nil || string(got.Value) != "one" {
		t.Fatalf("snapshot mutated engine value: %+v err=%v", got, err)
	}
}

func TestReadAuthoritySnapshotFollowerForwardsToLeader(t *testing.T) {
	engines := startForwardCluster(t, map[string]string{"A": "A", "B": "A"})
	leader, follower := engines["A"], engines["B"]
	if _, err := leader.Set("k", []byte("value")); err != nil {
		t.Fatal(err)
	}
	snap, err := follower.ReadAuthoritySnapshot(context.Background(), []string{"missing", "k"})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Revision == 0 || string(snap.Entries["k"].Value) != "value" {
		t.Fatalf("forwarded snapshot = %+v", snap)
	}
	if _, ok := snap.Entries["missing"]; ok {
		t.Fatal("missing key appeared in snapshot")
	}
}

func TestReadAuthoritySnapshotRejectsBoundsAndDuplicates(t *testing.T) {
	eng, _ := newEngine(t)
	tooMany := make([]string, maxAuthoritySnapshotKeys+1)
	if _, err := eng.ReadAuthoritySnapshot(context.Background(), tooMany); !errors.Is(err, kvapi.ErrSnapshotTooLarge) {
		t.Fatalf("too many keys = %v", err)
	}
	if _, err := eng.ReadAuthoritySnapshot(context.Background(), []string{"same", "same"}); !errors.Is(err, kvapi.ErrSnapshotInvalid) {
		t.Fatalf("duplicate keys = %v", err)
	}
}

func TestReadAuthoritySnapshotPreflightsOversizedValue(t *testing.T) {
	eng, _ := newEngine(t)
	if _, err := eng.Set("large", make([]byte, maxAuthoritySnapshotBytes+1)); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.ReadAuthoritySnapshot(context.Background(), []string{"large"}); !errors.Is(err, kvapi.ErrSnapshotTooLarge) {
		t.Fatalf("oversized value = %v", err)
	}
}

func TestReadAuthoritySnapshotHopLimitWithNoRouterReleasesPermit(t *testing.T) {
	fsm := NewRaftFSM()
	eng := NewRaftEngine(&fakeRaft{fsm: fsm, leader: false}, fsm, "node", nil, nil)
	req, err := encodeAuthorityRequest(1, maxForwardHops, nil)
	if err != nil {
		t.Fatal(err)
	}
	msg := &relay.Message{Payloads: []payload.Payload{payload.New(req)}}
	for i := 0; i < 100; i++ {
		eng.handleAuthorityReq("peer", pid.PID{Node: "claimed"}, msg)
	}
	if got := len(eng.authoritySem); got != 0 {
		t.Fatalf("hop-limit requests leaked permits: %d", got)
	}
}

func TestReadAuthoritySnapshotRejectsWrongPeerAndMalformedReply(t *testing.T) {
	fsm := NewRaftFSM()
	router := &authorityReplyRouter{}
	eng := NewRaftEngine(&fakeRaft{fsm: fsm, leaderID: "leader"}, fsm, "client", router, nil)
	router.engine = eng
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = eng.Stop() })
	_, err := eng.ReadAuthoritySnapshot(context.Background(), []string{"k"})
	if !errors.Is(err, kvapi.ErrSnapshotInvalid) {
		t.Fatalf("malformed reply = %v", err)
	}
}

func TestReadAuthoritySnapshotRetriesNotLeaderReply(t *testing.T) {
	fsm := NewRaftFSM()
	router := &notLeaderThenSnapshotRouter{}
	eng := NewRaftEngine(&fakeRaft{fsm: fsm, leaderID: "leader"}, fsm, "client", router, nil)
	router.engine = eng
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = eng.Stop() })
	snap, err := eng.ReadAuthoritySnapshot(context.Background(), []string{"k"})
	if err != nil {
		t.Fatal(err)
	}
	if string(snap.Entries["k"].Value) != "value" {
		t.Fatalf("snapshot = %+v", snap)
	}
}

func TestReadAuthoritySnapshotCancellationCleansWaiter(t *testing.T) {
	fsm := NewRaftFSM()
	router := &blockingAuthorityRouter{entered: make(chan struct{})}
	eng := NewRaftEngine(&fakeRaft{fsm: fsm, leaderID: "leader"}, fsm, "client", router, nil)
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = eng.Stop() })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := eng.ReadAuthoritySnapshot(ctx, []string{"k"}); done <- err }()
	select {
	case <-router.entered:
	case <-time.After(time.Second):
		t.Fatal("request did not reach relay")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled snapshot did not return")
	}
	eng.fwdMu.Lock()
	left := len(eng.pendingAuthority)
	eng.fwdMu.Unlock()
	if left != 0 {
		t.Fatalf("pending waiter leaked: %d", left)
	}
}

func TestReadAuthoritySnapshotCancelDoesNotBlockStopOnRaftFuture(t *testing.T) {
	fsm := NewRaftFSM()
	raft := &hangingBarrierRaft{fakeRaft: fakeRaft{fsm: fsm, leader: true}, release: make(chan struct{}), entered: make(chan struct{})}
	eng := NewRaftEngine(raft, fsm, "leader", nil, nil)
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	readDone := make(chan error, 1)
	go func() { _, err := eng.ReadAuthoritySnapshot(ctx, nil); readDone <- err }()
	select {
	case <-raft.entered:
	case <-time.After(time.Second):
		t.Fatal("read did not reach raft barrier")
	}
	cancel()
	select {
	case err := <-readDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled barrier read did not return")
	}
	stopDone := make(chan struct{})
	go func() {
		_ = eng.Stop()
		close(stopDone)
	}()
	select {
	case <-stopDone:
	case <-time.After(time.Second):
		t.Fatal("stop waited for a canceled raft future")
	}
	if got := len(eng.authoritySem); got != 1 {
		t.Fatalf("canceled barrier released permit before future completed: %d", got)
	}
	close(raft.release)
	deadline := time.Now().Add(time.Second)
	for len(eng.authoritySem) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := len(eng.authoritySem); got != 0 {
		t.Fatalf("barrier completion leaked permit: %d", got)
	}
}

func TestAuthorityBarrierExpiredDeadlineReturnsDeadlineExceeded(t *testing.T) {
	eng, _ := newEngine(t)
	permit, err := eng.acquireAuthority(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer permit.release()
	if err := eng.barrierContext(expiredDeadlineContext{}, permit); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired deadline = %v", err)
	}
}

func TestAuthorityBarrierCapsDistantDeadline(t *testing.T) {
	fsm := NewRaftFSM()
	raft := &recordingBarrierRaft{fakeRaft: fakeRaft{fsm: fsm, leader: true}, observed: make(chan time.Duration, 1)}
	eng := NewRaftEngine(raft, fsm, "leader", nil, nil)
	permit, err := eng.acquireAuthority(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer permit.release()
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	if err := eng.barrierContext(ctx, permit); err != nil {
		t.Fatal(err)
	}
	if got := <-raft.observed; got <= 0 || got > raftApplyTimeout {
		t.Fatalf("barrier enqueue timeout = %s, want at most %s", got, raftApplyTimeout)
	}
}

func TestBackgroundAuthorityReadLostReplyTimesOut(t *testing.T) {
	fsm := NewRaftFSM()
	router := &blockingAuthorityRouter{entered: make(chan struct{})}
	eng := NewRaftEngine(&fakeRaft{fsm: fsm, leaderID: "leader"}, fsm, "client", router, nil)
	eng.forwardWait = 40 * time.Millisecond
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = eng.Stop() })
	if _, err := eng.ReadAuthoritySnapshot(context.Background(), []string{"missing"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lost reply = %v, want deadline exceeded", err)
	}
	select {
	case <-router.entered:
	default:
		t.Fatal("request did not reach relay")
	}
	eng.fwdMu.Lock()
	left := len(eng.pendingAuthority)
	eng.fwdMu.Unlock()
	if left != 0 {
		t.Fatalf("timed out read leaked %d waiters", left)
	}
}

func TestAuthorityBlockedReplyDoesNotBlockIngressOrLeakPermits(t *testing.T) {
	fsm := NewRaftFSM()
	router := &blockedReplyRouter{entered: make(chan struct{}, 1)}
	eng := NewRaftEngine(&fakeRaft{fsm: fsm, leader: false}, fsm, "node", router, nil)
	eng.forwardWait = 80 * time.Millisecond
	req, err := encodeAuthorityRequest(1, maxForwardHops, nil)
	if err != nil {
		t.Fatal(err)
	}
	msg := &relay.Message{Payloads: []payload.Payload{payload.New(req)}}
	returned := make(chan struct{})
	go func() {
		for i := 0; i < 2*maxAuthorityConcurrent; i++ {
			eng.handleAuthorityReq("peer", pid.PID{}, msg)
		}
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("relay dispatch blocked behind a stalled reply")
	}
	select {
	case <-router.entered:
	case <-time.After(time.Second):
		t.Fatal("no admitted reply reached the blocked sender")
	}
	if got := len(eng.authoritySem); got > maxAuthorityConcurrent {
		t.Fatalf("reply work exceeded admission bound: %d", got)
	}
	deadline := time.After(time.Second)
	for len(eng.authoritySem) != 0 {
		select {
		case <-deadline:
			t.Fatalf("stalled replies retained %d permits after deadline", len(eng.authoritySem))
		default:
			// Yield without depending on a particular scheduler or fixed sleep.
			select {
			case <-time.After(time.Millisecond):
			case <-deadline:
				t.Fatalf("stalled replies retained %d permits after deadline", len(eng.authoritySem))
			}
		}
	}
}

func TestAuthorityResponseClassifiesOperationalFailures(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want error
		name string
	}{
		{errNoForwardLeader, kvapi.ErrSnapshotUnavailable, "no leader"},
		{errors.New("transport disconnected"), kvapi.ErrSnapshotUnavailable, "transport disconnected"},
		{kvapi.ErrKVClosed, kvapi.ErrSnapshotUnavailable, "closed"},
		{kvapi.ErrSnapshotInvalid, kvapi.ErrSnapshotInvalid, "invalid envelope"},
		{kvapi.ErrSnapshotBusy, kvapi.ErrSnapshotBusy, "busy"},
		{context.DeadlineExceeded, raftapi.ErrTimeout, "timeout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire, err := encodeAuthorityResponse(1, authorityResult{err: tc.err})
			if err != nil {
				t.Fatal(err)
			}
			got, err := decodeAuthorityResponse(wire, nil)
			if err != nil || !errors.Is(got.err, tc.want) {
				t.Fatalf("decoded error = %v, decode error = %v, want %v", got.err, err, tc.want)
			}
		})
	}
}

type expiredDeadlineContext struct{}

func (expiredDeadlineContext) Deadline() (time.Time, bool) { return time.Now().Add(-time.Second), true }
func (expiredDeadlineContext) Done() <-chan struct{}       { return nil }
func (expiredDeadlineContext) Err() error                  { return nil }
func (expiredDeadlineContext) Value(any) any               { return nil }

type hangingBarrierRaft struct { //nolint:govet // embedded test raft mirrors the production adapter
	fakeRaft
	release chan struct{}
	entered chan struct{}
}

type recordingBarrierRaft struct {
	observed chan time.Duration
	fakeRaft
}

func (r *recordingBarrierRaft) Barrier(timeout time.Duration) error {
	r.observed <- timeout
	return nil
}

type blockedReplyRouter struct{ entered chan struct{} }

func (*blockedReplyRouter) Send(*relay.Package) error {
	return errors.New("reply requires cancellable send")
}

func (r *blockedReplyRouter) SendContext(ctx context.Context, _ *relay.Package) error {
	select {
	case r.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return ctx.Err()
}

func (r *hangingBarrierRaft) Barrier(time.Duration) error {
	close(r.entered)
	<-r.release
	return nil
}

type authorityReplyRouter struct{ engine *RaftEngine }

func (r *authorityReplyRouter) Send(pkg *relay.Package) error {
	pkg.IngressNode = pkg.Source.Node
	defer relay.ReleasePackage(pkg)
	msg := pkg.Messages[0]
	request := msg.Payloads[0].Data().([]byte)
	// A wrong source must be ignored, then the authenticated peer supplies a
	// malformed frame. This exercises correlation plus strict decoding.
	corr := binary.BigEndian.Uint64(request[1:9])
	wrong := make([]byte, authorityRespHeader)
	wrong[0] = authorityWireVersion
	binary.BigEndian.PutUint64(wrong[1:9], corr)
	wrong[9] = authorityStatusOK
	wrongPkg := relay.NewServicePackage("intruder", KVRaftHostID, "client", KVRaftHostID, topicKVAuthorityResp, payload.New(wrong))
	wrongPkg.IngressNode = "intruder"
	r.engine.Send(wrongPkg)
	bad := append([]byte(nil), wrong...)
	bad[9] = 99
	badPkg := relay.NewServicePackage("leader", KVRaftHostID, "client", KVRaftHostID, topicKVAuthorityResp, payload.New(bad))
	badPkg.IngressNode = "leader"
	return r.engine.Send(badPkg)
}

func (r *authorityReplyRouter) SendContext(ctx context.Context, pkg *relay.Package) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.Send(pkg)
}

type blockingAuthorityRouter struct{ entered chan struct{} }

func (*blockingAuthorityRouter) Send(*relay.Package) error { return nil }

func (r *blockingAuthorityRouter) SendContext(_ context.Context, pkg *relay.Package) error {
	relay.ReleasePackage(pkg)
	close(r.entered)
	return nil
}

type notLeaderThenSnapshotRouter struct {
	engine *RaftEngine
	tries  int
}

func (r *notLeaderThenSnapshotRouter) Send(pkg *relay.Package) error {
	pkg.IngressNode = pkg.Source.Node
	defer relay.ReleasePackage(pkg)
	request := pkg.Messages[0].Payloads[0].Data().([]byte)
	corr := binary.BigEndian.Uint64(request[1:9])
	r.tries++
	if r.tries == 1 {
		data, err := encodeAuthorityResponse(corr, authorityResult{notLeader: true})
		if err != nil {
			return err
		}
		response := relay.NewServicePackage("leader", KVRaftHostID, "client", KVRaftHostID, topicKVAuthorityResp, payload.New(data))
		response.IngressNode = "leader"
		return r.engine.Send(response)
	}
	data, err := encodeAuthorityResponse(corr, authorityResult{snapshot: kvapi.AuthoritySnapshot{
		Entries:  map[string]kvapi.Entry{"k": {Key: "k", Value: []byte("value")}},
		Revision: 1,
	}})
	if err != nil {
		return err
	}
	response := relay.NewServicePackage("leader", KVRaftHostID, "client", KVRaftHostID, topicKVAuthorityResp, payload.New(data))
	response.IngressNode = "leader"
	return r.engine.Send(response)
}

func (r *notLeaderThenSnapshotRouter) SendContext(ctx context.Context, pkg *relay.Package) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.Send(pkg)
}

var _ relay.Receiver = (*authorityReplyRouter)(nil)
var _ relay.Receiver = (*blockingAuthorityRouter)(nil)
