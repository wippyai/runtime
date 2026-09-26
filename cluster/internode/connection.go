// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wippyai/runtime/api/cluster"
	"go.uber.org/zap"
)

var (
	ErrConnectionClosed = errors.New("internode: connection is closed")
	ErrMessageTooLarge  = errors.New("internode: message exceeds max size")
	ErrCleanShutdown    = errors.New("internode: clean shutdown")
)

// ExitReason defines the category of error that caused a connection to terminate.
type ExitReason int

const (
	ExitUnknown ExitReason = iota
	ExitCleanShutdown
	ExitNetworkError
	ExitProtocolError
	ExitPeerClosed
)

// String returns a human-readable representation of the ExitReason.
func (er ExitReason) String() string {
	switch er {
	case ExitUnknown:
		return "UNKNOWN"
	case ExitCleanShutdown:
		return "CLEAN_SHUTDOWN"
	case ExitNetworkError:
		return "NETWORK_ERROR"
	case ExitProtocolError:
		return "PROTOCOL_ERROR"
	case ExitPeerClosed:
		return "PEER_CLOSED"
	default:
		return "UNKNOWN"
	}
}

// ConnectionError is a structured error returned by a connection's Run loop.
// It provides both the reason for termination and the underlying error.
type ConnectionError struct {
	Err    error
	Reason ExitReason
}

// Error implements the error interface.
func (ce *ConnectionError) Error() string {
	if ce.Err != nil {
		return fmt.Sprintf("%s: %v", ce.Reason, ce.Err)
	}
	return ce.Reason.String()
}

// Unwrap returns the underlying error for error wrapping support.
func (ce *ConnectionError) Unwrap() error { return ce.Err }

// ShouldRetry determines whether the connection should be retried based on the exit reason.
func (ce *ConnectionError) ShouldRetry() bool {
	switch ce.Reason {
	case ExitNetworkError, ExitPeerClosed:
		return true
	case ExitCleanShutdown, ExitProtocolError:
		return false
	case ExitUnknown:
		return false
	default:
		return false
	}
}

const (
	// protocolVersion v3 sequences frames on a per-peer session and carries
	// the sender's cumulative ack in every frame. Earlier versions are not
	// accepted; all nodes in a cluster upgrade together.
	protocolVersion = 0x03
	// frameHeaderSize: version u8, class u8, length u32, seq u64, ack u64.
	frameHeaderSize        = 22
	defaultWriteBufferSize = 64 * 1024
	// ackEvery bounds how many frames the reader delivers before it wakes the
	// writer to acknowledge them while more input is buffered.
	ackEvery = 32
)

// Outbound is one frame handed to a writer. The Class is preserved
// end-to-end so the receiver can dispatch by sub-protocol without inspecting
// the payload; seq is the frame's session sequence number, zero for gossip.
type Outbound struct {
	Data  []byte
	Class Class
	seq   uint64
}

// NodeConnectionConfig holds configuration parameters for a NodeConnection.
type NodeConnectionConfig struct {
	AuthorizePeer         func(cluster.NodeID, net.Addr) bool
	ResolvePeerKey        func(cluster.NodeID) (ed25519.PublicKey, bool)
	AuthenticationKey     []byte
	SigningKey            ed25519.PrivateKey
	HandshakeTimeout      time.Duration
	MaxMessageSize        uint32
	RequireAuthentication bool
}

// DefaultNodeConnectionConfig returns a default set of configuration parameters
// for NodeConnection with reasonable timeout and size limits.
func DefaultNodeConnectionConfig() NodeConnectionConfig {
	return NodeConnectionConfig{
		HandshakeTimeout: 5 * time.Second,
		MaxMessageSize:   512 * 1024 * 1024,
	}
}

// NodeConnection is one framed socket carrying a peer session. Its writeLoop
// drains the session (wired via bindSession) and its readLoop delivers the
// peer's frames exactly once, in order. The session outlives the socket: a
// replacement connection resumes it.
type NodeConnection struct {
	conn       net.Conn
	logger     *zap.Logger
	cancel     context.CancelFunc
	link       *sessionLink
	runDone    chan struct{}
	remoteNode cluster.NodeID
	config     NodeConnectionConfig
	// peerIncarnation is the peer process incarnation proven by the handshake.
	peerIncarnation uint64
	lifecycleMu     sync.Mutex
	closed          atomic.Bool
	// dialed reports that this node opened the connection as the handshake
	// client.
	dialed bool
}

// newNodeConnection creates a new, un-started NodeConnection. bindSession
// must be called before Run.
func newNodeConnection(conn net.Conn, remoteNode cluster.NodeID, peerIncarnation uint64, config NodeConnectionConfig, logger *zap.Logger) *NodeConnection {
	return &NodeConnection{
		conn:            conn,
		logger:          logger.With(zap.String("remote_node", remoteNode)),
		config:          config,
		remoteNode:      remoteNode,
		peerIncarnation: peerIncarnation,
		runDone:         make(chan struct{}),
	}
}

