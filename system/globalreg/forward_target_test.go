// SPDX-License-Identifier: MPL-2.0

package globalreg

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	clusterapi "github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/globalreg"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	raftapi "github.com/wippyai/runtime/api/raft"
	"github.com/wippyai/runtime/api/relay"
)

// --- Deterministic derivation ---

// fixedDeriver is the canonical pure-function seam the boot wiring uses,
// reused here so tests can verify two independently-instantiated services
// arrive at the same forwarding decision for the same gossip snapshot.
//
// The body is intentionally tiny: rank-by-ID (the actual derivation goes
// through DeriveMembers in production; this stub keeps the test focused on
// the contract, not the rank function's internals).
type fixedDeriver struct {
	members []clusterapi.NodeID
}

func (d *fixedDeriver) Derive(nodes []clusterapi.NodeInfo) []clusterapi.NodeID {
	// Filter to nodes present in gossip so the test exercises the
	// "deriver-meets-membership" path the real wiring uses.
	out := make([]clusterapi.NodeID, 0, len(d.members))
	live := make(map[clusterapi.NodeID]struct{}, len(nodes))
	for _, n := range nodes {
		live[n.ID] = struct{}{}
	}
	for _, id := range d.members {
		if _, ok := live[id]; ok {
			out = append(out, id)
		}
	}
	return out
}

// TestDeriveMembers_Deterministic proves two nodes given the same membership +
// caps produce the same member set. The real DeriveMembers from system/raft is
// covered there; this guards the seam contract from the globalreg side.
func TestDeriveMembers_Deterministic(t *testing.T) {
	d := &fixedDeriver{members: []clusterapi.NodeID{"node-a", "node-b", "node-c"}}
	nodes := []clusterapi.NodeInfo{
		{ID: "node-c"}, {ID: "node-a"}, {ID: "node-b"}, {ID: "node-d"},
	}
	first := d.Derive(nodes)
	second := d.Derive(nodes)
	assert.Equal(t, first, second, "same input must yield same output")
	assert.Equal(t, []clusterapi.NodeID{"node-a", "node-b", "node-c"}, first,
		"non-member node-d is filtered out; ranking is stable")
}

// --- resolveForwardTarget ---

// noLeaderRaft is a non-member shape: Apply errors NotLeader, Leader() reports
// ErrNoLeader (a non-member never observes AppendEntries → never learns the
// leader). The deriver supplies the forwarding candidates instead.
type noLeaderRaft struct {
	leaderCh chan bool
	idx      atomic.Uint64
}

func newNoLeaderRaft() *noLeaderRaft { return &noLeaderRaft{leaderCh: make(chan bool, 1)} }

func (r *noLeaderRaft) Apply(_ []byte, _ time.Duration) (*raftapi.ApplyResponse, error) {
	return nil, raftapi.ErrNotLeader
}
func (r *noLeaderRaft) Leader() (raftapi.ServerID, raftapi.ServerAddress, error) {
	return "", "", raftapi.ErrNoLeader
}
func (r *noLeaderRaft) IsLeader() bool                { return false }
func (r *noLeaderRaft) LeaderCh() <-chan bool         { return r.leaderCh }
func (r *noLeaderRaft) State() raftapi.State          { return raftapi.Follower }
func (r *noLeaderRaft) Barrier(_ time.Duration) error { return nil }
func (r *noLeaderRaft) CommitIndex() uint64           { return r.idx.Load() }
func (r *noLeaderRaft) AddVoter(_ raftapi.ServerID, _ raftapi.ServerAddress, _ time.Duration) error {
	return nil
}
func (r *noLeaderRaft) AddNonvoter(_ raftapi.ServerID, _ raftapi.ServerAddress, _ time.Duration) error {
	return nil
}
func (r *noLeaderRaft) DemoteVoter(_ raftapi.ServerID, _ time.Duration) error  { return nil }
func (r *noLeaderRaft) RemoveServer(_ raftapi.ServerID, _ time.Duration) error { return nil }
func (r *noLeaderRaft) LeadershipTransfer(_ raftapi.ServerID, _ time.Duration) error {
	return nil
}
func (r *noLeaderRaft) GetConfiguration() ([]raftapi.Server, error) { return nil, nil }
func (r *noLeaderRaft) Stats() map[string]string                    { return nil }

