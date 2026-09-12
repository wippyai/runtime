// SPDX-License-Identifier: MPL-2.0
package internode

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type observedFlushConn struct {
	net.Conn
	entered chan struct{}
	once    sync.Once
}

func (c *observedFlushConn) Write(data []byte) (int, error) {
	c.once.Do(func() { close(c.entered) })
	return c.Conn.Write(data)
}

func TestWriterReleasesRetentionOnlyAfterSuccessfulFlush(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	observed := &observedFlushConn{Conn: a, entered: make(chan struct{})}
	connection := newNodeConnection(observed, "peer", DefaultNodeConnectionConfig(), zap.NewNop())
	source := newTestDrainSource()
	source.push([]byte("retained"), ClassRaftControl)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	completed := make(chan []Outbound, 1)
	connection.bindDrain(source.notify, source.drain, source.requeue, 32, func(batch []Outbound) { completed <- batch; cancel() })
	done := make(chan *ConnectionError, 1)
	go func() { done <- connection.writeLoop(ctx) }()
	select {
	case <-observed.entered:
	case <-time.After(time.Second):
		t.Fatal("writer did not reach socket")
	}
	// The actual socket has no reader yet: merely draining cannot complete.
	select {
	case <-completed:
		t.Fatal("retention released before socket consumed frame")
	default:
	}
	class, data, err := readFrame(b, 1024)
	require.NoError(t, err)
	require.Equal(t, ClassRaftControl, class)
	require.Equal(t, []byte("retained"), data)
	select {
	case batch := <-completed:
		require.Len(t, batch, 1)
		require.Equal(t, data, batch[0].Data)
	case <-time.After(time.Second):
		t.Fatal("successful flush did not complete retention")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("writer did not join")
	}
	require.Empty(t, source.requeuedMessages())
}

func TestWriterFailedFlushRetainsBatchForRetry(t *testing.T) {
	a, b := newMockConnPair()
	defer a.Close()
	defer b.Close()
	injected := errors.New("failed write")
	a.setWriteError(injected)
	connection := newNodeConnection(a, "peer", DefaultNodeConnectionConfig(), zap.NewNop())
	source := newTestDrainSource()
	source.push([]byte("retained"), ClassRaftControl)
	completions := 0
	connection.bindDrain(source.notify, source.drain, source.requeue, 32, func([]Outbound) { completions++ })
	result := connection.writeLoop(context.Background())
	require.ErrorIs(t, result.Err, injected)
	require.Zero(t, completions, "failed flush must not release reservation")
	require.Len(t, source.requeuedMessages(), 1)
	require.Equal(t, []byte("retained"), source.requeuedMessages()[0].Data)
}

func TestWriterDrainsShortBatchesWithoutAnotherNotification(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	connection := newNodeConnection(a, "peer", DefaultNodeConnectionConfig(), zap.NewNop())
	source := newTestDrainSource()
	for _, data := range []string{"one", "two", "three"} {
		source.push([]byte(data), ClassRaftControl)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	flushed := 0
	// A byte budget can produce one frame even when the count budget is32.
	// The queue was populated before connection; no subsequent notify is sent.
	connection.bindDrain(make(chan struct{}), func(int) []Outbound { return source.drain(1) }, source.requeue, 32, func(batch []Outbound) {
		flushed += len(batch)
		if flushed == 3 {
			cancel()
		}
	})
	done := make(chan *ConnectionError, 1)
	go func() { done <- connection.writeLoop(ctx) }()
	defer func() { cancel(); connection.Close(); <-done }()
	require.NoError(t, b.SetReadDeadline(time.Now().Add(2*time.Second)))
	for _, expected := range []string{"one", "two", "three"} {
		_, data, err := readFrame(b, 1024)
		require.NoError(t, err)
		require.Equal(t, expected, string(data))
	}
}
