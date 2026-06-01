// SPDX-License-Identifier: MPL-2.0

package global

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology/namereg/global"
	"go.uber.org/zap"
)

// Dissemination relay topics. Cold-miss forward-resolve + anti-entropy ride the
// existing relay so non-members reach members through the same RaftMesh
// transport the write plane uses. Wire identifiers retain a stable prefix; new
// hops do not collide with the strong/forward-write topics.
const (
	// topicLookupRequest carries a non-member's cold-miss forward-resolve Lookup
	// to a derived raft member. Body is a msgpack-encoded lookupRequestEnvelope.
	// The recipient answers from its local FSM or dissem cache (whichever has
	// the name), wrapping the result in topicLookupResponse. Never mutates Raft.
	topicLookupRequest relay.Topic = "global.lookup.req"
	// topicLookupResponse delivers the cold-miss reply. Body is a msgpack-encoded
	// lookupResponseEnvelope keyed by the request's CorrID.
	topicLookupResponse relay.Topic = "global.lookup.resp"
	// topicDigestExchange carries an anti-entropy digest probe sent from a
	// non-leader to a derived peer. Body is a msgpack-encoded digestEnvelope.
	// The receiver compares its own cache snapshot and replies with the
	// divergent entries on topicDigestDelta. Bounded; never mutates Raft.
	topicDigestExchange relay.Topic = "global.dissem.digest"
	// topicDigestDelta carries the divergent (name, pid, raftIndex) tuples the
	// peer holds at a higher index than the requester. Body is a length-prefixed
	// stream of bindingDelta wire frames (same encoding as the gossip channel).
	topicDigestDelta relay.Topic = "global.dissem.delta"
)

// lookupRequestEnvelope is the wire form of a cold-miss forward-resolve.
type lookupRequestEnvelope struct {
	Name   string     `codec:"n"`
	Origin pid.NodeID `codec:"o"`
	CorrID uint64     `codec:"c"`
	Hop    uint8      `codec:"h,omitempty"`
}

// lookupResponseEnvelope is the wire form of the reply.
type lookupResponseEnvelope struct {
	Name      string  `codec:"n"`
	PID       pid.PID `codec:"p"`
	RaftIndex uint64  `codec:"ri"`
	CorrID    uint64  `codec:"c"`
	Found     bool    `codec:"f"`
}

// digestEnvelope carries a node's cache digest (name -> raftIndex) for
// anti-entropy. Sorted name order; only live entries (no tombstones).
type digestEnvelope struct {
	Origin  pid.NodeID    `codec:"o"`
	Entries []digestEntry `codec:"en"`
	CorrID  uint64        `codec:"c"`
}

type digestEntry struct {
	Name      string `codec:"n"`
	RaftIndex uint64 `codec:"i"`
}

// SetDissem wires the dissemination plane after construction. The boot path
// builds the Service first, then the Dissem (the Dissem needs the localNode
// id which Service already carries). Safe to call before or after Start.
func (s *Service) SetDissem(d *Dissem) {
	if s == nil {
		return
	}
	s.dissem.Store(dissemHolder{d: d})
}

func (s *Service) loadDissem() *Dissem {
	if s == nil {
		return nil
	}
	v := s.dissem.Load()
	if v == nil {
		return nil
	}
	h, ok := v.(dissemHolder)
	if !ok {
		return nil
	}
	return h.d
}

// dissemHolder boxes the *Dissem so atomic.Value sees a stable concrete type.
type dissemHolder struct {
	d *Dissem
}

// handleBindingEvent is invoked by the FSM on every replica's Apply for an
// ACTIVE-binding transition. The leader translates it to a gossip broadcast;
// followers and the leader both seed their own local cache so a same-node
// Lookup uses the cache as a fast path alongside the FSM.
func (s *Service) handleBindingEvent(ev BindingEvent) {
	d := s.loadDissem()
	if d == nil {
		return
	}
	d.LocalApply(ev)
	if s.raftSvc != nil && s.raftSvc.IsLeader() {
		d.LeaderBroadcast(ev)
	}
}

