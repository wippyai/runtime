// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"encoding/binary"
	"errors"
	"sync/atomic"
	"time"

	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	"go.uber.org/zap"
)

// KVRaftHostID is the relay host the kv engine registers so non-leader nodes can
// forward writes to the leader and receive the reply.
const KVRaftHostID pid.HostID = "storekv"

const (
	topicKVForwardReq  relay.Topic = "kv.forward.req"
	topicKVForwardResp relay.Topic = "kv.forward.resp"
)

// maxForwardRetries bounds re-resolving the leader when a forwarded write lands
// on a node that just lost leadership.
const maxForwardRetries = 3

var kvCorrIDCounter atomic.Uint64

// errForwardNotLeader marks a forwarded write that reached a non-leader, so the
// caller re-resolves the leader and retries.
var errForwardNotLeader = staticErr("kv: forwarded write reached a non-leader")

// errNoForwardLeader is returned when no leader can be resolved within retries.
var errNoForwardLeader = staticErr("kv: no raft leader for forwarded write")

type staticErr string

func (e staticErr) Error() string { return string(e) }

// error-kind codes carried on the forward wire so the caller can reconstruct the
// sentinel (preserving errors.Is) instead of a flat string.
const (
	errNone byte = iota
	errKeyNotFound
	errLeaseNotFound
	errVersionMismatch
	errNotLeaderCode
	errOther
)

func errToKind(err error) (byte, string) {
	switch {
	case err == nil:
		return errNone, ""
	case errors.Is(err, kvapi.ErrKeyNotFound):
		return errKeyNotFound, ""
	case errors.Is(err, kvapi.ErrLeaseNotFound):
		return errLeaseNotFound, ""
	case errors.Is(err, kvapi.ErrVersionMismatch):
		return errVersionMismatch, ""
	case errors.Is(err, raftapi.ErrNotLeader):
		return errNotLeaderCode, ""
	default:
		return errOther, err.Error()
	}
}

func kindToErr(kind byte, msg string) error {
	switch kind {
	case errNone:
		return nil
	case errKeyNotFound:
		return kvapi.ErrKeyNotFound
	case errLeaseNotFound:
		return kvapi.ErrLeaseNotFound
	case errVersionMismatch:
		return kvapi.ErrVersionMismatch
	case errNotLeaderCode:
		return errForwardNotLeader
	default:
		return staticErr(msg)
	}
}

// forwardToLeader sends a domain-tagged command to the raft leader and awaits
// the applied result, re-resolving the leader on a mid-flight election. `data`
// is the [KVDomain|cmd] blob exactly as a local Apply would receive.
func (e *RaftEngine) forwardToLeader(data []byte) (applyResult, error) {
	for attempt := 0; attempt < maxForwardRetries; attempt++ {
		leaderID, _, err := e.raft.Leader()
		if err != nil || leaderID == "" {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		res, transportErr := e.sendForward(leaderID, data)
		if errors.Is(transportErr, errForwardNotLeader) {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if transportErr != nil {
			return applyResult{}, transportErr
		}
		return res, nil
	}
	return applyResult{}, errNoForwardLeader
}

// sendForward performs one forward round-trip to leaderNode. A errForwardNotLeader
// transport error means the target rejected as non-leader (caller retries).
func (e *RaftEngine) sendForward(leaderNode string, data []byte) (applyResult, error) {
	corr := kvCorrIDCounter.Add(1)
	ch := make(chan applyResult, 1)
	e.fwdMu.Lock()
	e.pending[corr] = ch
	e.fwdMu.Unlock()
	defer func() {
		e.fwdMu.Lock()
		delete(e.pending, corr)
		e.fwdMu.Unlock()
	}()

	env := make([]byte, 8+len(data))
	binary.BigEndian.PutUint64(env[:8], corr)
	copy(env[8:], data)

	pkg := relay.NewServicePackage(e.localNode, KVRaftHostID, leaderNode, KVRaftHostID,
		topicKVForwardReq, payload.New(env))
	if err := e.router.Send(pkg); err != nil {
		relay.ReleasePackage(pkg)
		return applyResult{}, err
	}

	select {
	case res := <-ch:
		if errors.Is(res.Err, errForwardNotLeader) {
			return applyResult{}, errForwardNotLeader
		}
		return res, nil
	case <-time.After(raftApplyTimeout):
		return applyResult{}, errForwardNotLeader
	case <-e.ctx.Done():
		return applyResult{}, staticErr("kv: engine stopped")
	}
}

// Send implements relay.Receiver: the leader side serves forwarded writes, the
// follower side delivers replies to the waiting caller.
func (e *RaftEngine) Send(pkg *relay.Package) error {
	defer relay.ReleasePackage(pkg)
	for _, msg := range pkg.Messages {
		switch msg.Topic {
		case topicKVForwardReq:
			e.handleForwardReq(pkg.Source, msg)
		case topicKVForwardResp:
			e.handleForwardResp(msg)
		}
	}
	return nil
}

func (e *RaftEngine) handleForwardReq(source pid.PID, msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}
	env, ok := msg.Payloads[0].Data().([]byte)
	if !ok || len(env) < 8 {
		return
	}
	corr := binary.BigEndian.Uint64(env[:8])
	data := env[8:]

	var res applyResult
	resp, err := e.raft.Apply(data, raftApplyTimeout)
	switch {
	case err != nil:
		res = applyResult{Err: err}
	case resp.Response != nil:
		if r, isRes := resp.Response.(applyResult); isRes {
			res = r
		}
	}
	e.replyForward(source.Node, corr, res)
}

func (e *RaftEngine) replyForward(node pid.NodeID, corr uint64, res applyResult) {
	kind, msg := errToKind(res.Err)
	out := make([]byte, 18+len(msg))
	binary.BigEndian.PutUint64(out[:8], corr)
	binary.BigEndian.PutUint64(out[8:16], res.Version)
	if res.OK {
		out[16] = 1
	}
	out[17] = kind
	copy(out[18:], msg)

	pkg := relay.NewServicePackage(e.localNode, KVRaftHostID, node, KVRaftHostID,
		topicKVForwardResp, payload.New(out))
	if err := e.router.Send(pkg); err != nil {
		relay.ReleasePackage(pkg)
		e.logger.Debug("kv: send forward response failed", zap.Error(err))
	}
}

func (e *RaftEngine) handleForwardResp(msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}
	out, ok := msg.Payloads[0].Data().([]byte)
	if !ok || len(out) < 18 {
		return
	}
	corr := binary.BigEndian.Uint64(out[:8])
	res := applyResult{
		Version: binary.BigEndian.Uint64(out[8:16]),
		OK:      out[16] == 1,
		Err:     kindToErr(out[17], string(out[18:])),
	}
	e.fwdMu.Lock()
	ch, found := e.pending[corr]
	e.fwdMu.Unlock()
	if !found {
		return
	}
	select {
	case ch <- res:
	default:
	}
}