// TestResolveForwardTarget_LeaderKnownIsFirst proves that when Leader() returns
// a non-empty ID, it is the first candidate; the derived set comes after.
func TestResolveForwardTarget_LeaderKnownIsFirst(t *testing.T) {
	fsm := NewFSM()
	r := newDirectApplyRaft(fsm, false)
	r.knownLeader = "node-1"
	mem := &fakeMembership{local: "node-3", ids: []string{"node-1", "node-2", "node-3"}}
	svc := NewService(r, fsm, &nopBus{}, nil, &nopRouter{}, mem, "node-3", noopLogger(), nil, nil, nil)
	svc.SetMemberDeriver((&fixedDeriver{members: []clusterapi.NodeID{"node-1", "node-2"}}).Derive)

	targets, err := svc.resolveForwardTarget()
	require.NoError(t, err)
	require.NotEmpty(t, targets)
	assert.Equal(t, pid.NodeID("node-1"), targets[0], "known leader first")
	assert.Contains(t, targets, pid.NodeID("node-2"), "derived members included as fallback")
	assert.NotContains(t, targets, pid.NodeID("node-3"), "self excluded")
}

// TestResolveForwardTarget_NoLeaderFallsBackToDerivedMembers proves a non-member
// shape (no leader known) falls back to the deterministic derived member set in
// rank order.
func TestResolveForwardTarget_NoLeaderFallsBackToDerivedMembers(t *testing.T) {
	fsm := NewFSM()
	mem := &fakeMembership{local: "node-9", ids: []string{"node-1", "node-2", "node-9"}}
	svc := NewService(newNoLeaderRaft(), fsm, &nopBus{}, nil, &nopRouter{}, mem, "node-9", noopLogger(), nil, nil, nil)
	svc.SetMemberDeriver((&fixedDeriver{members: []clusterapi.NodeID{"node-1", "node-2"}}).Derive)

	targets, err := svc.resolveForwardTarget()
	require.NoError(t, err)
	assert.Equal(t, []pid.NodeID{"node-1", "node-2"}, targets,
		"non-member falls back to derived members in rank order")
}

// TestResolveForwardTarget_NoTargetsErrors proves an empty derive + unknown
// leader yields ErrNotAvailable so the caller surfaces a hard error instead
// of spinning.
func TestResolveForwardTarget_NoTargetsErrors(t *testing.T) {
	fsm := NewFSM()
	mem := &fakeMembership{local: "node-9", ids: []string{"node-9"}}
	svc := NewService(newNoLeaderRaft(), fsm, &nopBus{}, nil, &nopRouter{}, mem, "node-9", noopLogger(), nil, nil, nil)
	svc.SetMemberDeriver((&fixedDeriver{members: nil}).Derive)

	_, err := svc.resolveForwardTarget()
	assert.ErrorIs(t, err, globalreg.ErrNotAvailable)
}

// --- Non-member reaches leader via member re-forward ---

// crossClusterRouter wires three services together by NodeID. Each Send is
// re-dispatched to the destination service's Send handler so handlers exchange
// real envelopes — the shape the non-member fallback actually depends on.
type crossClusterRouter struct {
	services map[pid.NodeID]*Service
	mu       sync.Mutex
}

func (r *crossClusterRouter) wire(node pid.NodeID, svc *Service) {
	r.mu.Lock()
	if r.services == nil {
		r.services = make(map[pid.NodeID]*Service)
	}
	r.services[node] = svc
	r.mu.Unlock()
}

