// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"encoding/binary"
	"errors"
	"sort"
	"time"

	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

const (
	authorityWireVersion       byte = 1
	authorityReqHeader              = 1 + 8 + 1 + 2
	authorityRespHeader             = 1 + 8 + 1 + 8 + 2
	authorityRecordHeader           = 4 + 4 + 4 + 8 + 8
	authorityStatusOK          byte = 0
	authorityStatusNotLeader   byte = 1
	authorityStatusTooLarge    byte = 2
	authorityStatusInvalid     byte = 3
	authorityStatusBusy        byte = 4
	authorityStatusUnavailable byte = 5
	authorityStatusTimeout     byte = 6
)

// ReadAuthoritySnapshot returns one detached view of keys at a KV-domain
// publication point. Leaders barrier shared Raft before reading the immutable
// FSM snapshot. Followers use the authenticated relay and never substitute a
// stale local read.
func (e *RaftEngine) ReadAuthoritySnapshot(ctx context.Context, keys []string) (kvapi.AuthoritySnapshot, error) {
	keys, err := normalizeAuthorityKeys(keys)
	if err != nil {
		return kvapi.AuthoritySnapshot{}, err
	}
	opCtx, done := e.authorityContext(ctx)
	defer done()
	if err := opCtx.Err(); err != nil {
		return kvapi.AuthoritySnapshot{}, err
	}
	permit, err := e.acquireAuthority(opCtx)
	if err != nil {
		return kvapi.AuthoritySnapshot{}, err
	}
	defer permit.release()
	if e.raft.IsLeader() {
		return e.readAuthorityLeader(opCtx, keys, permit)
	}
	if e.router == nil {
		return kvapi.AuthoritySnapshot{}, raftapi.ErrNotLeader
	}
	return e.forwardAuthority(opCtx, keys, 0)
}

func (e *RaftEngine) authorityContext(ctx context.Context) (context.Context, func()) {
	if ctx == nil {
		ctx = context.Background()
	}
	opCtx, cancel := context.WithTimeout(ctx, e.forwardWait)
	var stop func() bool
	if e.ctx != nil {
		stop = context.AfterFunc(e.ctx, cancel)
	}
	return opCtx, func() {
		if stop != nil {
			stop()
		}
		cancel()
	}
}

func (e *RaftEngine) readAuthorityLeader(ctx context.Context, keys []string, permit *authorityPermit) (kvapi.AuthoritySnapshot, error) {
	if err := e.barrierContext(ctx, permit); err != nil {
		return kvapi.AuthoritySnapshot{}, err
	}
	snap := e.fsm.snap.Load()
	if snap == nil {
		return kvapi.AuthoritySnapshot{}, kvapi.ErrKVClosed
	}
	// Preflight against the immutable snapshot before stateSnapshot.get can
	// allocate a value copy. This keeps an oversized value from allocating and
	// then being rejected by the reply envelope.
	encodedBytes := authorityRespHeader
	for _, key := range keys {
		ent := snap.shards[snapshotShard(key)][key]
		if ent == nil {
			continue
		}
		if len(ent.Value) > maxAuthoritySnapshotBytes || len(ent.LeaseID) > maxAuthoritySnapshotBytes || len(key) > maxAuthoritySnapshotKeyBytes {
			return kvapi.AuthoritySnapshot{}, kvapi.ErrSnapshotTooLarge
		}
		encodedBytes += authorityRecordHeader + len(key) + len(ent.Value) + len(ent.LeaseID)
		if encodedBytes > maxAuthoritySnapshotBytes {
			return kvapi.AuthoritySnapshot{}, kvapi.ErrSnapshotTooLarge
		}
	}
	entries := make(map[string]kvapi.Entry, len(keys))
	for _, key := range keys {
		if ent := snap.shards[snapshotShard(key)][key]; ent != nil {
			entry := *ent
			entry.Value = append([]byte(nil), ent.Value...)
			entries[key] = entry
		}
	}
	out := kvapi.AuthoritySnapshot{Entries: entries, Revision: snap.index}
	return out, nil
}