// isNonMember reports whether this node does not participate in Raft (no leader
// observation channel). Members observe leadership instantly via AppendEntries;
// non-members never see Leader() return non-empty. The FSM is also empty on a
// non-member because Apply is leader-driven via raft replication. Used to gate
// the cold-miss forward-resolve.
func (s *Service) isNonMember() bool {
	if s.raftSvc == nil {
		return true
	}
	if id, _, err := s.raftSvc.Leader(); err == nil && id != "" {
		return false
	}
	return true
}

// forwardLookup queries a derived raft member for an authoritative answer to
// a cold-miss Lookup. The first responder wins; the result is cached on the
// requester via the dissem so subsequent Lookups hit the cache.
func (s *Service) forwardLookup(ctx context.Context, name string) (global.LookupResult, bool) {
	targets, err := s.resolveForwardTarget()
	if err != nil || len(targets) == 0 {
		return global.LookupResult{}, false
	}
	corrID := correlationIDCounter.Add(1)
	respCh := make(chan *lookupResponseEnvelope, 1)
	s.lookupMu.Lock()
	if s.lookupPending == nil {
		s.lookupPending = make(map[uint64]chan *lookupResponseEnvelope, 4)
	}
	s.lookupPending[corrID] = respCh
	s.lookupMu.Unlock()
	defer func() {
		s.lookupMu.Lock()
		delete(s.lookupPending, corrID)
		s.lookupMu.Unlock()
	}()

	body, err := marshalMsgpack(lookupRequestEnvelope{
		Name:   name,
		Origin: s.localNode,
		CorrID: corrID,
	})
	if err != nil {
		return global.LookupResult{}, false
	}

	// Bounded per-attempt deadline so a stalled member does not extend the
	// caller's Lookup beyond its own ctx deadline (or a 2s ceiling).
	perAttempt := 2 * time.Second
	if d, ok := ctx.Deadline(); ok {
		remaining := time.Until(d)
		if remaining < perAttempt {
			perAttempt = remaining
		}
	}
	if perAttempt < 200*time.Millisecond {
		perAttempt = 200 * time.Millisecond
	}

	for _, target := range targets {
		pkg := relay.NewServicePackage(
			s.localNode, HostID,
			target, HostID,
			topicLookupRequest,
			payload.New(body),
		)
		if err := s.router.Send(pkg); err != nil {
			relay.ReleasePackage(pkg)
			continue
		}
		select {
		case resp := <-respCh:
			if resp == nil || !resp.Found {
				continue
			}
			if d := s.loadDissem(); d != nil {
				d.SeedFromSnapshot(resp.Name, resp.PID, resp.RaftIndex)
			}
			return global.LookupResult{PID: resp.PID, Found: true}, true
		case <-time.After(perAttempt):
			continue
		case <-ctx.Done():
			return global.LookupResult{}, false
		case <-s.stopCh:
			return global.LookupResult{}, false
		}
	}
	return global.LookupResult{}, false
}

// handleLookupRequest serves a cold-miss forward-resolve. A raft member answers
// from its local FSM (authoritative) or, on FSM-miss, its dissem cache. A
// non-leader member that does NOT have the name in either source declines
// (Found=false); the requester tries the next candidate.
func (s *Service) handleLookupRequest(msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}
	body, ok := msg.Payloads[0].Data().([]byte)
	if !ok || len(body) == 0 {
		return
	}
	var env lookupRequestEnvelope
	if err := unmarshalMsgpack(body, &env); err != nil {
		s.logger.Debug("globalreg dissem: malformed lookup request", zap.Error(err))
		return
	}

	resp := lookupResponseEnvelope{Name: env.Name, CorrID: env.CorrID}
	if s.fsm != nil {
		if p, idx, found := s.fsm.State().LookupWithIndex(env.Name); found {
			resp.PID = p
			resp.RaftIndex = idx
			resp.Found = true
		}
	}
	if !resp.Found {
		if d := s.loadDissem(); d != nil {
			if p, found := d.Lookup(env.Name); found {
				resp.PID = p
				resp.RaftIndex = d.CachedIndex(env.Name)
				resp.Found = true
			}
		}
	}

	respBody, err := marshalMsgpack(resp)
	if err != nil {
		return
	}
	pkg := relay.NewServicePackage(
		s.localNode, HostID,
		env.Origin, HostID,
		topicLookupResponse,
		payload.New(respBody),
	)
	if err := s.router.Send(pkg); err != nil {
		relay.ReleasePackage(pkg)
		s.logger.Debug("globalreg dissem: send lookup response failed",
			zap.String("to", env.Origin), zap.Error(err))
	}
}

