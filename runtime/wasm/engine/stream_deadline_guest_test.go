// SPDX-License-Identifier: MPL-2.0
package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/process"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	socketapi "github.com/wippyai/runtime/api/socket"
	"github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/actor"
	iohost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/io"
	pollhost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/poll"
	sockethost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/sockets"
	socketservice "github.com/wippyai/runtime/service/socket"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

func TestStreamGuestConfiguredDeadline(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode byte
	}{
		{"read", 'R'}, {"skip", 'K'}, {"write", 'W'}, {"zeroes", 'Z'},
		{"flush", 'F'}, {"splice", 'S'}, {"idle-poll", 'I'}, {"delayed-success", 'D'},
	} {
		t.Run(tc.name, func(t *testing.T) { testStreamGuestDeadline(t, tc.mode) })
	}
}

func testStreamGuestDeadline(t *testing.T, mode byte) {
	t.Helper()
	const timeout = 80 * time.Millisecond
	ctx, frame := ctxapi.OpenFrameContext(ctxapi.NewRootContext())
	defer frame.Close()
	rt, err := wasmrt.NewWithConfig(ctx, &wasmrt.Config{CloseOnContextDone: true})
	require.NoError(t, err)
	defer rt.Close(ctx)
	table := preview2.NewResourceTableWithLimits(64, 2)
	defer table.Close()
	for _, host := range []wasmrt.Host{
		sockethost.NewTCPCreateSocketHost(table), sockethost.NewTCPHost(table), sockethost.NewInstanceNetworkHost(table), sockethost.NewNetworkHost(table),
		iohost.NewStreamsHost(table), iohost.NewErrorHost(table), pollhost.NewHost(table),
	} {
		require.NoError(t, rt.RegisterHost(host))
	}
	code, err := os.ReadFile("testdata/stream_deadline.wasm")
	require.NoError(t, err)
	module, err := rt.LoadComponent(ctx, code)
	require.NoError(t, err)
	require.NoError(t, module.Compile(ctx))
	limits := wasmapi.LimitsConfig{MaxExecutionMS: 10000, SocketTimeoutMS: int(timeout / time.Millisecond)}
	p := NewActorProcess(NewProcess(module, "", wasmapi.WASIConfig{}, limits, nil), actor.DefaultLimits(), nil)
	p.SetSocketBudget(table.SocketBudget())
	defer p.Close()
	require.NoError(t, p.Init(ctx, "run", nil))
	waitCtx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	left, right := net.Pipe()
	conn := &streamDeadlineConn{Conn: left, closed: make(chan struct{})}
	t.Cleanup(func() { left.Close(); right.Close() })
	require.NoError(t, right.SetDeadline(time.Now().Add(10*time.Second)))
	releaseOutput := make(chan struct{})
	var releaseOnce sync.Once
	peerDone := make(chan error, 1)
	go func() { peerDone <- runStreamDeadlinePeer(waitCtx, right, conn.closed, releaseOutput, mode, timeout) }()
	network := &streamDeadlineNetwork{conn: conn}
	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	socketservice.NewDispatcher(network).RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) { handlers[id] = h })
	var events []process.Event
	var out process.StepOutput
	waits := 0
	delayed := false
	for steps := 0; steps < 32; steps++ {
		out.Reset()
		require.NoError(t, p.Step(events, &out))
		if out.IsDone() {
			want := "timed-out:closed"
			if mode == 'I' {
				want = "idle:done"
				require.Zero(t, waits, "generic poll acquired an operation deadline")
			}
			if mode == 'D' {
				want = "written"
				require.True(t, delayed)
			}
			require.Equal(t, map[string]any{"ok": want}, out.Result().Data())
			if mode != 'I' {
				require.Positive(t, waits, "guest did not suspend its blocking operation")
			}
			require.Zero(t, table.SocketBudget().Used())
			require.Equal(t, int32(1), conn.closes.Load(), "socket leaked or physically closed more than once")
			select {
			case err := <-peerDone:
				require.NoError(t, err)
			case <-waitCtx.Done():
				t.Fatal("peer did not stop")
			}
			return
		}
		require.Equal(t, 1, out.Count())
		y := out.Yields()[0]
		stream, isStream := y.Cmd.(*socketapi.StreamWaitCmd)
		if isStream {
			waits++
			require.False(t, stream.Deadline.IsZero(), "TCP blocking operation has no deadline")
			if mode == 'D' {
				releaseOnce.Do(func() { close(releaseOutput) })
			}
		}
		h := handlers[y.Cmd.CmdID()]
		require.NotNil(t, h)
		receiver := &mqttGuestReceiver{done: make(chan process.Event, 1)}
		require.NoError(t, h.Handle(waitCtx, y.Cmd, y.Tag, receiver))
		select {
		case event := <-receiver.done:
			require.NoError(t, receiver.err)
			if mode == 'D' && isStream {
				timer := time.NewTimer(time.Until(stream.Deadline) + 25*time.Millisecond)
				select {
				case <-timer.C:
				case <-waitCtx.Done():
					timer.Stop()
					t.Fatal(waitCtx.Err())
				}
				delayed = true
			}
			events = []process.Event{event}
		case <-waitCtx.Done():
			t.Fatal("guest stream operation did not finish")
		}
	}
	t.Fatal("guest exceeded bounded stream steps")
}

