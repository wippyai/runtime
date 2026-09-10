// SPDX-License-Identifier: MPL-2.0
package engine

import (
	"context"
	"fmt"
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
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	securityapi "github.com/wippyai/runtime/api/security"
	socketapi "github.com/wippyai/runtime/api/socket"
	"github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/actor"
	iohost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/io"
	pollhost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/poll"
	sockethost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/sockets"
	socketservice "github.com/wippyai/runtime/service/socket"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

type udpActorNetwork struct {
	netapi.Service
	address chan string
}

func (n *udpActorNetwork) ListenPacket(ctx context.Context, network, address string) (net.PacketConn, error) {
	if network != "udp" || address != "127.0.0.1:8100" {
		return nil, fmt.Errorf("unexpected UDP bind %s %s", network, address)
	}
	conn, err := (&net.ListenConfig{}).ListenPacket(ctx, "udp4", "127.0.0.1:0")
	if err == nil {
		n.address <- conn.LocalAddr().String()
	}
	return conn, err
}

type observedUDPActorHost struct {
	*sockethost.UDPHost
	emptyReceive atomic.Bool
}

func (h *observedUDPActorHost) Register() map[string]any {
	functions := h.UDPHost.Register()
	functions["[method]incoming-datagram-stream.receive"] = func(ctx context.Context, self uint32, maxResults uint64) ([]sockethost.IncomingDatagram, *sockethost.NetworkError) {
		packets, err := h.MethodIncomingDatagramStreamReceive(ctx, self, maxResults)
		if err == nil && len(packets) == 0 {
			h.emptyReceive.Store(true)
		}
		return packets, err
	}
	return functions
}

func startUDPActorGuest(t testing.TB) (context.Context, string, <-chan concurrentTCPResult) {
	t.Helper()
	root := ctxapi.NewRootContext()
	securityapi.SetStrictMode(root, false)
	base, frame := ctxapi.OpenFrameContext(root)
	ctx, cancel := context.WithTimeout(base, time.Minute)
	rt, err := wasmrt.NewWithConfig(ctx, &wasmrt.Config{CloseOnContextDone: true})
	require.NoError(t, err)
	table := preview2.NewResourceTableWithLimits(128, 4)
	var p *ActorProcess
	var stopped chan struct{}
	t.Cleanup(func() {
		cancel()
		if stopped != nil {
			select {
			case <-stopped:
			case <-time.After(5 * time.Second):
				t.Error("UDP guest driver did not stop")
				return
			}
		}
		if p != nil {
			p.Close()
		}
		require.NoError(t, table.Close())
		require.Zero(t, table.SocketBudget().Used())
		require.NoError(t, rt.Close(context.Background()))
		require.NoError(t, frame.Close())
	})
	udpHost := &observedUDPActorHost{UDPHost: sockethost.NewUDPHost(table)}
	for _, host := range []wasmrt.Host{
		sockethost.NewUDPCreateSocketHost(table), udpHost, sockethost.NewInstanceNetworkHost(table), sockethost.NewNetworkHost(table),
		iohost.NewStreamsHost(table), iohost.NewErrorHost(table), pollhost.NewHost(table),
	} {
		require.NoError(t, rt.RegisterHost(host))
	}
	code, err := os.ReadFile("testdata/udp_actor.wasm")
	require.NoError(t, err)
	module, err := rt.LoadComponent(ctx, code)
	require.NoError(t, err)
	require.NoError(t, module.Compile(ctx))
	p = NewActorProcess(NewProcess(module, "", wasmapi.WASIConfig{}, wasmapi.LimitsConfig{MaxExecutionMS: 60000}, nil), actor.DefaultLimits(), nil)
	p.SetSocketBudget(table.SocketBudget())
	require.NoError(t, p.Init(ctx, "run", nil))
	network := &udpActorNetwork{address: make(chan string, 1)}
	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	socketservice.NewDispatcher(network).RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) { handlers[id] = h })
	ready := make(chan struct{})
	var once sync.Once
	handler := handlers[socketapi.SocketPollWait]
	handlers[socketapi.SocketPollWait] = dispatcher.HandlerFunc(func(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
		if udpHost.emptyReceive.Load() {
			once.Do(func() { close(ready) })
		}
		return handler.Handle(ctx, cmd, tag, receiver)
	})
	result := make(chan concurrentTCPResult, 1)
	stopped = make(chan struct{})
	go func() {
		defer close(stopped)
		result <- driveConcurrentTCPGuest(ctx, p, table, handlers)
	}()

	select {
	case address := <-network.address:
		select {
		case <-ready:
		case outcome := <-result:
			t.Fatalf("UDP guest stopped before idle poll: %v (value %s)", outcome.err, outcome.value)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		return ctx, address, result
	case outcome := <-result:
		t.Fatalf("server stopped before bind: %v (value %s)", outcome.err, outcome.value)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	return nil, "", nil
}

func TestUDPActorGuestDatagramBoundaries(t *testing.T) {
	parent, address, result := startUDPActorGuest(t)
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "udp4", address)
	require.NoError(t, err)
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	require.NoError(t, conn.SetDeadline(deadline))
	for _, payload := range [][]byte{[]byte("first"), {}, []byte("third")} {
		n, err := conn.Write(payload)
		require.NoError(t, err)
		require.Equal(t, len(payload), n)
		var reply [64]byte
		n, err = conn.Read(reply[:])
		if err != nil {
			select {
			case outcome := <-result:
				t.Fatalf("UDP read: %v; guest: %v (%s)", err, outcome.err, outcome.value)
			default:
			}
		}
		require.NoError(t, err)
		require.Equal(t, payload, reply[:n])
	}
	_, err = conn.Write([]byte("ACK"))
	require.NoError(t, err)
	select {
	case outcome := <-result:
		require.NoError(t, outcome.err)
		require.Equal(t, "datagrams:3", outcome.value)
		require.Positive(t, outcome.waits)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestUDPActorGuestCancellationWhilePolling(t *testing.T) {
	startUDPActorGuest(t)
}
