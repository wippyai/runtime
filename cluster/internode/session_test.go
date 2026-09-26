// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// runPair runs both connections of a pair, delivering B's inbound frames to
// deliver, and returns channels reporting each Run's end.
func runPair(p testConnPair, deliver func(Class, []byte)) (doneA, doneB chan *ConnectionError) {
	doneA, doneB = make(chan *ConnectionError, 1), make(chan *ConnectionError, 1)
	go func() { doneA <- p.a.Run(func(Class, []byte) {}) }()
	go func() { doneB <- p.b.Run(deliver) }()
	return doneA, doneB
}

// Frames across every class stream over a pipe while the socket is killed
// mid-stream; a new socket bound to the same sessions resumes. Every
// sequenced frame is delivered exactly once in order; gossip may be lost but
// never duplicated.
func TestSessionSurvivesSocketAbortMidStream(t *testing.T) {
	sideA := newTestSessionSide("node-B")
	sideB := newTestSessionSide("node-A")
	rec := newTrafficRecorder()
	deliver := func(_ Class, data []byte) { rec.record(data) }
	// The first socket delivers slowly so the abort lands mid-stream.
	slow := func(class Class, data []byte) {
		time.Sleep(20 * time.Microsecond)
		deliver(class, data)
	}

	classes := []Class{ClassRaftControl, ClassPGBroadcast, ClassRaftRPC, ClassSurface, ClassGossip}
	const total = 1000
	sent := make(map[Class]uint64)
	// queue admits frames; with wait unset a full bounded queue skips the
	// frame, as a sender facing ErrQueueFull would.
	queue := func(from, to int, wait bool) {
		for i := from; i < to; i++ {
			class := classes[i%len(classes)]
			data := trafficFrame(class, sent[class])
			if wait {
				sideA.pushWhenAdmitted(t, data, class)
			} else if err := sideA.nsm.QueueMessageClass(sideA.peer, data, class); err != nil {
				require.ErrorIs(t, err, ErrQueueFull)
				continue
			}
			sent[class]++
		}
	}

	first := handshakeInto(t, sideA, sideB)
	doneA, doneB := runPair(first, slow)
	queue(0, total/2, true)
	require.Eventually(t, func() bool { return rec.count(ClassRaftControl) > 10 }, 5*time.Second, time.Millisecond)
	abortConnection(first.a.conn)
	<-doneA
	<-doneB
	require.Less(t, rec.count(ClassRaftControl), int(sent[ClassRaftControl]), "the abort must land mid-stream")

	// Frames queued while no socket is up.
	queue(total/2, total*3/4, false)
	second := handshakeInto(t, sideA, sideB)
	runPair(second, deliver)
	queue(total*3/4, total, true)

	waitReliableDelivered(t, rec, sent)
	rec.requireExactlyOnceInOrder(t, sent)
}

// A full window pauses sequenced draining until the peer acknowledges;
// gossip keeps flowing; a frame larger than the window passes when nothing
// is in flight.
func TestWindowPausesDrainingUntilAck(t *testing.T) {
	cfg := insecureManagerConfig()
	cfg.LinkWindowBytes = 100
	nsm := NewNodeStateManager(cfg, newTelemetry(nil), zap.NewNop())
	nsm.CreateNodeState("peer")
	state := nsm.GetNodeState("peer")
	sess := state.session

	for range 3 {
		require.NoError(t, nsm.QueueMessageClass("peer", make([]byte, 60), ClassRaftControl))
	}
	require.NoError(t, nsm.QueueMessageClass("peer", []byte("gossip"), ClassGossip))

	batch := nsm.DrainMessages("peer", 10)
	require.Len(t, batch, 2)
	require.Equal(t, uint64(1), batch[0].seq)
	require.Equal(t, ClassGossip, batch[1].Class)
	require.Empty(t, nsm.DrainMessages("peer", 10), "window full: draining pauses")

	freed, err := applyAck(state, sess, 2)
	require.NoError(t, err)
	require.True(t, freed)
	batch = nsm.DrainMessages("peer", 10)
	require.Len(t, batch, 1)
	require.Equal(t, uint64(2), batch[0].seq)

	freed, err = applyAck(state, sess, 3)
	require.NoError(t, err)
	require.True(t, freed)
	batch = nsm.DrainMessages("peer", 10)
	require.Len(t, batch, 1)
	require.Equal(t, uint64(3), batch[0].seq)
	_, err = applyAck(state, sess, 4)
	require.NoError(t, err)

	require.NoError(t, nsm.QueueMessageClass("peer", make([]byte, 500), ClassPGBroadcast))
	require.NoError(t, nsm.QueueMessageClass("peer", make([]byte, 10), ClassPGBroadcast))
	batch = nsm.DrainMessages("peer", 10)
	require.Len(t, batch, 1, "an oversized frame passes alone when nothing is in flight")
	require.Len(t, batch[0].Data, 500)
	_, err = applyAck(state, sess, 5)
	require.NoError(t, err)
	require.Len(t, nsm.DrainMessages("peer", 10), 1)

	_, err = applyAck(state, sess, 3)
	require.Error(t, err, "a regressing ack violates the protocol")
	_, err = applyAck(state, sess, 100)
	require.Error(t, err, "acking an unsent frame violates the protocol")
}

