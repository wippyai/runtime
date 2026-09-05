// SPDX-License-Identifier: MPL-2.0
package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
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

// Only the address is redirected: accepted connections and socket dispatch use
// real loopback TCP. This exercises the documented MQTT fixture subset, not a broker.
func TestMQTTGuestLoopbackServer(t *testing.T) {
	t.Run("two-clients", func(t *testing.T) { testMQTTGuestLoopbackServer(t, false, false) })
	t.Run("oversized-frame", func(t *testing.T) { testMQTTGuestLoopbackServer(t, true, false) })
	t.Run("cancel-pending-accept", func(t *testing.T) { testMQTTGuestLoopbackServer(t, false, true) })
}

func testMQTTGuestLoopbackServer(t *testing.T, malformed, cancelAccept bool) {
	t.Helper()
	ctx, frame := ctxapi.OpenFrameContext(ctxapi.NewRootContext())
	defer frame.Close()
	rt, err := wasmrt.NewWithConfig(ctx, &wasmrt.Config{CloseOnContextDone: true})
	require.NoError(t, err)
	defer rt.Close(ctx)
	table := preview2.NewResourceTableWithLimits(64, 3)
	defer table.Close()
	for _, host := range []wasmrt.Host{
		sockethost.NewTCPCreateSocketHost(table), sockethost.NewTCPHost(table), sockethost.NewInstanceNetworkHost(table), sockethost.NewNetworkHost(table),
		iohost.NewStreamsHost(table), iohost.NewErrorHost(table), pollhost.NewHost(table),
	} {
		require.NoError(t, rt.RegisterHost(host))
	}
	code, err := os.ReadFile("testdata/mqtt.wasm")
	require.NoError(t, err)
	module, err := rt.LoadComponent(ctx, code)
	require.NoError(t, err)
	require.NoError(t, module.Compile(ctx))
	p := NewActorProcess(NewProcess(module, "", wasmapi.WASIConfig{}, wasmapi.LimitsConfig{MaxExecutionMS: 10000}, nil), actor.DefaultLimits(), nil)
	p.SetSocketBudget(table.SocketBudget())
	defer p.Close()
	require.NoError(t, p.Init(ctx, "run", nil))
	waitCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	network := &mqttTestNetwork{address: make(chan string, 1)}
	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	socketservice.NewDispatcher(network).RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) { handlers[id] = h })
	clients := make(chan error, 1)
	clientStart := make(chan struct{})
	waitedForAccept := false
	go func() {
		select {
		case addr := <-network.address:
			select {
			case <-clientStart:
			case <-waitCtx.Done():
				clients <- waitCtx.Err()
				return
			}
			if malformed {
				clients <- sendMalformedMQTTFixture(waitCtx, addr)
				return
			}
			for i := 0; i < 2; i++ {
				if err := exchangeMQTTFixture(waitCtx, addr); err != nil {
					clients <- err
					return
				}
			}
			clients <- nil
		case <-waitCtx.Done():
			clients <- waitCtx.Err()
		}
	}()
	var events []process.Event
	var out process.StepOutput
	for steps := 0; steps < 64; steps++ {
		out.Reset()
		require.NoError(t, p.Step(events, &out))
		if out.IsDone() {
			require.True(t, waitedForAccept, "guest never suspended on empty listener")
			if malformed {
				require.Equal(t, map[string]any{"err": "remaining length exceeds 4096"}, out.Result().Data())
			} else {
				require.Equal(t, map[string]any{"ok": "served:2"}, out.Result().Data())
			}
			require.Zero(t, table.SocketBudget().Used(), "server leaked listener, accepted socket, or pending accept reservation")
			select {
			case err := <-clients:
				require.NoError(t, err)
			case <-waitCtx.Done():
				t.Fatal(waitCtx.Err())
			}
			return
		}
		require.Equal(t, 1, out.Count())
		y := out.Yields()[0]
		if y.Cmd.CmdID() == socketapi.SocketPollWait && !waitedForAccept {
			waitedForAccept = true
			if !cancelAccept {
				close(clientStart)
			}
		}
		h := handlers[y.Cmd.CmdID()]
		require.NotNil(t, h, "unregistered socket command %d", y.Cmd.CmdID())
		receiver := &mqttGuestReceiver{done: make(chan process.Event, 1)}
		require.NoError(t, h.Handle(waitCtx, y.Cmd, y.Tag, receiver))
		if cancelAccept && waitedForAccept {
			cancel()
			p.Close()
			require.NoError(t, table.Close())
			require.Zero(t, table.SocketBudget().Used(), "canceled accept retained quota")
			select {
			case err := <-clients:
				require.ErrorIs(t, err, context.Canceled)
			case <-time.After(time.Second):
				t.Fatal("client waiter did not stop")
			}
			probe, err := (&net.Dialer{Timeout: time.Second}).DialContext(t.Context(), "tcp4", network.listener.Addr().String())
			if probe != nil {
				probe.Close()
			}
			require.Error(t, err, "canceled listener still accepts TCP connections")
			return
		}

		select {
		case event := <-receiver.done:
			require.NoError(t, receiver.err)
			events = []process.Event{event}
		case <-waitCtx.Done():
			t.Fatal(waitCtx.Err())
		}
	}
	t.Fatal("server exceeded bounded process steps")
}

