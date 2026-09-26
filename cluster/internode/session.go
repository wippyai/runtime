// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"crypto/rand"
	"encoding/binary"
	"sync/atomic"

	"github.com/wippyai/runtime/api/cluster"
	"go.uber.org/zap"
)

// session is the reliable per-peer stream that outlives individual sockets.
// Every sequenced frame drained for the peer receives the next sequence
// number and stays in the resend ring until the peer acknowledges it. A
// replacement connection resumes the session: both sides exchange RESUME,
// trim by the peer's receive cursor, and replay what remains.
//
// A session ends when the peer departs (node removal), restarts (new
// incarnation), or ends its own session for this node (new peer session ID
// under the same incarnation). An ended session's frames are discarded and
// never delivered; the manager signals its end.
type session struct {
	// predecessor is closed once the session created before this one for the
	// same node has been signaled ended; nothing of this session is delivered
	// before that. Nil when no earlier session is still unsettled.
	predecessor <-chan struct{}
	// settled is closed once this session's end has been signaled, which in
	// turn waits for its predecessor: ends are signaled in creation order.
	settled chan struct{}
	ring    resendRing // guarded by NodeState.queueMu
	// id identifies this node's side of the session.
	id uint64
	// peerIncarnation is the peer's process incarnation, zero until the first
	// handshake. Guarded by NodeState.queueMu.
	peerIncarnation uint64
	// peerSession is the peer's session ID, zero until the first RESUME.
	// Guarded by NodeState.queueMu.
	peerSession uint64
	// sendNext is the sequence number of the next drained frame. Guarded by
	// NodeState.queueMu.
	sendNext uint64
	// acked is the peer's cumulative ack: every seq below it was delivered.
	// Written under NodeState.queueMu; read lock-free on the ack fast path.
	acked atomic.Uint64
	// recvNext is the next sequence number expected from the peer. Written
	// only by the session's single live reader.
	recvNext atomic.Uint64
	ended    atomic.Bool
	// reading marks a live reader; settling marks that the end signal was
	// scheduled. The end is signaled once the session ended and no reader is
	// live. Guarded by NodeState.queueMu.
	reading  bool
	settling bool
}

// chainSession opens a session for nodeID after every session created for it
// before. The caller holds nsm.chainMu.
func (nsm *NodeStateManager) chainSessionLocked(nodeID cluster.NodeID, peerIncarnation uint64) *session {
	var predecessor <-chan struct{}
	if tail := nsm.tails[nodeID]; tail != nil {
		predecessor = tail.settled
	}
	s := &session{
		id:              randomNonZero(),
		peerIncarnation: peerIncarnation,
		sendNext:        1,
		predecessor:     predecessor,
		settled:         make(chan struct{}),
	}
	s.acked.Store(1)
	s.recvNext.Store(1)
	nsm.tails[nodeID] = s
	return s
}

// chainSession opens a session for nodeID after every session created for it
// before.
func (nsm *NodeStateManager) chainSession(nodeID cluster.NodeID, peerIncarnation uint64) *session {
	nsm.chainMu.Lock()
	defer nsm.chainMu.Unlock()
	return nsm.chainSessionLocked(nodeID, peerIncarnation)
}

// randomNonZero draws a uniformly random nonzero identifier.
func randomNonZero() uint64 {
	var b [8]byte
	for {
		// crypto/rand.Read never returns an error; it aborts the process when
		// the system source fails.
		_, _ = rand.Read(b[:])
		if v := binary.LittleEndian.Uint64(b[:]); v != 0 {
			return v
		}
	}
}

// ringEntry is one sequenced frame awaiting the peer's ack.
type ringEntry struct {
	data  []byte
	seq   uint64
	class Class
}

// resendRing is the FIFO of sequenced frames sent but not yet acknowledged.
// Entries before sent were handed to the current connection's writer; the
// rest wait for (re)transmission.
type resendRing struct {
	entries []ringEntry
	head    int
	bytes   int
	sent    int
}