func (r *crossClusterRouter) Send(pkg *relay.Package) error {
	r.mu.Lock()
	dst, ok := r.services[pkg.Target.Node]
	r.mu.Unlock()
	if !ok {
		relay.ReleasePackage(pkg)
		return nil
	}
	// Re-wrap so the destination owns its own payload bytes.
	target := pkg.Target.Node
	source := pkg.Source.Node
	msgs := make([]*relay.Message, 0, len(pkg.Messages))
	for _, m := range pkg.Messages {
		var body []byte
		if len(m.Payloads) > 0 {
			if b, ok := m.Payloads[0].Data().([]byte); ok {
				body = append(body, b...)
			}
		}
		msgs = append(msgs, &relay.Message{Topic: m.Topic, Payloads: []payload.Payload{payload.New(body)}})
	}
	fresh := &relay.Package{
		Source:   pid.PID{Node: source, Host: HostID},
		Target:   pid.PID{Node: target, Host: HostID},
		Messages: msgs,
	}
	go func() { _ = dst.Send(fresh) }()
	relay.ReleasePackage(pkg)
	return nil
}

// memberFollowerRaft is a raft member that is NOT the leader: it knows the
// leader (via gossip / AppendEntries delivery) and Apply errors NotLeader, but
// Leader() reports the leader's ID. This is the second hop in the non-member
// forward chain.
type memberFollowerRaft struct {
	leaderCh chan bool
	leaderID raftapi.ServerID
	idx      atomic.Uint64
}

func newMemberFollowerRaft(leaderID raftapi.ServerID) *memberFollowerRaft {
	return &memberFollowerRaft{leaderID: leaderID, leaderCh: make(chan bool, 1)}
}

func (r *memberFollowerRaft) Apply(_ []byte, _ time.Duration) (*raftapi.ApplyResponse, error) {
	return nil, raftapi.ErrNotLeader
}
func (r *memberFollowerRaft) Leader() (raftapi.ServerID, raftapi.ServerAddress, error) {
	return r.leaderID, r.leaderID + ":0", nil
}
func (r *memberFollowerRaft) IsLeader() bool                { return false }
func (r *memberFollowerRaft) LeaderCh() <-chan bool         { return r.leaderCh }
func (r *memberFollowerRaft) State() raftapi.State          { return raftapi.Follower }
func (r *memberFollowerRaft) Barrier(_ time.Duration) error { return nil }
func (r *memberFollowerRaft) CommitIndex() uint64           { return r.idx.Load() }
func (r *memberFollowerRaft) AddVoter(_ raftapi.ServerID, _ raftapi.ServerAddress, _ time.Duration) error {
	return nil
}
func (r *memberFollowerRaft) AddNonvoter(_ raftapi.ServerID, _ raftapi.ServerAddress, _ time.Duration) error {
	return nil
}
func (r *memberFollowerRaft) DemoteVoter(_ raftapi.ServerID, _ time.Duration) error  { return nil }
func (r *memberFollowerRaft) RemoveServer(_ raftapi.ServerID, _ time.Duration) error { return nil }
func (r *memberFollowerRaft) LeadershipTransfer(_ raftapi.ServerID, _ time.Duration) error {
	return nil
}
func (r *memberFollowerRaft) GetConfiguration() ([]raftapi.Server, error) { return nil, nil }
func (r *memberFollowerRaft) Stats() map[string]string                    { return nil }
func (r *memberFollowerRaft) SetLeader(id raftapi.ServerID)               { r.leaderID = id }