// barrierContext uses an optional context-aware implementation when available.
// Hashicorp's Barrier(timeout) bounds enqueue, not completion. A canceled
// caller transfers its admission permit to a waiter until the noncancelable
// future resolves. This bounds the number of outstanding futures to
// maxAuthorityConcurrent while allowing the caller to return.
func (e *RaftEngine) barrierContext(ctx context.Context, permit *authorityPermit) error {
	if b, ok := e.raft.(interface{ BarrierContext(context.Context) error }); ok {
		return b.BarrierContext(ctx)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	timeout := raftApplyTimeout
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return context.DeadlineExceeded
		}
		if remaining < timeout {
			timeout = remaining
		}
	}
	done := make(chan error, 1)
	go func() { done <- e.raft.Barrier(timeout) }()
	select {
	case err := <-done:
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return err
	case <-ctx.Done():
		permit.transfer()
		go func() {
			<-done
			permit.complete()
		}()
		return ctx.Err()
	}
}

func (e *RaftEngine) forwardAuthority(ctx context.Context, keys []string, hop byte) (kvapi.AuthoritySnapshot, error) {
	for attempt := 0; attempt < maxForwardRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return kvapi.AuthoritySnapshot{}, err
		}
		leaderID, _, err := e.raft.Leader()
		if err != nil || leaderID == "" {
			continue
		}
		res, err := e.sendAuthority(ctx, leaderID, keys, hop)
		if err != nil {
			return kvapi.AuthoritySnapshot{}, err
		}
		if res.notLeader {
			continue
		}
		return res.snapshot, res.err
	}
	return kvapi.AuthoritySnapshot{}, kvapi.ErrSnapshotUnavailable
}

func (e *RaftEngine) sendAuthority(ctx context.Context, leaderNode string, keys []string, hop byte) (authorityResult, error) {
	corr := kvCorrIDCounter.Add(1)
	request, err := encodeAuthorityRequest(corr, hop, keys)
	if err != nil {
		return authorityResult{}, err
	}
	ch := make(chan authorityResult, 1)
	e.fwdMu.Lock()
	e.pendingAuthority[corr] = authorityWaiter{peer: leaderNode, ch: ch, keys: append([]string(nil), keys...)}
	e.fwdMu.Unlock()
	defer func() {
		e.fwdMu.Lock()
		delete(e.pendingAuthority, corr)
		e.fwdMu.Unlock()
	}()

	pkg := relay.NewServicePackage(e.localNode, KVRaftHostID, leaderNode, KVRaftHostID,
		topicKVAuthorityReq, payload.New(request))
	sender, ok := e.router.(relay.ContextSender)
	if !ok {
		relay.ReleasePackage(pkg)
		return authorityResult{}, kvapi.ErrSnapshotUnavailable
	}
	err = sender.SendContext(ctx, pkg)
	if err != nil {
		relay.ReleasePackage(pkg)
		return authorityResult{}, err
	}
	select {
	case res := <-ch:
		return res, nil
	case <-ctx.Done():
		return authorityResult{}, ctx.Err()
	}
}

func (e *RaftEngine) handleAuthorityReq(ingress pid.NodeID, _ pid.PID, msg *relay.Message) {
	if ingress == "" {
		return
	}
	if len(msg.Payloads) != 1 {
		return
	}
	data, ok := msg.Payloads[0].Data().([]byte)
	if !ok {
		return
	}
	corr, hop, keys, err := decodeAuthorityRequest(data)
	if err != nil {
		// Malformed frames have no trustworthy correlation to answer. Dropping
		// them also keeps relay dispatch independent of peer backpressure.
		return
	}
	permit := e.tryAcquireAuthority()
	if permit == nil {
		// No reply work may bypass the admission bound. The requester's
		// operation deadline bounds its wait for a saturated peer.
		return
	}
	if e.router == nil {
		permit.release()
		return
	}
	go func() {
		defer permit.release()
		ctx, done := e.authorityContext(context.Background())
		defer done()
		if e.raft.IsLeader() {
			result, readErr := e.readAuthorityLeader(ctx, keys, permit)
			e.replyAuthority(ctx, ingress, corr, authorityResult{snapshot: result, err: readErr})
			return
		}
		if hop >= maxForwardHops {
			e.replyAuthority(ctx, ingress, corr, authorityResult{notLeader: true})
			return
		}
		result, forwardErr := e.forwardAuthority(ctx, keys, hop+1)
		if forwardErr != nil {
			result = kvapi.AuthoritySnapshot{}
		}
		e.replyAuthority(ctx, ingress, corr, authorityResult{snapshot: result, err: forwardErr, notLeader: errors.Is(forwardErr, raftapi.ErrNotLeader)})
	}()
}