func (r *resendRing) len() int { return len(r.entries) - r.head }

// push appends a frame handed to the current writer.
func (r *resendRing) push(e ringEntry) {
	r.entries = append(r.entries, e)
	r.bytes += len(e.data)
	r.sent++
}

// next returns the oldest entry not yet written on the current connection.
func (r *resendRing) next() (ringEntry, bool) {
	if r.sent >= r.len() {
		return ringEntry{}, false
	}
	e := r.entries[r.head+r.sent]
	r.sent++
	return e, true
}

// trim drops entries with seq below ack and reports how many it dropped.
func (r *resendRing) trim(ack uint64) int {
	n := 0
	for r.len() > 0 && r.entries[r.head].seq < ack {
		r.bytes -= len(r.entries[r.head].data)
		r.entries[r.head] = ringEntry{}
		r.head++
		if r.sent > 0 {
			r.sent--
		}
		n++
	}
	if r.len() == 0 {
		r.entries = r.entries[:0]
		r.head = 0
	} else if r.head > 1024 && r.head*2 >= len(r.entries) {
		live := copy(r.entries, r.entries[r.head:])
		clear(r.entries[live:])
		r.entries = r.entries[:live]
		r.head = 0
	}
	return n
}

// rewind schedules every unacknowledged entry for retransmission.
func (r *resendRing) rewind() { r.sent = 0 }

// reset discards every entry.
func (r *resendRing) reset() {
	clear(r.entries)
	r.entries = r.entries[:0]
	r.head, r.bytes, r.sent = 0, 0, 0
}

// admits reports whether a sequenced frame of size fits the window. A frame
// larger than the window is admitted when nothing is outstanding.
func (r *resendRing) admits(size, window int) bool {
	return r.len() == 0 || r.bytes+size <= window
}

// Session end reasons, used as telemetry labels.
const (
	sessionEndRemoved     = "removed"
	sessionEndPeerRestart = "peer_restart"
	sessionEndPeerReset   = "peer_session_reset"
	sessionEndProtocol    = "protocol_error"
)

// sessionEnd describes an ended session for reporting outside the queue lock.
type sessionEnd struct {
	ended     *session
	reason    string
	discarded int
	changed   [numClasses]bool
	// settleNow reports that no reader was live; otherwise the reader
	// settles the session when it stops.
	settleNow bool
}

// endSessionLocked ends state's session. The ended session's resend ring and
// queued sequenced frames are discarded; allQueues also discards queued
// gossip. With replace, a fresh session bound to peerIncarnation follows it;
// a removed state keeps its ended session. The caller holds state.queueMu and
// passes the result to finishSessionEnd after unlocking.
func (nsm *NodeStateManager) endSessionLocked(nodeID cluster.NodeID, state *NodeState, reason string, peerIncarnation uint64, allQueues, replace bool) sessionEnd {
	old := state.session
	if old.ended.Load() {
		return sessionEnd{ended: old, reason: reason}
	}
	old.ended.Store(true)
	end := sessionEnd{ended: old, reason: reason, settleNow: !old.reading}
	old.settling = !old.reading
	end.discarded = old.ring.len()
	old.ring.reset()
	for class, q := range state.queues {
		if !allQueues && !Class(class).sequenced() {
			continue
		}
		end.discarded += q.len()
		q.reset()
		end.changed[class] = state.lastDepth[class] != 0
		state.lastDepth[class] = 0
	}
	if replace {
		state.session = nsm.chainSession(nodeID, peerIncarnation)
	}
	return end
}