// TestForwardToLeader_NonMemberReachesLeaderViaMember proves the foundational
// requirement: a node outside the bounded raft membership (no leader knowledge)
// can still reach the leader through any deterministic raft member that does
// know the leader. The member acts as the shared write plane and proxies the
// response back on the original corrID.
func TestForwardToLeader_NonMemberReachesLeaderViaMember(t *testing.T) {
	xport := &crossClusterRouter{}

	leaderFSM := NewFSM()
	leaderMem := &fakeMembership{local: "leader", ids: []string{"leader", "member", "outsider"}}
	leaderSvc := NewService(newDirectApplyRaft(leaderFSM, true), leaderFSM, &nopBus{}, nil, xport, leaderMem, "leader", noopLogger(), nil, nil, nil)

	memberFSM := NewFSM()
	memberMem := &fakeMembership{local: "member", ids: []string{"leader", "member", "outsider"}}
	memberSvc := NewService(newMemberFollowerRaft("leader"), memberFSM, &nopBus{}, nil, xport, memberMem, "member", noopLogger(), nil, nil, nil)

	outsiderFSM := NewFSM()
	outsiderMem := &fakeMembership{local: "outsider", ids: []string{"leader", "member", "outsider"}}
	outsiderSvc := NewService(newNoLeaderRaft(), outsiderFSM, &nopBus{}, nil, xport, outsiderMem, "outsider", noopLogger(), nil, nil, nil)
	// The outsider's deriver picks "member" (and "leader") as the write plane.
	outsiderSvc.SetMemberDeriver((&fixedDeriver{members: []clusterapi.NodeID{"member", "leader"}}).Derive)

	xport.wire("leader", leaderSvc)
	xport.wire("member", memberSvc)
	xport.wire("outsider", outsiderSvc)

	// Drive a CONSISTENT register from the non-member. resolveForwardTarget
	// returns ["member","leader"]; the outsider sends to "member" first.
	// "member" re-forwards to "leader"; the response is relayed back on the
	// original corrID.
	p := makePID("outsider", "host", "p1")
	resp, err := outsiderSvc.applyCommand(&Command{
		Type:   CmdRegister,
		Name:   "consistent.outsider",
		PID:    p,
		NodeID: "outsider",
	})
	require.NoError(t, err)
	result, ok := resp.(*RegisterResult)
	require.True(t, ok, "expected *RegisterResult, got %T", resp)
	require.Nil(t, result.Err)
	assert.Equal(t, p, result.PID)
}

// TestSendAck_NonMemberReachesLeader proves a Strong-scope ack from a node that
// has no direct leader knowledge still lands on the leader's FSM. The shape:
// outsider emits sendAck → member re-forwards → leader applies CmdRegisterAck.
func TestSendAck_NonMemberReachesLeader(t *testing.T) {
	xport := &crossClusterRouter{}

	leaderFSM := NewFSM()
	leaderMem := &fakeMembership{local: "leader", ids: []string{"leader", "member", "outsider"}}
	leaderSvc := NewService(newDirectApplyRaft(leaderFSM, true), leaderFSM, &nopBus{}, nil, xport, leaderMem, "leader", noopLogger(), nil, nil, nil)
	// Detach the leader's auto-ack hook so the test deterministically
	// observes the OUTSIDER's ack (the leader self-ack races and can fill
	// the AckSet before the outsider's re-forward arrives).
	leaderFSM.SetOnPending(nil)

	memberFSM := NewFSM()
	memberMem := &fakeMembership{local: "member", ids: []string{"leader", "member", "outsider"}}
	memberSvc := NewService(newMemberFollowerRaft("leader"), memberFSM, &nopBus{}, nil, xport, memberMem, "member", noopLogger(), nil, nil, nil)

	outsiderFSM := NewFSM()
	outsiderMem := &fakeMembership{local: "outsider", ids: []string{"leader", "member", "outsider"}}
	outsiderSvc := NewService(newNoLeaderRaft(), outsiderFSM, &nopBus{}, nil, xport, outsiderMem, "outsider", noopLogger(), nil, nil, nil)
	outsiderSvc.SetMemberDeriver((&fixedDeriver{members: []clusterapi.NodeID{"member"}}).Derive)

	xport.wire("leader", leaderSvc)
	xport.wire("member", memberSvc)
	xport.wire("outsider", outsiderSvc)

	// Seed a pending strong reservation that requires the outsider's ack.
	p := makePID("leader", "host", "p1")
	epoch := openPending(t, leaderFSM, "strong.outsider", p, "leader",
		[]pid.NodeID{"leader", "outsider"}, 100)

	// The outsider acks. resolveForwardTarget returns ["member"]; the ack
	// reaches "member", which re-forwards to "leader", which applies it.
	require.NoError(t, outsiderSvc.sendAck("strong.outsider", epoch))

	require.Eventually(t, func() bool {
		pv := leaderFSM.State().pendingByName("strong.outsider")
		if pv == nil {
			return false
		}
		for _, a := range pv.AckSet {
			if a == "outsider" {
				return true
			}
		}
		return false
	}, 2*time.Second, 5*time.Millisecond, "leader observes outsider's ack via member re-forward")
}

