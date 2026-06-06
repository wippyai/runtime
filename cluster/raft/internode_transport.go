// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/go-msgpack/v2/codec"
	hraft "github.com/hashicorp/raft"
	"go.uber.org/zap"

	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/cluster/internode"
)

const (
	raftRPCAppendEntries uint8 = iota + 1
	raftRPCRequestVote
	raftRPCRequestPreVote
	raftRPCInstallSnapshot
	raftRPCTimeoutNow
)

const (
	raftSnapshotChunkSize = 64 * 1024
	maxInboundRaftRPC     = 256
)

type raftFrame struct {
	Error    string
	Payload  []byte
	Snapshot []byte
	ID       uint64
	Type     uint8
	Request  bool
	EOF      bool
}

type raftMessageTransport struct {
	connMgr     internode.ConnectionManager
	heartbeatFn atomic.Value
	logger      *zap.Logger
	pending     map[uint64]chan raftFrame
	snapshots   map[raftRequestKey]*raftSnapshotStream
	consumerCh  chan hraft.RPC
	inflight    chan struct{}
	closeCh     chan struct{}
	local       cluster.NodeID
	nextID      atomic.Uint64
	timeout     time.Duration
	closeOnce   sync.Once
	mu          sync.Mutex
}

type raftHeartbeatHandler struct {
	fn func(hraft.RPC)
}

type raftRequestKey struct {
	peer cluster.NodeID
	id   uint64
}

type raftSnapshotStream struct {
	writer *io.PipeWriter
	timer  *time.Timer
}

func newRaftMessageTransport(local cluster.NodeID, connMgr internode.ConnectionManager, timeout time.Duration, logger *zap.Logger) (*raftMessageTransport, error) {
	t := &raftMessageTransport{
		local:      local,
		connMgr:    connMgr,
		timeout:    timeout,
		logger:     logger,
		consumerCh: make(chan hraft.RPC, 256),
		inflight:   make(chan struct{}, maxInboundRaftRPC),
		closeCh:    make(chan struct{}),
		pending:    map[uint64]chan raftFrame{},
		snapshots:  map[raftRequestKey]*raftSnapshotStream{},
	}
	if !connMgr.RegisterClassReceiver(internode.ClassRaftRPC, t.onFrame) {
		return nil, errors.New("raft internode: raft RPC receiver already registered")
	}
	return t, nil
}

func (t *raftMessageTransport) Consumer() <-chan hraft.RPC { return t.consumerCh }

func (t *raftMessageTransport) LocalAddr() hraft.ServerAddress {
	return hraft.ServerAddress(t.local)
}

func (t *raftMessageTransport) AppendEntriesPipeline(id hraft.ServerID, target hraft.ServerAddress) (hraft.AppendPipeline, error) {
	return newRaftMessagePipeline(t, id, target), nil
}

func (t *raftMessageTransport) AppendEntries(id hraft.ServerID, target hraft.ServerAddress, args *hraft.AppendEntriesRequest, resp *hraft.AppendEntriesResponse) error {
	return t.rpc(id, target, raftRPCAppendEntries, args, resp, t.timeout)
}

func (t *raftMessageTransport) RequestVote(id hraft.ServerID, target hraft.ServerAddress, args *hraft.RequestVoteRequest, resp *hraft.RequestVoteResponse) error {
	return t.rpc(id, target, raftRPCRequestVote, args, resp, t.timeout)
}

func (t *raftMessageTransport) RequestPreVote(id hraft.ServerID, target hraft.ServerAddress, args *hraft.RequestPreVoteRequest, resp *hraft.RequestPreVoteResponse) error {
	return t.rpc(id, target, raftRPCRequestPreVote, args, resp, t.timeout)
}

func (t *raftMessageTransport) InstallSnapshot(_ hraft.ServerID, target hraft.ServerAddress, args *hraft.InstallSnapshotRequest, resp *hraft.InstallSnapshotResponse, data io.Reader) error {
	timeout := t.timeout
	if args.Size > 0 {
		timeout += time.Duration(args.Size/(64*1024*1024)) * t.timeout
	}
	return t.snapshotRPC(target, args, resp, data, timeout)
}

func (t *raftMessageTransport) TimeoutNow(id hraft.ServerID, target hraft.ServerAddress, args *hraft.TimeoutNowRequest, resp *hraft.TimeoutNowResponse) error {
	return t.rpc(id, target, raftRPCTimeoutNow, args, resp, 10*t.timeout)
}

func (t *raftMessageTransport) EncodePeer(_ hraft.ServerID, addr hraft.ServerAddress) []byte {
	return []byte(addr)
}