// finishSessionEnd records an ended session outside the queue lock and
// signals it unless a live reader signals when it stops.
func (nsm *NodeStateManager) finishSessionEnd(nodeID cluster.NodeID, end sessionEnd) {
	for class, c := range end.changed {
		if c {
			nsm.tel.recordQueueDepth(Class(class), nodeID, 0)
		}
	}
	nsm.tel.recordSessionEnd(end.reason)
	nsm.logger.Info("Peer session ended",
		zap.String("node", nodeID),
		zap.String("reason", end.reason),
		zap.Int("discarded_messages", end.discarded))
	if end.settleNow {
		nsm.settle(nodeID, end.ended)
	}
}

// settle signals the end of s once its predecessor has settled, then
// releases what follows it. Ends of one node are signaled in the order their
// sessions were created, and the hook returns before any frame of a later
// session is delivered. When the predecessor is still pending, a goroutine
// waits for it; manager shutdown abandons the wait.
func (nsm *NodeStateManager) settle(nodeID cluster.NodeID, s *session) {
	signal := func() {
		if nsm.sessionEnded != nil {
			nsm.sessionEnded(nodeID)
		}
		close(s.settled)
		nsm.chainMu.Lock()
		if nsm.tails[nodeID] == s {
			delete(nsm.tails, nodeID)
		}
		nsm.chainMu.Unlock()
	}
	if s.predecessor == nil {
		signal()
		return
	}
	select {
	case <-s.predecessor:
		signal()
		return
	default:
	}
	go func() {
		select {
		case <-s.predecessor:
			signal()
		case <-nsm.stop:
		}
	}()
}

// signalDeparture signals the departure of a node without state: it follows
// every earlier session of the node and precedes every later one.
func (nsm *NodeStateManager) signalDeparture(nodeID cluster.NodeID, departed *session) {
	nsm.tel.recordSessionEnd(sessionEndRemoved)
	nsm.settle(nodeID, departed)
}

// awaitPredecessor blocks until the session this one replaced has been
// signaled ended, or done closes.
func (l *sessionLink) awaitPredecessor(done <-chan struct{}) bool {
	if l.sess.predecessor == nil {
		return true
	}
	select {
	case <-l.sess.predecessor:
		return true
	case <-done:
		return false
	}
}

// failSession ends the session after a protocol violation on its stream. The
// live reader signals the end when it stops; the next connection starts a
// fresh session.
func (l *sessionLink) failSession() {
	l.state.queueMu.Lock()
	if l.state.session != l.sess || l.nsm.GetNodeState(l.nodeID) != l.state {
		l.state.queueMu.Unlock()
		return
	}
	end := l.nsm.endSessionLocked(l.nodeID, l.state, sessionEndProtocol, l.sess.peerIncarnation, false, true)
	l.state.queueMu.Unlock()
	l.nsm.finishSessionEnd(l.nodeID, end)
}

// beginRead registers the connection's reader as the session's live reader.
// It fails when the session already ended.
func (l *sessionLink) beginRead() bool {
	l.state.queueMu.Lock()
	defer l.state.queueMu.Unlock()
	if l.sess.ended.Load() {
		return false
	}
	l.sess.reading = true
	return true
}

// endRead unregisters the reader and settles a session that ended while it
// was live: no frame of that session is delivered after this point.
func (l *sessionLink) endRead() {
	l.state.queueMu.Lock()
	l.sess.reading = false
	settle := l.sess.ended.Load() && !l.sess.settling
	l.sess.settling = l.sess.settling || settle
	l.state.queueMu.Unlock()
	if settle {
		l.nsm.settle(l.nodeID, l.sess)
	}
}

// incarnationBinding is the outcome of binding a connection's peer
// incarnation to the node's session.
type incarnationBinding int

const (
	// incarnationBound: the session carries this incarnation.
	incarnationBound incarnationBinding = iota
	// incarnationRestarted: the peer restarted; the previous session ended
	// and a fresh one carries the new incarnation.
	incarnationRestarted
	// incarnationRejected: the state no longer belongs to the node.
	incarnationRejected
)

