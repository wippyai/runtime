// SPDX-License-Identifier: MPL-2.0

package global

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology/namereg/global"
	"go.uber.org/zap"
)

// Leader-directed write forwarding plane: non-leaders forward mutating
// commands to the raft leader and relay the response back on the
// originating correlation id. Split out of service.go; same package.

// forwardResponse wraps the result of a forwarded command. Result carries the
// typed FSM response on success; ErrMsg carries the failure otherwise. The two
// are mutually exclusive.
type forwardResponse struct {
	Result any
	ErrMsg string
}

// correlationIDCounter generates unique correlation IDs for forwarded requests.
var correlationIDCounter atomic.Uint64

// stampLeaderPending stamps RequiredNodes onto a fresh Strong open from the
// leader's membership. Only the leader stamps, and only a fresh open (Epoch
// unassigned, no RequiredNodes yet). An already-committed pending is never
// re-stamped: a new leader inherits the committed RequiredNodes because fresh
// opens are the only CmdRegisterPending the protocol applies. This keeps
// FSM.Apply deterministic — the set is fixed once, then replicated verbatim.
func (s *Service) stampLeaderPending(cmd *Command) {
	if cmd == nil || cmd.Type != CmdRegisterPending {
		return
	}
	if cmd.Epoch != 0 || len(cmd.RequiredNodes) > 0 {
		return
	}
	if s.raftSvc == nil || !s.raftSvc.IsLeader() {
		return
	}
	cmd.RequiredNodes = s.snapshotRequiredNodes()
}

// applyCommand encodes and proposes a command to Raft.
// If this node is not the leader, the command is forwarded via relay.
func (s *Service) applyCommand(cmd *Command) (any, error) {
	s.stampLeaderPending(cmd)

	data, err := EncodeCommand(cmd)
	if err != nil {
		return nil, fmt.Errorf("encode command: %w", err)
	}

	resp, err := s.raftSvc.Apply(data, defaultApplyTimeout)
	if err == nil {
		// FSM.Apply may return an error wrapped in a result struct.
		if resp.Response != nil {
			if fsmErr, ok := resp.Response.(error); ok {
				return nil, fsmErr
			}
		}
		return resp.Response, nil
	}

	// Not the leader — forward to leader via relay.
	if errors.Is(err, raftapi.ErrNotLeader) {
		return s.forwardToLeader(cmd)
	}

	return nil, err
}