func (t *raftMessageTransport) DecodePeer(buf []byte) hraft.ServerAddress {
	return hraft.ServerAddress(buf)
}

func (t *raftMessageTransport) SetHeartbeatHandler(cb func(hraft.RPC)) {
	t.heartbeatFn.Store(&raftHeartbeatHandler{fn: cb})
}

func (t *raftMessageTransport) Close() error {
	t.closeOnce.Do(func() {
		close(t.closeCh)
		_ = t.connMgr.RegisterClassReceiver(internode.ClassRaftRPC, nil)

		t.mu.Lock()
		for id := range t.pending {
			delete(t.pending, id)
		}
		for key, stream := range t.snapshots {
			if stream.timer != nil {
				stream.timer.Stop()
			}
			_ = stream.writer.CloseWithError(hraft.ErrTransportShutdown)
			delete(t.snapshots, key)
		}
		t.mu.Unlock()
	})
	return nil
}

func (t *raftMessageTransport) rpc(_ hraft.ServerID, target hraft.ServerAddress, typ uint8, args any, resp any, timeout time.Duration) error {
	payload, err := encodeMsgpack(args)
	if err != nil {
		return err
	}

	id := t.nextID.Add(1)
	respCh := make(chan raftFrame, 1)
	t.mu.Lock()
	t.pending[id] = respCh
	t.mu.Unlock()
	defer t.removePending(id)

	frame := raftFrame{ID: id, Type: typ, Request: true, Payload: payload}
	if err := t.sendFrame(cluster.NodeID(target), frame); err != nil {
		return err
	}

	return t.waitReply(typ, target, respCh, resp, timeout)
}

func (t *raftMessageTransport) snapshotRPC(target hraft.ServerAddress, args *hraft.InstallSnapshotRequest, resp *hraft.InstallSnapshotResponse, data io.Reader, timeout time.Duration) error {
	payload, err := encodeMsgpack(args)
	if err != nil {
		return err
	}

	id := t.nextID.Add(1)
	respCh := make(chan raftFrame, 1)
	t.mu.Lock()
	t.pending[id] = respCh
	t.mu.Unlock()
	defer t.removePending(id)

	peer := cluster.NodeID(target)
	header := raftFrame{ID: id, Type: raftRPCInstallSnapshot, Request: true, Payload: payload}
	if err := t.sendFrame(peer, header); err != nil {
		return err
	}
	if err := t.sendSnapshotChunks(peer, id, data); err != nil {
		return err
	}
	return t.waitReply(raftRPCInstallSnapshot, target, respCh, resp, timeout)
}

func (t *raftMessageTransport) sendSnapshotChunks(peer cluster.NodeID, id uint64, data io.Reader) error {
	buf := make([]byte, raftSnapshotChunkSize)
	for {
		n, readErr := data.Read(buf)
		if n > 0 {
			frame := raftFrame{
				ID:       id,
				Type:     raftRPCInstallSnapshot,
				Request:  true,
				Snapshot: buf[:n],
			}
			if err := t.sendFrame(peer, frame); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			return t.sendFrame(peer, raftFrame{ID: id, Type: raftRPCInstallSnapshot, Request: true, EOF: true})
		}
		if readErr != nil {
			_ = t.sendFrame(peer, raftFrame{ID: id, Type: raftRPCInstallSnapshot, Request: true, EOF: true, Error: readErr.Error()})
			return readErr
		}
	}
}

func (t *raftMessageTransport) sendFrame(peer cluster.NodeID, frame raftFrame) error {
	wire, err := encodeMsgpack(&frame)
	if err != nil {
		return err
	}
	return t.connMgr.SendToNode(peer, wire, internode.ClassRaftRPC)
}

func (t *raftMessageTransport) waitReply(typ uint8, target hraft.ServerAddress, respCh <-chan raftFrame, resp any, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = t.timeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case reply, ok := <-respCh:
		if !ok {
			return hraft.ErrTransportShutdown
		}
		if reply.Error != "" {
			return errors.New(reply.Error)
		}
		return decodeMsgpack(reply.Payload, resp)
	case <-timer.C:
		return fmt.Errorf("raft internode: rpc %d to %s timed out", typ, target)
	case <-t.closeCh:
		return hraft.ErrTransportShutdown
	}
}

func (t *raftMessageTransport) removePending(id uint64) {
	t.mu.Lock()
	delete(t.pending, id)
	t.mu.Unlock()
}