// With a window far smaller than the traffic, acknowledgements keep the
// stream moving to completion.
func TestSmallWindowStreamCompletes(t *testing.T) {
	newSide := func(peer string) *testSessionSide {
		cfg := insecureManagerConfig()
		cfg.LinkWindowBytes = 4 << 10
		nsm := NewNodeStateManager(cfg, newTelemetry(nil), zap.NewNop())
		nsm.CreateNodeState(peer)
		return &testSessionSide{nsm: nsm, state: nsm.GetNodeState(peer), peer: peer}
	}
	sideA, sideB := newSide("node-B"), newSide("node-A")
	received := make(chan []byte, 4096)
	p := handshakeInto(t, sideA, sideB)
	runPair(p, func(_ Class, data []byte) { received <- data })
	const frames = 2000
	for i := range frames {
		data := make([]byte, 1024)
		binary.BigEndian.PutUint32(data, uint32(i))
		sideA.push(t, data, ClassPGBroadcast)
	}
	for i := range frames {
		select {
		case data := <-received:
			require.Equal(t, uint32(i), binary.BigEndian.Uint32(data))
		case <-time.After(5 * time.Second):
			t.Fatalf("stream stalled after %d frames", i)
		}
	}
}

// startRawPeer runs a session side over a pipe against a hand-driven peer
// that has agreed on the session. It returns the peer's end, the delivered
// frames, and the side's Run result.
func startRawPeer(t *testing.T) (net.Conn, chan []byte, chan *ConnectionError) {
	t.Helper()
	local, remote := net.Pipe()
	t.Cleanup(func() { _ = remote.Close() })
	side := newTestSessionSide("peer")
	conn := newNodeConnection(local, "peer", testIncarnation, DefaultNodeConnectionConfig(), zap.NewNop())
	side.bind(conn)
	t.Cleanup(conn.Close)
	delivered := make(chan []byte, 16)
	done := make(chan *ConnectionError, 1)
	go func() { done <- conn.Run(func(_ Class, data []byte) { delivered <- data }) }()
	// Consume everything the side writes so its writer never blocks.
	resume := make(chan frame, 1)
	go func() {
		first := true
		for {
			f, err := readFrame(remote, DefaultNodeConnectionConfig().MaxMessageSize)
			if err != nil {
				return
			}
			if first {
				resume <- f
				first = false
			}
		}
	}()
	theirs := <-resume
	view := make([]byte, 8)
	binary.LittleEndian.PutUint64(view, theirs.seq)
	require.NoError(t, writeFrame(remote, classResume, randomNonZero(), 1, view))
	return remote, delivered, done
}

// A duplicate of an already delivered frame is dropped; a gap is a protocol
// error that ends the connection.
func TestSequenceDuplicateDroppedGapRejected(t *testing.T) {
	remote, delivered, done := startRawPeer(t)
	require.NoError(t, writeFrame(remote, ClassRaftControl, 1, 1, []byte("one")))
	require.NoError(t, writeFrame(remote, ClassRaftControl, 1, 1, []byte("one-again")))
	require.NoError(t, writeFrame(remote, ClassRaftControl, 2, 1, []byte("two")))
	require.NoError(t, writeFrame(remote, ClassRaftControl, 4, 1, []byte("four")))

	select {
	case err := <-done:
		require.Equal(t, ExitProtocolError, err.Reason)
	case <-time.After(2 * time.Second):
		t.Fatal("gap did not end the connection")
	}
	close(delivered)
	var got []string
	for d := range delivered {
		got = append(got, string(d))
	}
	require.Equal(t, []string{"one", "two"}, got)
}

// A connection must open with RESUME.
func TestConnectionMustOpenWithResume(t *testing.T) {
	local, remote := net.Pipe()
	t.Cleanup(func() { _ = remote.Close() })
	side := newTestSessionSide("peer")
	conn := newNodeConnection(local, "peer", testIncarnation, DefaultNodeConnectionConfig(), zap.NewNop())
	side.bind(conn)
	done := make(chan *ConnectionError, 1)
	go func() { done <- conn.Run(func(Class, []byte) {}) }()
	go func() {
		for {
			if _, err := readFrame(remote, DefaultNodeConnectionConfig().MaxMessageSize); err != nil {
				return
			}
		}
	}()
	require.NoError(t, writeFrame(remote, ClassRaftControl, 1, 1, []byte("early")))
	select {
	case err := <-done:
		require.Equal(t, ExitProtocolError, err.Reason)
	case <-time.After(2 * time.Second):
		t.Fatal("frame before RESUME was accepted")
	}
}