// handleLookupResponse delivers a forward-resolve reply to the waiting
// forwardLookup goroutine, if any.
func (s *Service) handleLookupResponse(msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}
	body, ok := msg.Payloads[0].Data().([]byte)
	if !ok || len(body) == 0 {
		return
	}
	var env lookupResponseEnvelope
	if err := unmarshalMsgpack(body, &env); err != nil {
		return
	}
	s.lookupMu.Lock()
	ch, ok := s.lookupPending[env.CorrID]
	s.lookupMu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- &env:
	default:
	}
}

// runAntiEntropy launches the periodic digest exchange. One peer per tick from
// the derived member set. A non-leader sends its digest; the peer responds
// with the entries it holds at strictly higher raft-index. Bounded; cheap.
func (s *Service) runAntiEntropy() {
	t := time.NewTicker(antiEntropyInterval)
	defer t.Stop()
	rotation := uint64(0)
	for {
		select {
		case <-s.stopCh:
			return
		case <-t.C:
		}
		if s.raftSvc != nil && s.raftSvc.IsLeader() {
			// Leaders broadcast on every committed transition — anti-entropy
			// would be redundant work on the leader.
			continue
		}
		d := s.loadDissem()
		if d == nil {
			continue
		}
		targets, err := s.resolveForwardTarget()
		if err != nil || len(targets) == 0 {
			continue
		}
		atomic.AddUint64(&rotation, 1)
		target := targets[int(rotation)%len(targets)]
		s.sendDigest(d, target)
	}
}

// sendDigest pushes the current cache digest to one peer.
func (s *Service) sendDigest(d *Dissem, target pid.NodeID) {
	snap := d.DigestSnapshot()
	entries := make([]digestEntry, 0, len(snap.Entries))
	for _, e := range snap.Entries {
		entries = append(entries, digestEntry(e))
	}
	corrID := correlationIDCounter.Add(1)
	body, err := marshalMsgpack(digestEnvelope{
		Origin:  s.localNode,
		Entries: entries,
		CorrID:  corrID,
	})
	if err != nil {
		return
	}
	pkg := relay.NewServicePackage(
		s.localNode, HostID,
		target, HostID,
		topicDigestExchange,
		payload.New(body),
	)
	if err := s.router.Send(pkg); err != nil {
		relay.ReleasePackage(pkg)
		s.logger.Debug("globalreg dissem: send digest failed",
			zap.String("to", target), zap.Error(err))
	}
}