func (t *raftMessageTransport) onFrame(peer cluster.NodeID, data []byte) {
	var frame raftFrame
	if err := decodeMsgpack(data, &frame); err != nil {
		t.logger.Warn("raft internode: decode frame failed", zap.String("peer", peer), zap.Error(err))
		return
	}
	if !frame.Request {
		t.mu.Lock()
		ch := t.pending[frame.ID]
		t.mu.Unlock()
		if ch == nil {
			return
		}
		select {
		case ch <- frame:
		case <-t.closeCh:
		}
		return
	}
	if frame.Type == raftRPCInstallSnapshot && len(frame.Payload) == 0 {
		t.handleSnapshotChunk(peer, frame)
		return
	}
	if frame.Type == raftRPCInstallSnapshot {
		key := raftRequestKey{peer: peer, id: frame.ID}
		reader, writer := io.Pipe()
		stream := &raftSnapshotStream{writer: writer}
		stream.timer = time.AfterFunc(t.timeout, func() {
			t.removeSnapshotWriter(key, errors.New("raft internode: snapshot stream timed out"))
		})
		t.mu.Lock()
		t.snapshots[key] = stream
		t.mu.Unlock()
		t.dispatchRequest(peer, frame, reader, key)
		return
	}
	t.dispatchRequest(peer, frame, nil, raftRequestKey{})
}

func (t *raftMessageTransport) dispatchRequest(peer cluster.NodeID, frame raftFrame, snapshotReader *io.PipeReader, snapshotKey raftRequestKey) {
	select {
	case t.inflight <- struct{}{}:
		go func() {
			defer func() { <-t.inflight }()
			t.handleRequest(peer, frame, snapshotReader, snapshotKey)
		}()
	case <-t.closeCh:
		if snapshotReader != nil {
			t.removeSnapshotWriter(snapshotKey, hraft.ErrTransportShutdown)
			_ = snapshotReader.Close()
		}
	default:
		if snapshotReader != nil {
			t.removeSnapshotWriter(snapshotKey, errors.New("raft internode: inbound rpc limit reached"))
			_ = snapshotReader.Close()
		}
		t.sendReply(peer, frame.ID, frame.Type, nil, errors.New("raft internode: inbound rpc limit reached"))
	}
}

func (t *raftMessageTransport) handleRequest(peer cluster.NodeID, frame raftFrame, snapshotReader *io.PipeReader, snapshotKey raftRequestKey) {
	var cmd any
	var err error
	switch frame.Type {
	case raftRPCAppendEntries:
		req := new(hraft.AppendEntriesRequest)
		err = decodeMsgpack(frame.Payload, req)
		cmd = req
	case raftRPCRequestVote:
		req := new(hraft.RequestVoteRequest)
		err = decodeMsgpack(frame.Payload, req)
		cmd = req
	case raftRPCRequestPreVote:
		req := new(hraft.RequestPreVoteRequest)
		err = decodeMsgpack(frame.Payload, req)
		cmd = req
	case raftRPCInstallSnapshot:
		req := new(hraft.InstallSnapshotRequest)
		err = decodeMsgpack(frame.Payload, req)
		cmd = req
	case raftRPCTimeoutNow:
		req := new(hraft.TimeoutNowRequest)
		err = decodeMsgpack(frame.Payload, req)
		cmd = req
	default:
		err = fmt.Errorf("unknown raft rpc type %d", frame.Type)
	}
	if err != nil {
		t.sendReply(peer, frame.ID, frame.Type, nil, err)
		return
	}

	respCh := make(chan hraft.RPCResponse, 1)
	rpc := hraft.RPC{Command: cmd, RespChan: respCh}
	if frame.Type == raftRPCInstallSnapshot {
		defer t.removeSnapshotWriter(snapshotKey, hraft.ErrTransportShutdown)
		rpc.Reader = snapshotReader
	}

	if t.tryHeartbeatFastPath(rpc) {
		t.waitAndReply(peer, frame, respCh)
		return
	}

	select {
	case t.consumerCh <- rpc:
		t.waitAndReply(peer, frame, respCh)
	case <-t.closeCh:
	}
}

func (t *raftMessageTransport) handleSnapshotChunk(peer cluster.NodeID, frame raftFrame) {
	key := raftRequestKey{peer: peer, id: frame.ID}
	t.mu.Lock()
	stream := t.snapshots[key]
	t.mu.Unlock()
	if stream == nil {
		return
	}
	stream.timer.Reset(t.timeout)
	if frame.Error != "" {
		t.removeSnapshotWriter(key, errors.New(frame.Error))
		return
	}
	if len(frame.Snapshot) > 0 {
		if _, err := stream.writer.Write(frame.Snapshot); err != nil {
			t.removeSnapshotWriter(key, err)
			return
		}
	}
	if frame.EOF {
		t.removeSnapshotWriter(key, nil)
	}
}