// TestPingLeader_NonMemberViaMember proves a non-member's pingLeader resolves
// against a deterministic raft member that confirms the leader (directly or
// via re-forward); the pong reaches the original prober.
func TestPingLeader_NonMemberViaMember(t *testing.T) {
	xport := &crossClusterRouter{}

	leaderFSM := NewFSM()
	leaderMem := &fakeMembership{local: "leader", ids: []string{"leader", "member", "outsider"}}
	leaderSvc := NewService(newDirectApplyRaft(leaderFSM, true), leaderFSM, &nopBus{}, nil, xport, leaderMem, "leader", noopLogger(), nil, nil, nil)

	memberFSM := NewFSM()
	memberMem := &fakeMembership{local: "member", ids: []string{"leader", "member", "outsider"}}
	memberSvc := NewService(newMemberFollowerRaft("leader"), memberFSM, &nopBus{}, nil, xport, memberMem, "member", noopLogger(), nil, nil, nil)

	outsiderFSM := NewFSM()
	outsiderMem := &fakeMembership{local: "outsider", ids: []string{"leader", "member", "outsider"}}
	outsiderSvc := NewService(newNoLeaderRaft(), outsiderFSM, &nopBus{}, nil, xport, outsiderMem, "outsider", noopLogger(), nil, nil, nil)
	outsiderSvc.SetMemberDeriver((&fixedDeriver{members: []clusterapi.NodeID{"member"}}).Derive)

	xport.wire("leader", leaderSvc)
	xport.wire("member", memberSvc)
	xport.wire("outsider", outsiderSvc)

	require.NoError(t, outsiderSvc.pingLeader(),
		"non-member reaches leader through member relay")
}

// TestPingLeader_NoReachableMemberFails proves pingLeader fails when the
// non-member has no reachable derived member. The gate-close logic depends on
// this surface.
func TestPingLeader_NoReachableMemberFails(t *testing.T) {
	xport := &crossClusterRouter{}

	outsiderFSM := NewFSM()
	outsiderMem := &fakeMembership{local: "outsider", ids: []string{"outsider"}}
	outsiderSvc := NewService(newNoLeaderRaft(), outsiderFSM, &nopBus{}, nil, xport, outsiderMem, "outsider", noopLogger(), nil, nil, nil)
	// Deriver lists a member that is NOT wired into the cross-router: every
	// send is dropped. pingLeader must fail rather than return success.
	outsiderSvc.SetMemberDeriver((&fixedDeriver{members: []clusterapi.NodeID{"unreachable"}}).Derive)

	xport.wire("outsider", outsiderSvc)

	assert.Error(t, outsiderSvc.pingLeader(),
		"no reachable derived member => probe fails")
}

