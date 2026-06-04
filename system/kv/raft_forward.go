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
	topicKVReadReq     relay.Topic = "kv.read.req"
	topicKVReadResp    relay.Topic = "kv.read.resp"
)

// readResult is a forwarded leader-read reply. err carries errForwardNotLeader
// when the target was not the leader, so the caller re-resolves and retries.
type readResult struct {
	err     error
	value   []byte
	version kvapi.Version
	epoch   uint64
	found   bool
}

// maxForwardRetries bounds re-resolving the leader when a forwarded write lands
// on a node that just lost leadership.
const maxForwardRetries = 3

// maxForwardHops bounds re-forwarding: a non-leader member that receives a
// forwarded op relays it to the leader it can resolve, so a registry non-member
// (role=client, no raft state to call raft.Leader()) can forward to any member
// and still reach the leader. Each relay increments the hop; past this bound the
// op is rejected as not-leader rather than chained further.
const maxForwardHops byte = 2

var kvCorrIDCounter atomic.Uint64

// forwardReplyGrace is added to raftApplyTimeout to size the follower's wait for
// a forward reply. The leader applies with raftApplyTimeout, so waiting strictly
// longer lets a slow-but-committed apply's reply arrive instead of spuriously
// timing out — keeping a successful forwarded write from surfacing as a timeout.
const forwardReplyGrace = 2 * time.Second

// forwardWaitTimeout is how long a forwarder waits for a reply before giving up.
const forwardWaitTimeout = raftApplyTimeout + forwardReplyGrace

// errForwardNotLeader marks a forwarded write that reached a non-leader. The op
// was provably NOT applied (a leader that loses leadership mid-Apply does not
// commit), so the caller may safely re-resolve the leader and retry.
var errForwardNotLeader = staticErr("kv: forwarded write reached a non-leader")

// errForwardTimeout marks a forwarded write whose reply never arrived. Unlike a
// not-leader rejection the outcome is ambiguous — the leader may have committed
// it — so it MUST NOT be retried, or a committed non-idempotent op (Set/CAS/
// Delete) would apply twice. The caller surfaces it as an honest write error.
var errForwardTimeout = staticErr("kv: forwarded write timed out")

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
	return e.forwardToLeaderHop(data, 0)
}