// forwardToLeader sends a leader-directed command over the relay, trying each
// resolveForwardTarget candidate in order until one responds. The first
// candidate is the authoritative leader (raftSvc.Leader()) when known; the
// remainder are deterministic raft members derived from the gossip view, used
// as the shared write plane for non-members that never observe a leader
// directly. Uses a correlation ID to pair the response with this caller and
// retries the next candidate on send failure or per-attempt timeout.
//
// The forward envelope carries an 8B corrID, a 1B hop count, and the encoded
// command. A member receiving a hop=0 envelope while NOT the leader
// re-forwards it once to its authoritative Leader() (hop becomes 1) and relays
// the response on the same corrID back to the original requester. A hop>=1
// recipient that is not the leader replies an error so a stale-leader window
// cannot infinite-loop.
func (s *Service) forwardToLeader(cmd *Command) (any, error) {
	start := time.Now()

	targets, err := s.waitForForwardTargets()
	if err != nil {
		s.tel.recordForwardedApply(cmd.Type, forwardResultNoLeader, time.Since(start))
		return nil, err
	}

	data, err := EncodeCommand(cmd)
	if err != nil {
		s.tel.recordForwardedApply(cmd.Type, forwardResultError, time.Since(start))
		return nil, fmt.Errorf("encode forward command: %w", err)
	}

	corrID := correlationIDCounter.Add(1)
	respCh := make(chan *forwardResponse, 1)

	s.mu.Lock()
	s.pending[corrID] = respCh
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.pending, corrID)
		s.mu.Unlock()
	}()

	// Per-attempt timeout: defaultApplyTimeout split across at most a small
	// set of candidates so the caller's effective wall-clock stays bounded.
	// One re-forward hop adds at most a single internal round-trip per attempt.
	attempts := len(targets)
	if attempts > 3 {
		attempts = 3
	}
	perAttempt := defaultApplyTimeout / time.Duration(attempts)
	if perAttempt < time.Second {
		perAttempt = time.Second
	}

	envelope := encodeForwardRequest(corrID, 0, data)

	var sendErr error
	for i, target := range targets {
		if i >= attempts {
			break
		}
		pkg := relay.NewServicePackage(
			s.localNode, HostID,
			target, HostID,
			topicForwardRequest,
			payload.New(envelope),
		)
		if err := s.router.Send(pkg); err != nil {
			relay.ReleasePackage(pkg)
			sendErr = err
			continue
		}

		select {
		case resp := <-respCh:
			if resp.ErrMsg != "" {
				s.tel.recordForwardedApply(cmd.Type, forwardResultError, time.Since(start))
				return nil, errors.New(resp.ErrMsg)
			}
			s.tel.recordForwardedApply(cmd.Type, forwardResultOK, time.Since(start))
			return resp.Result, nil
		case <-time.After(perAttempt):
			sendErr = global.ErrNotAvailable
			continue
		case <-s.stopCh:
			s.tel.recordForwardedApply(cmd.Type, forwardResultTimeout, time.Since(start))
			return nil, global.ErrNotAvailable
		}
	}

	if sendErr != nil {
		s.tel.recordForwardedApply(cmd.Type, forwardResultSendFailed, time.Since(start))
		return nil, fmt.Errorf("forward to leader: %w", sendErr)
	}
	s.tel.recordForwardedApply(cmd.Type, forwardResultTimeout, time.Since(start))
	return nil, global.ErrNotAvailable
}

// encodeForwardRequest packs the leader-directed forward envelope:
//
//	[8B corrID][1B hop][command bytes]
//
// The hop byte gates the no-loop guarantee in handleForwardRequest: a member
// receiving the envelope while not the leader bumps hop and re-forwards once
// to its authoritative Leader(); a hop>=maxForwardHops recipient that is
// still not the leader returns an error instead of looping.
func encodeForwardRequest(corrID uint64, hop uint8, data []byte) []byte {
	out := make([]byte, 8+1+len(data))
	binary.BigEndian.PutUint64(out[:8], corrID)
	out[8] = hop
	copy(out[9:], data)
	return out
}

// decodeForwardRequest unpacks a forward envelope and returns the corrID,
// hop count, and command bytes. Returns false when the envelope is too
// short to be valid.
func decodeForwardRequest(envelope []byte) (corrID uint64, hop uint8, data []byte, ok bool) {
	if len(envelope) < 9 {
		return 0, 0, nil, false
	}
	return binary.BigEndian.Uint64(envelope[:8]), envelope[8], envelope[9:], true
}