func (e *RaftEngine) replyAuthority(ctx context.Context, node pid.NodeID, corr uint64, result authorityResult) {
	if e.router == nil {
		return
	}
	data, err := encodeAuthorityResponse(corr, result)
	if err != nil {
		data, _ = encodeAuthorityResponse(corr, authorityResult{err: err})
	}
	pkg := relay.NewServicePackage(e.localNode, KVRaftHostID, node, KVRaftHostID,
		topicKVAuthorityResp, payload.New(data))
	sender, ok := e.router.(relay.ContextSender)
	if !ok {
		relay.ReleasePackage(pkg)
		return
	}
	if err := sender.SendContext(ctx, pkg); err != nil {
		relay.ReleasePackage(pkg)
	}
}

func (e *RaftEngine) handleAuthorityResp(ingress pid.NodeID, _ pid.PID, msg *relay.Message) {
	if len(msg.Payloads) != 1 {
		return
	}
	data, ok := msg.Payloads[0].Data().([]byte)
	if !ok || len(data) < authorityRespHeader {
		return
	}
	corr := binary.BigEndian.Uint64(data[1:9])
	e.fwdMu.Lock()
	waiter, found := e.pendingAuthority[corr]
	e.fwdMu.Unlock()
	if !found || ingress == "" || ingress != waiter.peer {
		return
	}
	result, err := decodeAuthorityResponse(data, waiter.keys)
	if err != nil {
		result.err = err
	}
	select {
	case waiter.ch <- result:
	default:
	}
}

func normalizeAuthorityKeys(keys []string) ([]string, error) {
	if len(keys) > maxAuthoritySnapshotKeys {
		return nil, kvapi.ErrSnapshotTooLarge
	}
	out := append([]string(nil), keys...)
	sort.Strings(out)
	total := authorityReqHeader
	for i, key := range out {
		if i > 0 && out[i-1] == key {
			return nil, kvapi.ErrSnapshotInvalid
		}
		if len(key) > maxAuthoritySnapshotKeyBytes {
			return nil, kvapi.ErrSnapshotTooLarge
		}
		total += 4 + len(key)
		if total > maxAuthoritySnapshotBytes {
			return nil, kvapi.ErrSnapshotTooLarge
		}
	}
	return out, nil
}

func encodeAuthorityRequest(corr uint64, hop byte, keys []string) ([]byte, error) {
	keys, err := normalizeAuthorityKeys(keys)
	if err != nil {
		return nil, err
	}
	out := make([]byte, authorityReqHeader)
	out[0] = authorityWireVersion
	binary.BigEndian.PutUint64(out[1:9], corr)
	out[9] = hop
	binary.BigEndian.PutUint16(out[10:12], uint16(len(keys)))
	for _, key := range keys {
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(len(key)))
		out = append(out, n[:]...)
		out = append(out, key...)
	}
	return out, nil
}