func (e *RaftEngine) forwardToLeaderHop(data []byte, hop byte) (applyResult, error) {
	for attempt := 0; attempt < maxForwardRetries; attempt++ {
		leaderID, _, err := e.raft.Leader()
		if err != nil || leaderID == "" {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		res, transportErr := e.sendForward(leaderID, data, hop)
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
func (e *RaftEngine) sendForward(leaderNode string, data []byte, hop byte) (applyResult, error) {
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

	env := make([]byte, 9+len(data))
	binary.BigEndian.PutUint64(env[:8], corr)
	env[8] = hop
	copy(env[9:], data)

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
	case <-time.After(e.forwardWait):
		// Final non-blocking check: a response that arrived simultaneously with
		// the timeout must not be dropped (else an applied write is mistaken for a
		// timeout). A genuine timeout returns errForwardTimeout, which the caller
		// does NOT retry — the write may have committed, so retrying could
		// double-apply.
		select {
		case res := <-ch:
			if errors.Is(res.Err, errForwardNotLeader) {
				return applyResult{}, errForwardNotLeader
			}
			return res, nil
		default:
			return applyResult{}, errForwardTimeout
		}
	case <-e.ctx.Done():
		return applyResult{}, staticErr("kv: engine stopped")
	}
}

// GetViaLeader reads a key from the raft leader's applied state, giving
// read-your-writes after a forwarded write even on a follower. On the leader
// (or with no router) it reads locally.
func (e *RaftEngine) GetViaLeader(key string) (kvapi.Entry, error) {
	if e.raft.IsLeader() || e.router == nil {
		return e.Get(key)
	}
	return e.forwardRead(key)
}

func (e *RaftEngine) forwardRead(key string) (kvapi.Entry, error) {
	res, err := e.forwardReadHop(key, 0)
	if err != nil {
		return kvapi.Entry{}, err
	}
	if !res.found {
		return kvapi.Entry{}, kvapi.ErrKeyNotFound
	}
	return kvapi.Entry{Key: key, Value: res.value, Version: res.version, Epoch: res.epoch}, nil
}

func (e *RaftEngine) forwardReadHop(key string, hop byte) (readResult, error) {
	for attempt := 0; attempt < maxForwardRetries; attempt++ {
		leaderID, _, err := e.raft.Leader()
		if err != nil || leaderID == "" {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		res, transportErr := e.sendRead(leaderID, key, hop)
		if transportErr != nil || errors.Is(res.err, errForwardNotLeader) {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		return res, nil
	}
	return readResult{}, errNoForwardLeader
}

func (e *RaftEngine) sendRead(leaderNode, key string, hop byte) (readResult, error) {
	corr := kvCorrIDCounter.Add(1)
	ch := make(chan readResult, 1)
	e.fwdMu.Lock()
	e.pendingReads[corr] = ch
	e.fwdMu.Unlock()
	defer func() {
		e.fwdMu.Lock()
		delete(e.pendingReads, corr)
		e.fwdMu.Unlock()
	}()

	env := make([]byte, 9+len(key))
	binary.BigEndian.PutUint64(env[:8], corr)
	env[8] = hop
	copy(env[9:], key)

	pkg := relay.NewServicePackage(e.localNode, KVRaftHostID, leaderNode, KVRaftHostID,
		topicKVReadReq, payload.New(env))
	if err := e.router.Send(pkg); err != nil {
		relay.ReleasePackage(pkg)
		return readResult{}, err
	}
	select {
	case res := <-ch:
		return res, nil
	case <-time.After(e.forwardWait):
		select {
		case res := <-ch:
			return res, nil
		default:
			return readResult{err: errForwardNotLeader}, nil
		}
	case <-e.ctx.Done():
		return readResult{}, staticErr("kv: engine stopped")
	}
}

// Send implements relay.Receiver: the leader side serves forwarded writes and
// reads, the follower side delivers replies to the waiting caller.
func (e *RaftEngine) Send(pkg *relay.Package) error {
	defer relay.ReleasePackage(pkg)
	for _, msg := range pkg.Messages {
		switch msg.Topic {
		case topicKVForwardReq:
			e.handleForwardReq(pkg.Source, msg)
		case topicKVForwardResp:
			e.handleForwardResp(msg)
		case topicKVReadReq:
			e.handleReadReq(pkg.Source, msg)
		case topicKVReadResp:
			e.handleReadResp(msg)
		}
	}
	return nil
}

func (e *RaftEngine) handleReadReq(source pid.PID, msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}
	env, ok := msg.Payloads[0].Data().([]byte)
	if !ok || len(env) < 9 {
		return
	}
	corr := binary.BigEndian.Uint64(env[:8])
	hop := env[8]
	key := string(env[9:])

	if e.raft.IsLeader() {
		var res readResult
		if ent, err := e.Get(key); err == nil {
			res.found, res.value, res.version, res.epoch = true, ent.Value, ent.Version, ent.Epoch
		}
		e.replyRead(source.Node, corr, res)
		return
	}
	if hop >= maxForwardHops {
		e.replyRead(source.Node, corr, readResult{err: errForwardNotLeader})
		return
	}
	go func() {
		res, err := e.forwardReadHop(key, hop+1)
		if err != nil {
			res = readResult{err: errForwardNotLeader}
		}
		e.replyRead(source.Node, corr, res)
	}()
}

func (e *RaftEngine) replyRead(node pid.NodeID, corr uint64, res readResult) {
	var flags byte
	if res.found {
		flags |= 1
	}
	if errors.Is(res.err, errForwardNotLeader) {
		flags |= 2
	}
	out := make([]byte, 29+len(res.value))
	binary.BigEndian.PutUint64(out[:8], corr)
	out[8] = flags
	binary.BigEndian.PutUint64(out[9:17], res.version)
	binary.BigEndian.PutUint64(out[17:25], res.epoch)
	binary.BigEndian.PutUint32(out[25:29], uint32(len(res.value)))
	copy(out[29:], res.value)

	pkg := relay.NewServicePackage(e.localNode, KVRaftHostID, node, KVRaftHostID,
		topicKVReadResp, payload.New(out))
	if err := e.router.Send(pkg); err != nil {
		relay.ReleasePackage(pkg)
		e.logger.Debug("kv: send read response failed", zap.Error(err))
	}
}

func (e *RaftEngine) handleReadResp(msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}
	out, ok := msg.Payloads[0].Data().([]byte)
	if !ok || len(out) < 29 {
		return
	}
	corr := binary.BigEndian.Uint64(out[:8])
	flags := out[8]
	res := readResult{
		found:   flags&1 != 0,
		version: binary.BigEndian.Uint64(out[9:17]),
		epoch:   binary.BigEndian.Uint64(out[17:25]),
	}
	if flags&2 != 0 {
		res.err = errForwardNotLeader
	}
	vlen := binary.BigEndian.Uint32(out[25:29])
	if 29+int(vlen) <= len(out) {
		res.value = append([]byte(nil), out[29:29+int(vlen)]...)
	} else if res.found {
		// A found entry whose value is truncated is a malformed frame; surface an
		// error rather than returning a silently-empty value.
		res.err = staticErr("kv: read response truncated")
	}

	e.fwdMu.Lock()
	ch, found := e.pendingReads[corr]
	e.fwdMu.Unlock()
	if !found {
		return
	}
	select {
	case ch <- res:
	default:
	}
}

func (e *RaftEngine) handleForwardReq(source pid.PID, msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}
	env, ok := msg.Payloads[0].Data().([]byte)
	if !ok || len(env) < 9 {
		return
	}
	corr := binary.BigEndian.Uint64(env[:8])
	hop := env[8]
	data := env[9:]

	if e.raft.IsLeader() {
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
		return
	}
	if hop >= maxForwardHops {
		e.replyForward(source.Node, corr, applyResult{Err: raftapi.ErrNotLeader})
		return
	}
	relayed := append([]byte(nil), data...)
	go func() {
		res, err := e.forwardToLeaderHop(relayed, hop+1)
		if err != nil {
			res = applyResult{Err: err}
		}
		e.replyForward(source.Node, corr, res)
	}()
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