// handleForwardRequest processes a forwarded command. The recipient is either
// the leader (apply directly and reply) or a non-leader raft member acting as
// the shared write plane for a non-member requester (re-forward once to the
// authoritative leader, relay the response back on the original corrID).
//
// The hop byte in the envelope caps the re-forward chain at maxForwardHops:
// a hop>=cap recipient that is still not the leader returns an error rather
// than re-forwarding, eliminating any stale-leader-induced loop.
func (s *Service) handleForwardRequest(source pid.PID, msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}

	envelope, ok := msg.Payloads[0].Data().([]byte)
	if !ok {
		s.logger.Error("invalid forward request payload")
		return
	}

	corrID, hop, data, valid := decodeForwardRequest(envelope)
	if !valid {
		s.logger.Error("invalid forward request payload")
		return
	}

	// Re-forward path: a non-leader member receiving a hop<cap envelope
	// forwards once to its authoritative Leader() and proxies the response
	// back to the original requester on the same corrID. Members observe
	// leadership instantly via AppendEntries, so this is election-safe even
	// when the original requester's derived list pointed here from a stale
	// leader hint.
	if s.raftSvc == nil || !s.raftSvc.IsLeader() {
		next, ok := s.reForwardTarget(hop)
		if !ok {
			// Cap hit or no leader known on this member: surface a typed
			// error instead of dropping. The original requester sees the
			// failure on its corrID and falls back to the next candidate.
			s.replyForward(source.Node, corrID, 0, raftapi.ErrNotLeader.Error(), nil)
			return
		}
		s.proxyForwardRequest(source.Node, corrID, hop+1, data, next)
		return
	}

	// Decode the command so we can tag the typed response with its kind. The
	// leader-side apply path itself does not need the decoded command — Raft
	// decodes it again inside FSM.Apply — but the response envelope must
	// carry the command kind so the follower knows how to decode the typed
	// result blob.
	cmd, decodeErr := DecodeCommand(data)
	if decodeErr != nil {
		s.logger.Error("invalid forward request command", zap.Error(decodeErr))
		s.replyForward(source.Node, corrID, 0, decodeErr.Error(), nil)
		return
	}

	// Stamp RequiredNodes from the leader's membership for a forwarded fresh
	// Strong open. The follower deliberately omits the set so the leader's
	// view is authoritative; re-encode so the stamped set lands in the log.
	if cmd.Type == CmdRegisterPending && cmd.Epoch == 0 && len(cmd.RequiredNodes) == 0 {
		s.stampLeaderPending(cmd)
		if restamped, encErr := EncodeCommand(cmd); encErr == nil {
			data = restamped
		} else {
			s.logger.Error("re-encode stamped forward command", zap.Error(encErr))
		}
	}

	var (
		errMsg     string
		resultBlob []byte
	)
	resp, err := s.raftSvc.Apply(data, defaultApplyTimeout)
	switch {
	case err != nil:
		errMsg = err.Error()
	case resp == nil || resp.Response == nil:
		// No-op: nothing to encode.
	default:
		if fsmErr, ok := resp.Response.(error); ok {
			errMsg = fsmErr.Error()
			break
		}
		encoded, encErr := encodeFSMResult(cmd.Type, resp.Response)
		if encErr != nil {
			s.logger.Error("encode forward response result",
				zap.Error(encErr), zap.String("cmd", commandLabel(cmd.Type)))
			errMsg = encErr.Error()
			break
		}
		resultBlob = encoded
	}

	s.replyForward(source.Node, corrID, cmd.Type, errMsg, resultBlob)
}

// proxyForwardRequest re-sends a forwarded command to the authoritative leader
// on behalf of the original requester. The leader's response envelope already
// carries the original corrID, so when handleForwardResponse fires on this
// member it relays the bytes verbatim back to the requester.
func (s *Service) proxyForwardRequest(originNode pid.NodeID, corrID uint64, hop uint8, data []byte, next pid.NodeID) {
	// Reserve a proxy slot on the original corrID so the leader's reply
	// (which arrives on this node, not the original requester) is intercepted
	// and relayed onward instead of being delivered to a local waiter.
	s.installForwardProxy(corrID, originNode)

	envelope := encodeForwardRequest(corrID, hop, data)
	pkg := relay.NewServicePackage(
		s.localNode, HostID,
		next, HostID,
		topicForwardRequest,
		payload.New(envelope),
	)
	if err := s.router.Send(pkg); err != nil {
		relay.ReleasePackage(pkg)
		s.removeForwardProxy(corrID)
		s.replyForward(originNode, corrID, 0, err.Error(), nil)
	}
}

// replyForward sends the v1 typed envelope back to the requesting follower.
func (s *Service) replyForward(sourceNode pid.NodeID, corrID uint64, cmd CommandType, errMsg string, result []byte) {
	respEnvelope, err := encodeForwardResponse(corrID, cmd, errMsg, result)
	if err != nil {
		s.logger.Error("encode forward response envelope",
			zap.Error(err), zap.String("to", sourceNode))
		return
	}

	respPkg := relay.NewServicePackage(
		s.localNode, HostID,
		sourceNode, HostID,
		topicForwardResponse,
		payload.New(respEnvelope),
	)

	if err := s.router.Send(respPkg); err != nil {
		relay.ReleasePackage(respPkg)
		s.logger.Error("failed to send forward response",
			zap.Error(err), zap.String("to", sourceNode))
	}
}