// TestPingLeader_MembersWithoutLeaderFails proves pingLeader fails when the
// derived members are reachable but report no leader (election in progress).
// The gate stays closed in this window — exactly the safety property the
// reachability monitor depends on.
func TestPingLeader_MembersWithoutLeaderFails(t *testing.T) {
	xport := &crossClusterRouter{}

	memberFSM := NewFSM()
	memberMem := &fakeMembership{local: "member", ids: []string{"member", "outsider"}}
	// Member has no leader knowledge — simulating an election in progress.
	memberSvc := NewService(newNoLeaderRaft(), memberFSM, &nopBus{}, nil, xport, memberMem, "member", noopLogger(), nil, nil, nil)

	outsiderFSM := NewFSM()
	outsiderMem := &fakeMembership{local: "outsider", ids: []string{"member", "outsider"}}
	outsiderSvc := NewService(newNoLeaderRaft(), outsiderFSM, &nopBus{}, nil, xport, outsiderMem, "outsider", noopLogger(), nil, nil, nil)
	outsiderSvc.SetMemberDeriver((&fixedDeriver{members: []clusterapi.NodeID{"member"}}).Derive)

	xport.wire("member", memberSvc)
	xport.wire("outsider", outsiderSvc)

	assert.Error(t, outsiderSvc.pingLeader(),
		"members reachable but reporting no leader => probe fails (gate stays closed)")
}

// --- Hop cap: no infinite loop ---

// TestHopCap_SecondNonLeaderRecipientErrors proves a forwarded write at hop>=1
// landing on a NON-leader returns an error envelope rather than re-forwarding
// again. The cap is the no-loop guarantee: a stale leader-flip cannot spin a
// write around the cluster.
func TestHopCap_SecondNonLeaderRecipientErrors(t *testing.T) {
	xport := &crossClusterRouter{}

	// memberA is the recipient under test. It is NOT the leader (its Leader()
	// could point at memberB or even back at the requester — irrelevant: with
	// hop already at the cap, no re-forward happens regardless).
	memberAFSM := NewFSM()
	memberAMem := &fakeMembership{local: "memberA", ids: []string{"memberA", "memberB"}}
	memberASvc := NewService(newMemberFollowerRaft("memberB"), memberAFSM, &nopBus{}, nil, xport, memberAMem, "memberA", noopLogger(), nil, nil, nil)

	// memberB is the requester: it owns the pending corrID. When memberA
	// declines to re-forward (cap hit), it sends an error envelope back to
	// memberB.
	memberBFSM := NewFSM()
	memberBMem := &fakeMembership{local: "memberB", ids: []string{"memberA", "memberB"}}
	memberBSvc := NewService(newMemberFollowerRaft("memberA"), memberBFSM, &nopBus{}, nil, xport, memberBMem, "memberB", noopLogger(), nil, nil, nil)

	xport.wire("memberA", memberASvc)
	xport.wire("memberB", memberBSvc)

	corrID := correlationIDCounter.Add(1)
	respCh := make(chan *forwardResponse, 1)
	memberBSvc.mu.Lock()
	memberBSvc.pending[corrID] = respCh
	memberBSvc.mu.Unlock()
	defer func() {
		memberBSvc.mu.Lock()
		delete(memberBSvc.pending, corrID)
		memberBSvc.mu.Unlock()
	}()

	cmd := &Command{Type: CmdRegister, Name: "x", PID: makePID("memberB", "host", "p"), NodeID: "memberB"}
	data, err := EncodeCommand(cmd)
	require.NoError(t, err)
	envelope := encodeForwardRequest(corrID, 1, data) // hop=1 means already re-forwarded once
	pkg := relay.NewServicePackage("memberB", HostID, "memberA", HostID,
		topicForwardRequest, payload.New(envelope))

	require.NoError(t, memberASvc.Send(pkg))

	select {
	case resp := <-respCh:
		assert.NotEmpty(t, resp.ErrMsg, "hop>=cap non-leader returns an error envelope")
	case <-time.After(2 * time.Second):
		t.Fatal("expected an error reply from hop-capped non-leader recipient")
	}
}