func decodeAuthorityRequest(data []byte) (uint64, byte, []string, error) {
	if len(data) < authorityReqHeader || len(data) > maxAuthoritySnapshotBytes || data[0] != authorityWireVersion {
		return 0, 0, nil, kvapi.ErrSnapshotInvalid
	}
	corr := binary.BigEndian.Uint64(data[1:9])
	hop := data[9]
	count := int(binary.BigEndian.Uint16(data[10:12]))
	if count > maxAuthoritySnapshotKeys {
		return 0, 0, nil, kvapi.ErrSnapshotTooLarge
	}
	keys := make([]string, 0, count)
	off := authorityReqHeader
	for i := 0; i < count; i++ {
		if len(data)-off < 4 {
			return 0, 0, nil, kvapi.ErrSnapshotInvalid
		}
		n64 := uint64(binary.BigEndian.Uint32(data[off : off+4]))
		off += 4
		if n64 > maxAuthoritySnapshotKeyBytes || n64 > uint64(len(data)-off) {
			return 0, 0, nil, kvapi.ErrSnapshotInvalid
		}
		n := int(n64)
		key := string(data[off : off+n])
		if i > 0 && keys[i-1] >= key {
			return 0, 0, nil, kvapi.ErrSnapshotInvalid
		}
		keys = append(keys, key)
		off += n
	}
	if off != len(data) {
		return 0, 0, nil, kvapi.ErrSnapshotInvalid
	}
	return corr, hop, keys, nil
}

func encodeAuthorityResponse(corr uint64, result authorityResult) ([]byte, error) {
	status := authorityStatusOK
	switch {
	case result.notLeader:
		status = authorityStatusNotLeader
	case errors.Is(result.err, kvapi.ErrSnapshotTooLarge):
		status = authorityStatusTooLarge
	case errors.Is(result.err, kvapi.ErrSnapshotBusy):
		status = authorityStatusBusy
	case errors.Is(result.err, context.DeadlineExceeded), errors.Is(result.err, raftapi.ErrTimeout):
		status = authorityStatusTimeout
	case errors.Is(result.err, context.Canceled), errors.Is(result.err, raftapi.ErrNotLeader), errors.Is(result.err, raftapi.ErrNoLeader), errors.Is(result.err, raftapi.ErrLeadershipLost), errors.Is(result.err, kvapi.ErrSnapshotUnavailable):
		status = authorityStatusUnavailable
	case errors.Is(result.err, kvapi.ErrSnapshotInvalid):
		status = authorityStatusInvalid
	case result.err != nil:
		status = authorityStatusUnavailable
	}
	out := make([]byte, authorityRespHeader)
	out[0] = authorityWireVersion
	binary.BigEndian.PutUint64(out[1:9], corr)
	out[9] = status
	if status != authorityStatusOK {
		binary.BigEndian.PutUint16(out[18:20], 0)
		return out, nil
	}
	binary.BigEndian.PutUint64(out[10:18], result.snapshot.Revision)
	if len(result.snapshot.Entries) > maxAuthoritySnapshotKeys {
		return nil, kvapi.ErrSnapshotTooLarge
	}
	binary.BigEndian.PutUint16(out[18:20], uint16(len(result.snapshot.Entries)))
	keys := make([]string, 0, len(result.snapshot.Entries))
	for key := range result.snapshot.Entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		ent := result.snapshot.Entries[key]
		if len(key) > maxAuthoritySnapshotKeyBytes || len(ent.Value) > maxAuthoritySnapshotBytes || len(ent.LeaseID) > maxAuthoritySnapshotBytes {
			return nil, kvapi.ErrSnapshotTooLarge
		}
		recordBytes := authorityRecordHeader + len(key) + len(ent.Value) + len(ent.LeaseID)
		if len(out)+recordBytes > maxAuthoritySnapshotBytes {
			return nil, kvapi.ErrSnapshotTooLarge
		}
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(len(key)))
		out = append(out, n[:]...)
		binary.BigEndian.PutUint32(n[:], uint32(len(ent.Value)))
		out = append(out, n[:]...)
		binary.BigEndian.PutUint32(n[:], uint32(len(ent.LeaseID)))
		out = append(out, n[:]...)
		var u [8]byte
		binary.BigEndian.PutUint64(u[:], ent.Version)
		out = append(out, u[:]...)
		binary.BigEndian.PutUint64(u[:], ent.Epoch)
		out = append(out, u[:]...)
		out = append(out, key...)
		out = append(out, ent.Value...)
		out = append(out, ent.LeaseID...)
	}
	return out, nil
}