type streamDeadlineNetwork struct {
	netapi.Service
	conn net.Conn
}

func (n *streamDeadlineNetwork) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" || address != "127.0.0.1:8099" {
		return nil, fmt.Errorf("unexpected dial %s %s", network, address)
	}
	return n.conn, ctx.Err()
}

type streamDeadlineConn struct {
	net.Conn
	closed chan struct{}
	once   sync.Once
	closes atomic.Int32
}

func (c *streamDeadlineConn) Close() error {
	c.closes.Add(1)
	err := c.Conn.Close()
	c.once.Do(func() { close(c.closed) })
	return err
}
func (*streamDeadlineConn) SetDeadline(time.Time) error { return errors.New("deadline unsupported") }
func (*streamDeadlineConn) SetReadDeadline(time.Time) error {
	return errors.New("read deadline unsupported")
}
func (*streamDeadlineConn) SetWriteDeadline(time.Time) error {
	return errors.New("write deadline unsupported")
}

func runStreamDeadlinePeer(ctx context.Context, conn net.Conn, closed, release <-chan struct{}, mode byte, timeout time.Duration) error {
	if _, err := conn.Write([]byte{mode}); err != nil {
		return err
	}
	if mode == 'S' {
		if _, err := conn.Write([]byte("data")); err != nil {
			return err
		}
	}
	if mode == 'I' {
		timer := time.NewTimer(4 * timeout)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return ctx.Err()
		}
		if _, err := conn.Write([]byte("done")); err != nil {
			return err
		}
	}
	if mode == 'D' {
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
		var data [4]byte
		if _, err := io.ReadFull(conn, data[:]); err != nil {
			return err
		}
		if string(data[:]) != "ping" {
			return fmt.Errorf("write payload %q", data[:])
		}
	}
	if mode == 'W' || mode == 'F' || mode == 'Z' {
		var prefix [2]byte
		if _, err := io.ReadFull(conn, prefix[:]); err != nil {
			return err
		}
		if mode != 'Z' && string(prefix[:]) != "pi" {
			return fmt.Errorf("partial write %q", prefix[:])
		}
		if mode == 'Z' && prefix != [2]byte{} {
			return fmt.Errorf("partial zeroes %v", prefix)
		}
	}
	select {
	case <-closed:
	case <-ctx.Done():
		return ctx.Err()
	}
	var extra [1]byte
	n, err := conn.Read(extra[:])
	if n != 0 || !errors.Is(err, io.EOF) {
		return fmt.Errorf("unexpected data after completion: %d bytes, %w", n, err)
	}
	return nil
}