// TestReForward_OneHopOnly proves a forwarded write makes exactly one
// re-forward hop: hop=0 → leader, but if the apparent leader is actually a
// non-leader member, that member re-forwards to its Leader() (hop=1) and the
// real leader applies. A subsequent forward by the real leader (which would
// be hop=2) never happens because the leader returns the response.
func TestReForward_OneHopOnly(t *testing.T) {
	xport := &crossClusterRouter{}

	leaderFSM := NewFSM()
	leaderMem := &fakeMembership{local: "leader", ids: []string{"leader", "member", "outsider"}}
	leaderSvc := NewService(newDirectApplyRaft(leaderFSM, true), leaderFSM, &nopBus{}, nil, xport, leaderMem, "leader", noopLogger(), nil, nil, nil)

	memberFSM := NewFSM()
	memberMem := &fakeMembership{local: "member", ids: []string{"leader", "member", "outsider"}}
	memberSvc := NewService(newMemberFollowerRaft("leader"), memberFSM, &nopBus{}, nil, xport, memberMem, "member", noopLogger(), nil, nil, nil)

	outsiderFSM := NewFSM()
	outsiderMem := &fakeMembership{local: "outsider", ids: []string{"leader", "member", "outsider"}}
	outsiderSvc := NewService(newNoLeaderRaft(), outsiderFSM, &nopBus{}, nil, xport, outsiderMem, "outsider", noopLogger(), nil, nil, nil)
	outsiderSvc.SetMemberDeriver((&fixedDeriver{members: []clusterapi.NodeID{"member", "leader"}}).Derive)

	xport.wire("leader", leaderSvc)
	xport.wire("member", memberSvc)
	xport.wire("outsider", outsiderSvc)

	resp, err := outsiderSvc.applyCommand(&Command{
		Type:   CmdRegister,
		Name:   "consistent.onehop",
		PID:    makePID("outsider", "host", "p"),
		NodeID: "outsider",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

// --- Election safety ---

// TestElectionSafety_MemberLeaderFlipsMidForward proves a member's
// authoritative Leader() returning the NEW leader after a flip mid-forward
// still resolves: the re-forward hits the new leader directly. The non-member's
// derived list may point at any member; the member's Leader() is the
// election-instant source of truth.
func TestElectionSafety_MemberLeaderFlipsMidForward(t *testing.T) {
	xport := &crossClusterRouter{}

	// Both members are wired. memberA is the initial leader. Mid-flow the
	// member-relay's view of leadership flips to memberB; the outsider's
	// derive points at memberA (stale), but memberA must surface NotLeader
	// rather than apply, and the retry through memberB succeeds.
	memberAFSM := NewFSM()
	memberAMem := &fakeMembership{local: "memberA", ids: []string{"memberA", "memberB", "outsider"}}
	memberARaft := newDirectApplyRaft(memberAFSM, false) // start as follower so we explicitly drive the test
	memberARaft.knownLeader = "memberB"
	memberASvc := NewService(memberARaft, memberAFSM, &nopBus{}, nil, xport, memberAMem, "memberA", noopLogger(), nil, nil, nil)

	memberBFSM := NewFSM()
	memberBMem := &fakeMembership{local: "memberB", ids: []string{"memberA", "memberB", "outsider"}}
	memberBSvc := NewService(newDirectApplyRaft(memberBFSM, true), memberBFSM, &nopBus{}, nil, xport, memberBMem, "memberB", noopLogger(), nil, nil, nil)

	outsiderFSM := NewFSM()
	outsiderMem := &fakeMembership{local: "outsider", ids: []string{"memberA", "memberB", "outsider"}}
	outsiderSvc := NewService(newNoLeaderRaft(), outsiderFSM, &nopBus{}, nil, xport, outsiderMem, "outsider", noopLogger(), nil, nil, nil)
	// Deriver lists memberA first (stale: real leader is memberB).
	outsiderSvc.SetMemberDeriver((&fixedDeriver{members: []clusterapi.NodeID{"memberA", "memberB"}}).Derive)

	xport.wire("memberA", memberASvc)
	xport.wire("memberB", memberBSvc)
	xport.wire("outsider", outsiderSvc)

	resp, err := outsiderSvc.applyCommand(&Command{
		Type:   CmdRegister,
		Name:   "consistent.elect",
		PID:    makePID("outsider", "host", "p"),
		NodeID: "outsider",
	})
	require.NoError(t, err)
	r, ok := resp.(*RegisterResult)
	require.True(t, ok)
	require.Nil(t, r.Err)
	// The committed entry must land on memberB's FSM (the real leader).
	_, found := memberBFSM.State().Lookup("consistent.elect")
	assert.True(t, found, "stale-derive resolves via re-forward to authoritative leader")
}

// --- Members unaffected: 1-hop fast path ---

// TestMember_StillUsesOneHopFastPath proves a member with a known leader still
// forwards in one hop (no derived-fan-out detour). The candidate list starts
// with the known leader; derive members come after as fallback only.
func TestMember_StillUsesOneHopFastPath(t *testing.T) {
	xport := &crossClusterRouter{}

	leaderFSM := NewFSM()
	leaderMem := &fakeMembership{local: "leader", ids: []string{"leader", "member"}}
	leaderSvc := NewService(newDirectApplyRaft(leaderFSM, true), leaderFSM, &nopBus{}, nil, xport, leaderMem, "leader", noopLogger(), nil, nil, nil)

	memberFSM := NewFSM()
	memberMem := &fakeMembership{local: "member", ids: []string{"leader", "member"}}
	memberRaft := newMemberFollowerRaft("leader")
	memberSvc := NewService(memberRaft, memberFSM, &nopBus{}, nil, xport, memberMem, "member", noopLogger(), nil, nil, nil)
	memberSvc.SetMemberDeriver((&fixedDeriver{members: []clusterapi.NodeID{"leader", "member"}}).Derive)

	xport.wire("leader", leaderSvc)
	xport.wire("member", memberSvc)

	targets, err := memberSvc.resolveForwardTarget()
	require.NoError(t, err)
	require.NotEmpty(t, targets)
	assert.Equal(t, pid.NodeID("leader"), targets[0],
		"a member's candidate list starts with the authoritative leader (1-hop fast path)")
}

// --- Idle gate ---

// TestNonMemberJoinBarrier_RunsViaMember proves the join-epoch barrier — the
// gate that lets a node serve LOCAL/EVENTUAL names — completes for a non-member
// by routing through a member. The previous implementation gated only on the
// member's response; the foundational write plane lets non-members participate.
func TestNonMemberJoinBarrier_RunsViaMember(t *testing.T) {
	xport := &crossClusterRouter{}

	leaderFSM := NewFSM()
	leaderMem := &fakeMembership{local: "leader", ids: []string{"leader", "member", "outsider"}}
	leaderSvc := NewService(newDirectApplyRaft(leaderFSM, true), leaderFSM, &nopBus{}, nil, xport, leaderMem, "leader", noopLogger(), nil, nil, nil)

	memberFSM := NewFSM()
	memberMem := &fakeMembership{local: "member", ids: []string{"leader", "member", "outsider"}}
	memberSvc := NewService(newMemberFollowerRaft("leader"), memberFSM, &nopBus{}, nil, xport, memberMem, "member", noopLogger(), nil, nil, nil)

	outsiderFSM := NewFSM()
	outsiderMem := &fakeMembership{local: "outsider", ids: []string{"leader", "member", "outsider"}}
	outsiderSvc := NewService(newNoLeaderRaft(), outsiderFSM, &nopBus{}, nil, xport, outsiderMem, "outsider", noopLogger(), nil, nil, nil)
	outsiderSvc.SetMemberDeriver((&fixedDeriver{members: []clusterapi.NodeID{"member"}}).Derive)

	xport.wire("leader", leaderSvc)
	xport.wire("member", memberSvc)
	xport.wire("outsider", outsiderSvc)

	// Run the join barrier on the outsider. It must complete without the
	// outsider ever seeing the leader directly — the snapshot arrives through
	// the member.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = ctx

	require.NoError(t, outsiderSvc.runJoinBarrier(outsiderSvc.nodeEpoch.Load()),
		"non-member completes join barrier via member relay")
	assert.True(t, outsiderSvc.NameReady(), "join barrier flips gate open")
}