// handleForwardResponse processes a response from the leader for a forwarded
// command. When this node is an intermediate hop (its corrID is registered in
// forwardProxies because it re-forwarded the original request on behalf of a
// non-member), the response bytes are relayed verbatim to the origin node
// instead of being delivered locally. Otherwise it decodes the typed v1
// envelope and delivers the result to the waiting caller.
func (s *Service) handleForwardResponse(msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}

	envelope, ok := msg.Payloads[0].Data().([]byte)
	if !ok || len(envelope) < 8 {
		s.logger.Error("invalid forward response payload")
		return
	}

	corrID := binary.BigEndian.Uint64(envelope[:8])
	if origin, isProxy := s.takeForwardProxy(corrID); isProxy {
		s.relayForwardResponse(origin, envelope)
		return
	}

	decoded, err := decodeForwardResponse(envelope)
	if err != nil {
		s.logger.Error("failed to decode forward response envelope",
			zap.Error(err), zap.Uint64("corr_id", decoded.CorrID))
		s.deliverForward(decoded.CorrID, &forwardResponse{
			ErrMsg: fmt.Sprintf("decode forward response: %v", err),
		})
		return
	}

	resp := &forwardResponse{
		ErrMsg: decoded.ErrMsg,
		Result: decoded.Result,
	}
	s.deliverForward(decoded.CorrID, resp)
}

// installForwardProxy records that this node is the intermediate hop for the
// given corrID; the original request came from origin. handleForwardResponse
// uses this map to redirect the leader's reply.
func (s *Service) installForwardProxy(corrID uint64, origin pid.NodeID) {
	s.mu.Lock()
	s.forwardProxies[corrID] = origin
	s.mu.Unlock()
}

// removeForwardProxy clears a proxy entry without consuming a reply. Used on
// send failure when no leader reply will arrive.
func (s *Service) removeForwardProxy(corrID uint64) {
	s.mu.Lock()
	delete(s.forwardProxies, corrID)
	s.mu.Unlock()
}

// takeForwardProxy atomically reads and removes a proxy entry. Returns the
// origin and true if a proxy was registered for this corrID, else ("", false).
func (s *Service) takeForwardProxy(corrID uint64) (pid.NodeID, bool) {
	s.mu.Lock()
	origin, ok := s.forwardProxies[corrID]
	if ok {
		delete(s.forwardProxies, corrID)
	}
	s.mu.Unlock()
	return origin, ok
}

// relayForwardResponse forwards a leader-emitted response envelope verbatim to
// the original requester. The envelope already carries the right corrID, so
// the origin's pending waiter resolves without this node needing to decode.
func (s *Service) relayForwardResponse(origin pid.NodeID, envelope []byte) {
	pkg := relay.NewServicePackage(
		s.localNode, HostID,
		origin, HostID,
		topicForwardResponse,
		payload.New(envelope),
	)
	if err := s.router.Send(pkg); err != nil {
		relay.ReleasePackage(pkg)
		s.logger.Debug("globalreg: relay forward response failed",
			zap.String("to", origin), zap.Error(err))
	}
}

// deliverForward delivers a parsed response to the waiting forwardToLeader
// goroutine, if any. Lost responses (no pending entry) are logged at debug
// level — the only callers are the timeout / stop branches in forwardToLeader.
func (s *Service) deliverForward(corrID uint64, resp *forwardResponse) {
	s.mu.Lock()
	ch, ok := s.pending[corrID]
	s.mu.Unlock()
	if !ok {
		s.logger.Debug("received forward response for unknown correlation ID",
			zap.Uint64("corr_id", corrID))
		return
	}
	select {
	case ch <- resp:
	default:
	}
}