func (t *raftMessageTransport) removeSnapshotWriter(key raftRequestKey, err error) {
	t.mu.Lock()
	stream := t.snapshots[key]
	if stream != nil {
		delete(t.snapshots, key)
	}
	t.mu.Unlock()
	if stream == nil {
		return
	}
	if stream.timer != nil {
		stream.timer.Stop()
	}
	if err != nil {
		_ = stream.writer.CloseWithError(err)
		return
	}
	_ = stream.writer.Close()
}

func (t *raftMessageTransport) tryHeartbeatFastPath(rpc hraft.RPC) bool {
	req, ok := rpc.Command.(*hraft.AppendEntriesRequest)
	if !ok || req.Term == 0 || req.PrevLogEntry != 0 || req.PrevLogTerm != 0 ||
		len(req.Entries) != 0 || req.LeaderCommitIndex != 0 {
		return false
	}
	if len(req.Addr) == 0 {
		return false
	}
	handler, _ := t.heartbeatFn.Load().(*raftHeartbeatHandler)
	if handler == nil || handler.fn == nil {
		return false
	}
	handler.fn(rpc)
	return true
}

func (t *raftMessageTransport) waitAndReply(peer cluster.NodeID, frame raftFrame, respCh <-chan hraft.RPCResponse) {
	select {
	case resp := <-respCh:
		t.sendReply(peer, frame.ID, frame.Type, resp.Response, resp.Error)
	case <-t.closeCh:
	}
}

func (t *raftMessageTransport) sendReply(peer cluster.NodeID, id uint64, typ uint8, resp any, rpcErr error) {
	payload, err := encodeMsgpack(resp)
	errStr := ""
	if rpcErr != nil {
		errStr = rpcErr.Error()
	}
	if err != nil && errStr == "" {
		errStr = err.Error()
	}
	frame := raftFrame{ID: id, Type: typ, Payload: payload, Error: errStr}
	wire, err := encodeMsgpack(&frame)
	if err != nil {
		t.logger.Warn("raft internode: encode reply failed", zap.String("peer", peer), zap.Error(err))
		return
	}
	if err := t.connMgr.SendToNode(peer, wire, internode.ClassRaftRPC); err != nil {
		t.logger.Debug("raft internode: send reply failed", zap.String("peer", peer), zap.Error(err))
	}
}

func encodeMsgpack(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := codec.NewEncoder(&buf, &codec.MsgpackHandle{}).Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decodeMsgpack(data []byte, v any) error {
	return codec.NewDecoder(bytes.NewReader(data), &codec.MsgpackHandle{}).Decode(v)
}

type raftMessagePipeline struct {
	trans     *raftMessageTransport
	doneCh    chan hraft.AppendFuture
	closeCh   chan struct{}
	id        hraft.ServerID
	target    hraft.ServerAddress
	closeOnce sync.Once
}

func newRaftMessagePipeline(trans *raftMessageTransport, id hraft.ServerID, target hraft.ServerAddress) *raftMessagePipeline {
	return &raftMessagePipeline{
		trans:   trans,
		id:      id,
		target:  target,
		doneCh:  make(chan hraft.AppendFuture, 256),
		closeCh: make(chan struct{}),
	}
}

func (p *raftMessagePipeline) AppendEntries(args *hraft.AppendEntriesRequest, resp *hraft.AppendEntriesResponse) (hraft.AppendFuture, error) {
	select {
	case <-p.closeCh:
		return nil, hraft.ErrPipelineShutdown
	default:
	}

	fut := &raftAppendFuture{start: time.Now(), args: args, resp: resp, errCh: make(chan error, 1)}
	go func() {
		err := p.trans.AppendEntries(p.id, p.target, args, resp)
		fut.errCh <- err
		close(fut.errCh)
		select {
		case p.doneCh <- fut:
		case <-p.closeCh:
		case <-p.trans.closeCh:
		}
	}()
	return fut, nil
}

func (p *raftMessagePipeline) Consumer() <-chan hraft.AppendFuture { return p.doneCh }

func (p *raftMessagePipeline) Close() error {
	p.closeOnce.Do(func() {
		close(p.closeCh)
	})
	return nil
}

type raftAppendFuture struct {
	start time.Time
	args  *hraft.AppendEntriesRequest
	resp  *hraft.AppendEntriesResponse
	errCh chan error
	err   error
	once  sync.Once
}

func (f *raftAppendFuture) Error() error {
	f.once.Do(func() {
		f.err = <-f.errCh
	})
	return f.err
}

func (f *raftAppendFuture) Index() uint64 { return 0 }

func (f *raftAppendFuture) Response() *hraft.AppendEntriesResponse { return f.resp }

func (f *raftAppendFuture) Start() time.Time { return f.start }

func (f *raftAppendFuture) Request() *hraft.AppendEntriesRequest { return f.args }