func decodeAuthorityResponse(data []byte, requested []string) (authorityResult, error) {
	if len(data) < authorityRespHeader || len(data) > maxAuthoritySnapshotBytes || data[0] != authorityWireVersion {
		return authorityResult{}, kvapi.ErrSnapshotInvalid
	}
	status := data[9]
	if status > authorityStatusTimeout {
		return authorityResult{}, kvapi.ErrSnapshotInvalid
	}
	if status != authorityStatusOK {
		if len(data) != authorityRespHeader || binary.BigEndian.Uint64(data[10:18]) != 0 || binary.BigEndian.Uint16(data[18:20]) != 0 {
			return authorityResult{}, kvapi.ErrSnapshotInvalid
		}
		if status == authorityStatusNotLeader {
			return authorityResult{notLeader: true}, nil
		}
		return authorityResult{err: authorityStatusError(status)}, nil
	}
	count := int(binary.BigEndian.Uint16(data[18:20]))
	if count > maxAuthoritySnapshotKeys {
		return authorityResult{}, kvapi.ErrSnapshotTooLarge
	}
	off := authorityRespHeader
	entries := make(map[string]kvapi.Entry, count)
	previousKey := ""
	for i := 0; i < count; i++ {
		if len(data)-off < authorityRecordHeader {
			return authorityResult{}, kvapi.ErrSnapshotInvalid
		}
		keyLen64 := uint64(binary.BigEndian.Uint32(data[off : off+4]))
		valueLen64 := uint64(binary.BigEndian.Uint32(data[off+4 : off+8]))
		leaseLen64 := uint64(binary.BigEndian.Uint32(data[off+8 : off+12]))
		version := binary.BigEndian.Uint64(data[off+12 : off+20])
		epoch := binary.BigEndian.Uint64(data[off+20 : off+28])
		off += authorityRecordHeader
		if keyLen64 > maxAuthoritySnapshotKeyBytes || valueLen64 > maxAuthoritySnapshotBytes || leaseLen64 > maxAuthoritySnapshotBytes {
			return authorityResult{}, kvapi.ErrSnapshotTooLarge
		}
		if keyLen64 > uint64(len(data)-off) {
			return authorityResult{}, kvapi.ErrSnapshotInvalid
		}
		keyLen, valueLen, leaseLen := int(keyLen64), int(valueLen64), int(leaseLen64)
		key := string(data[off : off+keyLen])
		off += keyLen
		if valueLen > len(data)-off {
			return authorityResult{}, kvapi.ErrSnapshotInvalid
		}
		value := append([]byte(nil), data[off:off+valueLen]...)
		off += valueLen
		if leaseLen > len(data)-off {
			return authorityResult{}, kvapi.ErrSnapshotInvalid
		}
		lease := string(data[off : off+leaseLen])
		off += leaseLen
		if i > 0 && previousKey >= key {
			return authorityResult{}, kvapi.ErrSnapshotInvalid
		}
		previousKey = key
		if _, found := sortKey(requested, key); !found {
			return authorityResult{}, kvapi.ErrSnapshotInvalid
		}
		entries[key] = kvapi.Entry{Key: key, Value: value, LeaseID: kvapi.LeaseID(lease), Version: version, Epoch: epoch}
	}
	if off != len(data) {
		return authorityResult{}, kvapi.ErrSnapshotInvalid
	}
	return authorityResult{snapshot: kvapi.AuthoritySnapshot{Entries: entries, Revision: binary.BigEndian.Uint64(data[10:18])}}, nil
}

func sortKey(keys []string, key string) (int, bool) {
	i := sort.SearchStrings(keys, key)
	return i, i < len(keys) && keys[i] == key
}

func authorityStatusError(status byte) error {
	switch status {
	case authorityStatusTooLarge:
		return kvapi.ErrSnapshotTooLarge
	case authorityStatusBusy:
		return kvapi.ErrSnapshotBusy
	case authorityStatusTimeout:
		return raftapi.ErrTimeout
	case authorityStatusUnavailable:
		return kvapi.ErrSnapshotUnavailable
	default:
		return kvapi.ErrSnapshotInvalid
	}
}
