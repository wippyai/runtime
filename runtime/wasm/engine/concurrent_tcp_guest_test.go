// SPDX-License-Identifier: MPL-2.0
package engine

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/process"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	"github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/actor"
	iohost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/io"
	pollhost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/poll"
	sockethost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/sockets"
	socketservice "github.com/wippyai/runtime/service/socket"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

const concurrentTCPClients = 8
const concurrentTCPFrame = 64

type concurrentTCPNetwork struct {
	netapi.Service
	address chan string
}

func (n *concurrentTCPNetwork) Listen(ctx context.Context, network, address string) (net.Listener, error) {
	if network != "tcp" || address != "127.0.0.1:8099" {
		return nil, fmt.Errorf("unexpected listener %s %s", network, address)
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp4", "127.0.0.1:0")
	if err == nil {
		n.address <- listener.Addr().String()
	}
	return listener, err
}

type concurrentTCPResult struct {
	err   error
	value string
	waits int
}

func startConcurrentTCPGuest(t testing.TB) (context.Context, string, <-chan concurrentTCPResult) {
	t.Helper()
	base, frame := ctxapi.OpenFrameContext(ctxapi.NewRootContext())
	ctx, cancel := context.WithTimeout(base, time.Minute)
	rt, err := wasmrt.NewWithConfig(ctx, &wasmrt.Config{CloseOnContextDone: true})
	require.NoError(t, err)
	table := preview2.NewResourceTableWithLimits(128, concurrentTCPClients+2)
	var p *ActorProcess
	var stopped chan struct{}
	t.Cleanup(func() {
		cancel()
		if stopped != nil {
			select {
			case <-stopped:
			case <-time.After(5 * time.Second):
				t.Error("TCP guest driver did not stop")
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
	for _, host := range []wasmrt.Host{
		sockethost.NewTCPCreateSocketHost(table), sockethost.NewTCPHost(table), sockethost.NewInstanceNetworkHost(table), sockethost.NewNetworkHost(table),
		iohost.NewStreamsHost(table), iohost.NewErrorHost(table), pollhost.NewHost(table),
	} {
		require.NoError(t, rt.RegisterHost(host))
	}
	code, err := os.ReadFile("testdata/concurrent_tcp.wasm")
	require.NoError(t, err)
	module, err := rt.LoadComponent(ctx, code)
	require.NoError(t, err)
	require.NoError(t, module.Compile(ctx))
	p = NewActorProcess(NewProcess(module, "", wasmapi.WASIConfig{}, wasmapi.LimitsConfig{MaxExecutionMS: 60000}, nil), actor.DefaultLimits(), nil)
	p.SetSocketBudget(table.SocketBudget())
	require.NoError(t, p.Init(ctx, "run", nil))
	network := &concurrentTCPNetwork{address: make(chan string, 1)}
	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	socketservice.NewDispatcher(network).RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) { handlers[id] = h })
	result := make(chan concurrentTCPResult, 1)
	stopped = make(chan struct{})
	go func() {
		defer close(stopped)
		result <- driveConcurrentTCPGuest(ctx, p, table, handlers)
	}()

	select {
	case address := <-network.address:
		return ctx, address, result
	case outcome := <-result:
		t.Fatalf("server stopped before listen: %+v", outcome)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	return nil, "", nil
}

func driveConcurrentTCPGuest(ctx context.Context, p *ActorProcess, table *preview2.ResourceTable, handlers map[dispatcher.CommandID]dispatcher.Handler) concurrentTCPResult {
	var events []process.Event
	var out process.StepOutput
	waits := 0
	for {
		out.Reset()
		if err := p.Step(events, &out); err != nil {
			return concurrentTCPResult{err: err}
		}
		if out.IsDone() {
			if used := table.SocketBudget().Used(); used != 0 {
				return concurrentTCPResult{err: fmt.Errorf("guest retained %d socket reservations", used)}
			}
			result, ok := out.Result().Data().(map[string]any)
			if !ok || result["err"] != nil {
				return concurrentTCPResult{err: fmt.Errorf("guest result: %v", out.Result().Data())}
			}
			return concurrentTCPResult{value: fmt.Sprint(result["ok"]), waits: waits}
		}
		if out.Count() != 1 {
			return concurrentTCPResult{err: fmt.Errorf("guest yielded %d commands", out.Count())}
		}
		y := out.Yields()[0]
		handler := handlers[y.Cmd.CmdID()]
		if handler == nil {
			return concurrentTCPResult{err: fmt.Errorf("unhandled command %d", y.Cmd.CmdID())}
		}
		receiver := &mqttGuestReceiver{done: make(chan process.Event, 1)}
		if err := handler.Handle(ctx, y.Cmd, y.Tag, receiver); err != nil {
			return concurrentTCPResult{err: err}
		}
		waits++
		select {
		case event := <-receiver.done:
			if receiver.err != nil {
				return concurrentTCPResult{err: receiver.err}
			}
			events = []process.Event{event}
		case <-ctx.Done():
			return concurrentTCPResult{err: ctx.Err()}
		}
	}
}

func exchangeConcurrentTCP(ctx context.Context, address string, client, frames int) (time.Duration, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp4", address)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return 0, err
	}
	request := bytes.Repeat([]byte{byte(client)}, concurrentTCPFrame)
	var reply [concurrentTCPFrame]byte
	var elapsed time.Duration
	for i := 0; i < frames; i++ {
		request[0] = byte(i)
		start := time.Now()
		if _, err := conn.Write(request); err != nil {
			return 0, err
		}
		if _, err := io.ReadFull(conn, reply[:]); err != nil {
			return 0, err
		}
		elapsed += time.Since(start)
		if !bytes.Equal(request, reply[:]) {
			return 0, fmt.Errorf("client %d frame %d echo corrupted", client, i)
		}
	}
	return elapsed, nil
}

func TestConcurrentTCPGuestSlowClientDoesNotBlockPeers(t *testing.T) {
	parent, address, result := startConcurrentTCPGuest(t)
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	slow, err := (&net.Dialer{}).DialContext(ctx, "tcp4", address)
	require.NoError(t, err)
	defer slow.Close()
	deadline, _ := ctx.Deadline()
	require.NoError(t, slow.SetDeadline(deadline))
	request := bytes.Repeat([]byte{0xA5}, concurrentTCPFrame)
	_, err = slow.Write(request[:concurrentTCPFrame/2])
	require.NoError(t, err)
	var wg sync.WaitGroup
	errs := make(chan error, concurrentTCPClients-1)
	for client := 1; client < concurrentTCPClients; client++ {
		wg.Add(1)
		go func(client int) {
			defer wg.Done()
			_, err := exchangeConcurrentTCP(ctx, address, client, 4)
			errs <- err
		}(client)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err, "an incomplete frame blocked other connections")
	}
	_, err = slow.Write(request[concurrentTCPFrame/2:])
	require.NoError(t, err)
	var reply [concurrentTCPFrame]byte
	_, err = io.ReadFull(slow, reply[:])
	require.NoError(t, err)
	require.Equal(t, request, reply[:])
	require.NoError(t, slow.Close())
	select {
	case outcome := <-result:
		require.NoError(t, outcome.err)
		require.Equal(t, "frames:29", outcome.value)
		require.Positive(t, outcome.waits)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestConcurrentTCPGuestPartialEOFReleasesSockets(t *testing.T) {
	parent, address, result := startConcurrentTCPGuest(t)
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp4", address)
	require.NoError(t, err)
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	require.NoError(t, conn.SetDeadline(deadline))
	_, err = conn.Write([]byte{1, 2, 3})
	require.NoError(t, err)
	require.NoError(t, conn.(*net.TCPConn).CloseWrite())
	select {
	case outcome := <-result:
		require.ErrorContains(t, outcome.err, "partial EOF: input closed at 3 of 64-byte frame")
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestConcurrentTCPGuestCancellationJoinsDriver(t *testing.T) {
	// Returning with the server still waiting for clients exercises cancellation
	// cleanup: the driver must stop before process and socket resources close.
	startConcurrentTCPGuest(t)
}