// sessionLink binds a connection to the peer session it carries.
type sessionLink struct {
	nsm   *NodeStateManager
	state *NodeState
	sess  *session
	// resumed is closed once the peer's RESUME is reconciled; the writer
	// holds sequenced frames until then.
	resumed chan struct{}
	nodeID  cluster.NodeID
	batch   int
}

// wakeWriter nudges the writer to drain or acknowledge.
func (l *sessionLink) wakeWriter() {
	select {
	case l.state.messageNotify <- struct{}{}:
	default:
	}
}

// bindSession wires the connection to state's current session. It MUST be
// called before Run.
func (c *NodeConnection) bindSession(nsm *NodeStateManager, nodeID cluster.NodeID, state *NodeState, batch int) {
	state.queueMu.Lock()
	sess := state.session
	state.queueMu.Unlock()
	c.link = &sessionLink{
		nsm:     nsm,
		state:   state,
		sess:    sess,
		resumed: make(chan struct{}),
		nodeID:  nodeID,
		batch:   batch,
	}
}

// Run starts the connection's read/write loops and blocks until termination.
// It returns a ConnectionError indicating the reason for termination. The
// handler receives every delivered inbound frame tagged with its sub-protocol
// Class so callers can dispatch by class without re-parsing payload.
//
// The read pump runs INLINE on the caller's goroutine; only the write pump
// is spawned. Steady state is 3 long-lived goroutines per connected peer: the
// control loop, this read pump, and the write pump.
//
// First-error-wins teardown: whichever pump fails first records its error and
// Close()s the connection (cancel ctx + close net.Conn), which unblocks the
// other pump. The writer records its error BEFORE calling Close so a
// writer-originated failure is captured before the socket close wakes the
// inline reader; the reader then observes ExitCleanShutdown and Run returns
// the writer's error. When Run returns, no further frame is delivered.
func (c *NodeConnection) Run(handler func(class Class, msg []byte)) *ConnectionError {
	defer close(c.runDone)
	c.lifecycleMu.Lock()
	// Close may win before the monitor goroutine starts. Do not publish a
	// fresh writer context after the one-shot Close has already completed.
	if c.closed.Load() {
		c.lifecycleMu.Unlock()
		return &ConnectionError{Reason: ExitCleanShutdown, Err: ErrCleanShutdown}
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.lifecycleMu.Unlock()

	defer c.Close()

	if c.link == nil {
		c.logger.Error("connection started without a bound session")
		return &ConnectionError{Reason: ExitProtocolError, Err: ErrConnectionClosed}
	}

	errCh := make(chan *ConnectionError, 1)
	writeDone := make(chan struct{})

	// record keeps only the first non-clean error.
	record := func(err *ConnectionError) {
		if err == nil || err.Reason == ExitCleanShutdown {
			return
		}
		select {
		case errCh <- err:
		default:
		}
	}

	go func() {
		defer close(writeDone)
		if err := c.writeLoop(ctx); err != nil && err.Reason != ExitCleanShutdown {
			record(err) // record before Close so it wins over the reader's clean-shutdown
			c.Close()   // cancels ctx + closes net.Conn, unblocking the inline reader
		}
	}()

	// Inline read pump on the caller's goroutine. The session learns of the
	// live reader so an end signal waits until no frame can be delivered.
	var readErr *ConnectionError
	if c.link.beginRead() {
		readErr = c.readLoop(ctx, handler)
		c.link.endRead()
	} else {
		readErr = &ConnectionError{Reason: ExitCleanShutdown, Err: errSessionEnded}
	}
	// Attribute the reader's error only if the reader is the FIRST cause:
	// if the connection is already closed when the reader exits, the socket
	// error is a consequence of an external Close() or a writer-triggered
	// teardown, not an independent failure.
	if !c.closed.Load() {
		record(readErr)
	}

	c.Close() // idempotent; stops the writer after the reader exits
	<-writeDone

	select {
	case err := <-errCh:
		return err
	default:
		return &ConnectionError{Reason: ExitCleanShutdown, Err: ErrCleanShutdown}
	}
}

// Close aborts the socket and cancels all ongoing operations. The session's
// unacknowledged frames stay in its resend ring for the next connection.
func (c *NodeConnection) Close() {
	if c.closed.CompareAndSwap(false, true) {
		c.lifecycleMu.Lock()
		if c.cancel != nil {
			c.cancel()
		}
		c.lifecycleMu.Unlock()
		abortConnection(c.conn)
	}
}

// RemoteNodeID returns the identifier of the connected peer node.
func (c *NodeConnection) RemoteNodeID() cluster.NodeID {
	return c.remoteNode
}

// writeLoop announces the session with RESUME, holds sequenced frames until
// the peer's RESUME is reconciled, then drains the session: frames awaiting
// retransmission first, then queued frames. Every frame carries the current
// cumulative ack; a standalone ack goes out only when no frame carried it.
func (c *NodeConnection) writeLoop(ctx context.Context) *ConnectionError {
	l := c.link
	writer := bufio.NewWriterSize(c.conn, defaultWriteBufferSize)

	id, view, lastAck := resumeState(l.state, l.sess)
	var viewBuf [8]byte
	binary.LittleEndian.PutUint64(viewBuf[:], view)
	if err := writeFrame(writer, classResume, id, lastAck, viewBuf[:]); err != nil {
		return c.writeFailure(ctx, err)
	}
	if err := writer.Flush(); err != nil {
		return c.writeFailure(ctx, err)
	}
	select {
	case <-l.resumed:
	case <-ctx.Done():
		return &ConnectionError{Reason: ExitCleanShutdown, Err: ErrCleanShutdown}
	}

	for {
		for ctx.Err() == nil {
			batch := l.nsm.drainSession(l.nodeID, l.state, l.sess, l.batch)
			if len(batch) == 0 {
				break
			}
			ack := l.sess.recvNext.Load()
			if err := c.flushBatch(writer, batch, ack); err != nil {
				return c.writeFailure(ctx, err)
			}
			lastAck = ack
			if len(batch) < l.batch {
				break
			}
		}
		if ack := l.sess.recvNext.Load(); ack != lastAck && ctx.Err() == nil {
			if err := writeFrame(writer, classAck, 0, ack, nil); err != nil {
				return c.writeFailure(ctx, err)
			}
			if err := writer.Flush(); err != nil {
				return c.writeFailure(ctx, err)
			}
			lastAck = ack
		}

		select {
		case <-l.state.messageNotify:
		case <-ctx.Done():
			return &ConnectionError{Reason: ExitCleanShutdown, Err: ErrCleanShutdown}
		}
	}
}

// writeFailure classifies a write error. A failure caused by an intentional
// local close must not be reported as a writer-originated network error.
func (c *NodeConnection) writeFailure(ctx context.Context, err error) *ConnectionError {
	if ctx.Err() != nil || c.closed.Load() {
		return &ConnectionError{Reason: ExitCleanShutdown, Err: ErrCleanShutdown}
	}
	return &ConnectionError{Reason: ExitNetworkError, Err: err}
}

// flushBatch writes every frame in batch with the given ack and flushes. A
// failed batch stays in the session's resend ring.
func (c *NodeConnection) flushBatch(writer *bufio.Writer, batch []Outbound, ack uint64) error {
	for _, msg := range batch {
		if err := writeFrame(writer, msg.Class, msg.seq, ack, msg.Data); err != nil {
			return err
		}
	}
	return writer.Flush()
}

// readLoop reconciles the peer's RESUME, then delivers the peer's frames:
// sequenced frames exactly once in order, duplicates of replayed frames
// dropped, gossip as it arrives. Every frame's ack trims this side's ring.
func (c *NodeConnection) readLoop(ctx context.Context, handler func(class Class, msg []byte)) *ConnectionError {
	l := c.link
	reader := bufio.NewReader(c.conn)

	f, err := readFrame(reader, c.config.MaxMessageSize)
	if err != nil {
		return c.readFailure(ctx, err)
	}
	if f.class != classResume {
		return &ConnectionError{Reason: ExitProtocolError, Err: newFrameError("connection must open with RESUME", f.class)}
	}
	outcome, err := l.nsm.resume(l.nodeID, l.state, l.sess, f.seq, binary.LittleEndian.Uint64(f.data), f.ack)
	if err != nil {
		if errors.Is(err, errSessionEnded) {
			return &ConnectionError{Reason: ExitCleanShutdown, Err: err}
		}
		return &ConnectionError{Reason: ExitProtocolError, Err: err}
	}
	switch outcome {
	case resumePeerReset:
		return &ConnectionError{Reason: ExitPeerClosed, Err: errPeerSessionReset}
	case resumeAwaitPeerReset:
		// The peer ends its session on this side's RESUME and closes; no
		// frame may follow.
		f, err := readFrame(reader, c.config.MaxMessageSize)
		if err != nil {
			return c.readFailure(ctx, err)
		}
		return &ConnectionError{Reason: ExitProtocolError, Err: newFrameError("frame before session agreement", f.class)}
	case resumeReady:
	}
	close(l.resumed)

	delivered := 0
	for {
		if ctx.Err() != nil {
			return &ConnectionError{Reason: ExitCleanShutdown, Err: ctx.Err()}
		}
		f, err := readFrame(reader, c.config.MaxMessageSize)
		if err != nil {
			return c.readFailure(ctx, err)
		}
		freed, err := applyAck(l.state, l.sess, f.ack)
		if err != nil {
			return &ConnectionError{Reason: ExitProtocolError, Err: err}
		}
		if freed {
			l.wakeWriter()
		}
		switch {
		case f.class == classAck:
			continue
		case f.class == classResume:
			return &ConnectionError{Reason: ExitProtocolError, Err: newFrameError("RESUME after session agreement", f.class)}
		case f.class.sequenced():
			next := l.sess.recvNext.Load()
			if f.seq < next {
				// A replayed frame the previous connection already delivered.
				continue
			}
			if f.seq > next {
				return &ConnectionError{Reason: ExitProtocolError, Err: newSequenceGapError(next, f.seq)}
			}
		}
		if l.sess.ended.Load() {
			return &ConnectionError{Reason: ExitCleanShutdown, Err: errSessionEnded}
		}
		// Hot path: no logging.
		handler(f.class, f.data)
		if f.class.sequenced() {
			l.sess.recvNext.Store(f.seq + 1)
			delivered++
			if reader.Buffered() == 0 || delivered%ackEvery == 0 {
				l.wakeWriter()
			}
		}
	}
}

// readFailure classifies a read error.
func (c *NodeConnection) readFailure(ctx context.Context, err error) *ConnectionError {
	if errors.Is(err, io.EOF) || strings.Contains(err.Error(), "use of closed network connection") {
		if ctx.Err() != nil {
			return &ConnectionError{Reason: ExitCleanShutdown, Err: ErrCleanShutdown}
		}
		return &ConnectionError{Reason: ExitPeerClosed, Err: err}
	}
	if errors.Is(err, ErrMessageTooLarge) {
		return &ConnectionError{Reason: ExitProtocolError, Err: err}
	}
	var perr protocolError
	if errors.As(err, &perr) {
		return &ConnectionError{Reason: ExitProtocolError, Err: perr}
	}
	return &ConnectionError{Reason: ExitNetworkError, Err: err}
}

// protocolError represents an error in the internode communication protocol.
type protocolError string

// Error implements the error interface for protocolError.
func (e protocolError) Error() string { return "protocol error: " + string(e) }

// frame is one decoded wire frame.
type frame struct {
	data  []byte
	seq   uint64
	ack   uint64
	class Class
}

// writeFrame writes one frame: header [version][class][length][seq][ack]
// followed by the payload.
func writeFrame(w io.Writer, class Class, seq, ack uint64, data []byte) error {
	dataLen := len(data)
	if dataLen > math.MaxUint32 {
		return NewMessageTooLargeError(dataLen)
	}
	var header [frameHeaderSize]byte
	header[0] = protocolVersion
	header[1] = byte(class)
	binary.LittleEndian.PutUint32(header[2:], uint32(dataLen))
	binary.LittleEndian.PutUint64(header[6:], seq)
	binary.LittleEndian.PutUint64(header[14:], ack)
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	if dataLen > 0 {
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	return nil
}

// readFrame reads one frame, validating the protocol version, the class and
// its sequencing rule, and the payload size.
func readFrame(r io.Reader, maxMessageSize uint32) (frame, error) {
	var header [frameHeaderSize]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return frame{}, err
	}
	if header[0] != protocolVersion {
		return frame{}, protocolError(fmt.Sprintf("unexpected protocol version %d", header[0]))
	}
	f := frame{
		class: Class(header[1]),
		seq:   binary.LittleEndian.Uint64(header[6:]),
		ack:   binary.LittleEndian.Uint64(header[14:]),
	}
	size := binary.LittleEndian.Uint32(header[2:])
	switch {
	case f.class == classAck:
		if size != 0 || f.seq != 0 {
			return frame{}, protocolError("ack frame carries a payload or sequence")
		}
	case f.class == classResume:
		if size != 8 || f.seq == 0 {
			return frame{}, protocolError("malformed resume frame")
		}
	case int(f.class) >= numClasses:
		return frame{}, protocolError(fmt.Sprintf("unknown sub-protocol class %d", header[1]))
	case f.class.sequenced() != (f.seq != 0):
		return frame{}, protocolError(fmt.Sprintf("class %s frame with sequence %d", f.class, f.seq))
	}
	if f.class == ClassSurface && size > MaxSurfaceFrameSize {
		return frame{}, NewMessageSizeExceedsMaxError(int(size), MaxSurfaceFrameSize)
	}
	if size > maxMessageSize {
		return frame{}, NewMessageSizeExceedsMaxError(int(size), int(maxMessageSize))
	}

	if size == 0 {
		f.data = []byte{}
		return f, nil
	}

	f.data = make([]byte, size)
	if _, err := io.ReadFull(r, f.data); err != nil {
		return frame{}, err
	}
	return f, nil
}