// handleDigestExchange compares the peer's digest to the local cache + FSM and
// replies with the divergent entries (entries the peer holds at strictly lower
// index than us, including names the peer does not have at all).
func (s *Service) handleDigestExchange(msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}
	body, ok := msg.Payloads[0].Data().([]byte)
	if !ok || len(body) == 0 {
		return
	}
	var env digestEnvelope
	if err := unmarshalMsgpack(body, &env); err != nil {
		return
	}
	d := s.loadDissem()
	if d == nil {
		return
	}
	peer := make(map[string]uint64, len(env.Entries))
	for _, e := range env.Entries {
		peer[e.Name] = e.RaftIndex
	}

	var deltas []bindingDelta
	// Iterate the FSM (authoritative for members) first; fall back to dissem
	// for names the FSM doesn't carry on this node (non-leader members hold the
	// full FSM, but a non-member only has the dissem cache).
	if s.fsm != nil {
		for _, name := range s.fsm.State().allActiveNames() {
			p, idx, found := s.fsm.State().LookupWithIndex(name)
			if !found {
				continue
			}
			if cur, ok := peer[name]; ok && cur >= idx {
				continue
			}
			deltas = append(deltas, bindingDelta{
				Name:      name,
				PID:       p,
				RaftIndex: idx,
				Wall:      time.Now().UnixNano(),
			})
		}
	}
	// Names present in our dissem cache but not the FSM (or with higher index)
	// fold in too. The dissem snapshot already excludes tombstones.
	for _, e := range d.DigestSnapshot().Entries {
		if cur, ok := peer[e.Name]; ok && cur >= e.RaftIndex {
			continue
		}
		// Avoid double-emitting names the FSM block above already produced at
		// the same or higher index.
		dup := false
		for _, existing := range deltas {
			if existing.Name == e.Name && existing.RaftIndex >= e.RaftIndex {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		if p, found := d.Lookup(e.Name); found {
			deltas = append(deltas, bindingDelta{
				Name:      e.Name,
				PID:       p,
				RaftIndex: e.RaftIndex,
				Wall:      time.Now().UnixNano(),
			})
		}
	}

	if len(deltas) == 0 {
		return
	}
	frame := encodeBindingFrame(deltas)
	pkg := relay.NewServicePackage(
		s.localNode, HostID,
		env.Origin, HostID,
		topicDigestDelta,
		payload.New(frame),
	)
	if err := s.router.Send(pkg); err != nil {
		relay.ReleasePackage(pkg)
	}
}

// handleDigestDelta merges divergent entries the peer sent in reply to our
// digest. Same LWW-by-index rule as the gossip channel.
func (s *Service) handleDigestDelta(msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}
	body, ok := msg.Payloads[0].Data().([]byte)
	if !ok || len(body) == 0 {
		return
	}
	d := s.loadDissem()
	if d == nil {
		return
	}
	deltas, _ := decodeBindingFrame(body)
	for _, delta := range deltas {
		d.merge(delta)
	}
}

// encodeBindingFrame stitches multiple deltas into one binary frame.
func encodeBindingFrame(deltas []bindingDelta) []byte {
	out := make([]byte, 0, 64*len(deltas))
	for _, d := range deltas {
		var err error
		out, err = encodeBindingDelta(out, d)
		if err != nil {
			continue
		}
	}
	return out
}

// startJoinSnapshotSeedingForConsistent extends the join-epoch snapshot with
// active CONSISTENT bindings. The leader already returns active STRONG names;
// here we layer the CONSISTENT names so a joining non-member resolves all
// pre-existing CONSISTENT names locally immediately after the barrier.
//
// Wire shape: a separate envelope appended in the join snapshot so members
// running an older Service still parse the original payload.
//
// This helper is called from buildJoinSnapshot via a shared seam.
func (s *Service) appendConsistentEntries(resp *joinResponseEnvelope) {
	if s.fsm == nil {
		return
	}
	for _, e := range s.fsm.State().listActiveConsistent() {
		resp.Entries = append(resp.Entries, joinEntryEnvelope{
			Name:  e.Name,
			Owner: e.PID,
			Epoch: e.RaftIndex,
			State: joinSnapshotStateConsistent,
		})
	}
}

// seedDissemFromSnapshot installs every snapshot entry into the local dissem
// cache. STRONG entries are seeded with their Epoch as the lww dot; CONSISTENT
// entries carry the raft index as the dot. Called by runJoinBarrier after the
// snapshot lands and before flipping name_ready.
func (s *Service) seedDissemFromSnapshot(snap *joinResponseEnvelope) {
	d := s.loadDissem()
	if d == nil || snap == nil {
		return
	}
	for _, e := range snap.Entries {
		// Skip PENDING — the cache holds only ACTIVE bindings.
		if e.State == joinSnapshotStatePending {
			continue
		}
		d.SeedFromSnapshot(e.Name, e.Owner, e.Epoch)
	}
}