// bindPeerIncarnation binds a handshaken peer incarnation to state's session.
// Membership admitted the incarnation as the node's current one, so a
// different incarnation than the bound one means the peer restarted: the old
// session ends.
func (nsm *NodeStateManager) bindPeerIncarnation(nodeID cluster.NodeID, state *NodeState, incarnation uint64) incarnationBinding {
	state.queueMu.Lock()
	// Removal detaches the state under this lock.
	if nsm.GetNodeState(nodeID) != state {
		state.queueMu.Unlock()
		return incarnationRejected
	}
	sess := state.session
	if sess.peerIncarnation == 0 || sess.peerIncarnation == incarnation {
		sess.peerIncarnation = incarnation
		state.queueMu.Unlock()
		return incarnationBound
	}
	end := nsm.endSessionLocked(nodeID, state, sessionEndPeerRestart, incarnation, true, true)
	state.queueMu.Unlock()
	nsm.finishSessionEnd(nodeID, end)
	return incarnationRestarted
}

// resumeOutcome is the result of processing the peer's RESUME.
type resumeOutcome int

const (
	// resumeReady: both sides agree on the session; unacknowledged frames
	// are scheduled for replay.
	resumeReady resumeOutcome = iota
	// resumeAwaitPeerReset: this session is fresh and the peer still holds
	// an ended one; the peer ends it when it reads this side's RESUME.
	resumeAwaitPeerReset
	// resumePeerReset: the peer ended its session; this side's session ended
	// too and its frames were discarded.
	resumePeerReset
)

// resume reconciles sess with the peer's RESUME. theirSession is the peer's
// session ID, theirView the peer's view of this side's session ID (zero when
// unknown), and theirRecv the peer's receive cursor.
func (nsm *NodeStateManager) resume(nodeID cluster.NodeID, state *NodeState, sess *session, theirSession, theirView, theirRecv uint64) (resumeOutcome, error) {
	state.queueMu.Lock()
	if state.session != sess || nsm.GetNodeState(nodeID) != state {
		state.queueMu.Unlock()
		return 0, errSessionEnded
	}
	if sess.peerSession != 0 && theirSession != sess.peerSession {
		end := nsm.endSessionLocked(nodeID, state, sessionEndPeerReset, sess.peerIncarnation, false, true)
		state.queueMu.Unlock()
		nsm.finishSessionEnd(nodeID, end)
		return resumePeerReset, nil
	}
	if theirView != 0 && theirView != sess.id {
		state.queueMu.Unlock()
		if sess.peerSession == 0 {
			return resumeAwaitPeerReset, nil
		}
		return 0, newResumeViewError(theirView, sess.id)
	}
	acked := sess.acked.Load()
	if theirRecv < acked || theirRecv > sess.sendNext {
		sendNext := sess.sendNext
		state.queueMu.Unlock()
		return 0, newAckRangeError(theirRecv, acked, sendNext)
	}
	sess.peerSession = theirSession
	sess.ring.trim(theirRecv)
	sess.acked.Store(theirRecv)
	sess.ring.rewind()
	state.queueMu.Unlock()
	return resumeReady, nil
}

// applyAck trims sess's resend ring by the peer's cumulative ack and reports
// whether it freed window space.
func applyAck(state *NodeState, sess *session, ack uint64) (bool, error) {
	if ack == sess.acked.Load() {
		return false, nil
	}
	state.queueMu.Lock()
	acked := sess.acked.Load()
	if ack < acked || ack > sess.sendNext {
		sendNext := sess.sendNext
		state.queueMu.Unlock()
		return false, newAckRangeError(ack, acked, sendNext)
	}
	trimmed := sess.ring.trim(ack)
	sess.acked.Store(ack)
	state.queueMu.Unlock()
	return trimmed > 0, nil
}

// resumeState returns what this side announces in its RESUME.
func resumeState(state *NodeState, sess *session) (id, peerView, recvNext uint64) {
	state.queueMu.Lock()
	defer state.queueMu.Unlock()
	return sess.id, sess.peerSession, sess.recvNext.Load()
}
