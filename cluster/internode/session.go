// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"crypto/rand"
	"encoding/binary"
	"slices"
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
	ring     resendRing // guarded by NodeState.queueMu
	// acked is the peer's cumulative ack: every seq below it was delivered.
	// Written under NodeState.queueMu; read lock-free on the ack fast path.
	acked atomic.Uint64
	// recvNext is the next sequence number expected from the peer. Written
	// only by the session's single live reader.
	recvNext atomic.Uint64
	ended    atomic.Bool
	// reading marks a live reader; endPending defers the end signal until
	// that reader stops. Guarded by NodeState.queueMu.
	reading    bool
	endPending bool
}

func newSession(peerIncarnation uint64) *session {
	s := &session{id: randomNonZero(), peerIncarnation: peerIncarnation, sendNext: 1}
	s.acked.Store(1)
	s.recvNext.Store(1)
	return s
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
)

// sessionEnd describes an ended session for reporting outside the queue lock.
type sessionEnd struct {
	reason    string
	discarded int
	changed   [numClasses]bool
	// signalNow reports that no reader was live; otherwise the reader
	// signals when it stops.
	signalNow bool
}

// endSessionLocked ends state's session and opens a fresh one bound to
// peerIncarnation. The ended session's resend ring and queued sequenced
// frames are discarded; allQueues also discards queued gossip. The caller
// holds state.queueMu and passes the result to finishSessionEnd after
// unlocking.
func endSessionLocked(state *NodeState, reason string, peerIncarnation uint64, allQueues bool) sessionEnd {
	old := state.session
	old.ended.Store(true)
	end := sessionEnd{reason: reason, signalNow: !old.reading}
	old.endPending = old.reading
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
	state.session = newSession(peerIncarnation)
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
	if end.signalNow {
		nsm.signalSessionEnd(nodeID)
	}
}

// signalSessionEnd reports an ended session once none of its frames can be
// delivered any more.
func (nsm *NodeStateManager) signalSessionEnd(nodeID cluster.NodeID) {
	if nsm.sessionEnded != nil {
		nsm.sessionEnded(nodeID)
	}
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

// endRead unregisters the reader and signals a session that ended while it
// was live: no frame of that session is delivered after this point.
func (l *sessionLink) endRead() {
	l.state.queueMu.Lock()
	l.sess.reading = false
	pending := l.sess.endPending
	l.sess.endPending = false
	l.state.queueMu.Unlock()
	if pending {
		l.nsm.signalSessionEnd(l.nodeID)
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
	// incarnationRejected: the incarnation was superseded by a restart, or
	// the state no longer belongs to the node.
	incarnationRejected
)

// bindPeerIncarnation binds a handshaken peer incarnation to state's session.
// A different incarnation than the bound one means the peer restarted: the
// old session ends and its incarnation is retired.
func (nsm *NodeStateManager) bindPeerIncarnation(nodeID cluster.NodeID, state *NodeState, incarnation uint64) incarnationBinding {
	if nsm.GetNodeState(nodeID) != state {
		return incarnationRejected
	}
	state.queueMu.Lock()
	if slices.Contains(state.retired, incarnation) {
		state.queueMu.Unlock()
		return incarnationRejected
	}
	sess := state.session
	if sess.peerIncarnation == 0 || sess.peerIncarnation == incarnation {
		sess.peerIncarnation = incarnation
		state.queueMu.Unlock()
		return incarnationBound
	}
	state.retired = append(state.retired, sess.peerIncarnation)
	end := endSessionLocked(state, sessionEndPeerRestart, incarnation, true)
	state.queueMu.Unlock()
	nsm.finishSessionEnd(nodeID, end)
	return incarnationRestarted
}

// boundPeerIncarnation reports the peer incarnation state's session carries.
func boundPeerIncarnation(state *NodeState) uint64 {
	state.queueMu.Lock()
	defer state.queueMu.Unlock()
	return state.session.peerIncarnation
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
	if state.session != sess {
		state.queueMu.Unlock()
		return 0, errSessionEnded
	}
	if sess.peerSession != 0 && theirSession != sess.peerSession {
		end := endSessionLocked(state, sessionEndPeerReset, sess.peerIncarnation, false)
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