type mqttGuestReceiver struct {
	done chan process.Event
	err  error
}

func (r *mqttGuestReceiver) CompleteYield(tag uint64, data any, err error) {
	r.err = err
	r.done <- process.Event{Type: process.EventYieldComplete, Tag: tag, Data: data}
}

type mqttTestNetwork struct {
	netapi.Service
	address  chan string
	listener net.Listener
}

func (n *mqttTestNetwork) Listen(ctx context.Context, network, address string) (net.Listener, error) {
	if network != "tcp" || address != "127.0.0.1:1883" {
		return nil, fmt.Errorf("unexpected fixture listener %s %s", network, address)
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp4", "127.0.0.1:0")
	if err == nil {
		n.listener = listener
		n.address <- listener.Addr().String()
	}
	return listener, err
}

func exchangeMQTTFixture(ctx context.Context, address string) error {
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp4", address)
	if err != nil {
		return err
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	if err = conn.SetDeadline(deadline); err != nil {
		return err
	}
	// MQTT3.1.1 CONNECT: clean session, client-id "w1x", keep-alive 60 seconds.
	connect := []byte{0x10, 15, 0, 4, 'M', 'Q', 'T', 'T', 4, 2, 0, 60, 0, 3, 'w', '1', 'x'}
	if _, err = conn.Write(connect); err != nil {
		return err
	}
	reply := make([]byte, 4)
	if _, err = io.ReadFull(conn, reply); err != nil {
		return err
	}
	if !bytes.Equal(reply, []byte{0x20, 2, 0, 0}) {
		return fmt.Errorf("invalid CONNACK %x", reply)
	}
	if _, err = conn.Write([]byte{0xc0, 0}); err != nil {
		return err
	}
	if _, err = io.ReadFull(conn, reply[:2]); err != nil {
		return err
	}
	if !bytes.Equal(reply[:2], []byte{0xd0, 0}) {
		return fmt.Errorf("invalid PINGRESP %x", reply[:2])
	}
	_, err = conn.Write([]byte{0xe0, 0})
	return err
}

func sendMalformedMQTTFixture(ctx context.Context, address string) error {
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp4", address)
	if err != nil {
		return err
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	if err = conn.SetDeadline(deadline); err != nil {
		return err
	}
	if _, err = conn.Write([]byte{0x10, 0x81, 0x20}); err != nil {
		return err
	}
	var b [1]byte
	_, err = conn.Read(b[:])
	if !errors.Is(err, io.EOF) {
		return fmt.Errorf("oversized frame did not close connection: %w", err)
	}
	return nil
}
