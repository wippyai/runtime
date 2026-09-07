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
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/process"
	relayapi "github.com/wippyai/runtime/api/relay"
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

const concurrentTCPClients = 8
const concurrentTCPFrame = 64

type concurrentTCPNetwork struct {
	netapi.Service
	address chan string
	reads   chan int
	arm     func()
}

func (n *concurrentTCPNetwork) Listen(ctx context.Context, network, address string) (net.Listener, error) {
	if network != "tcp" || address != "127.0.0.1:8099" {
		return nil, fmt.Errorf("unexpected listener %s %s", network, address)
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	n.address <- listener.Addr().String()
	if n.reads == nil {
		return listener, nil
	}
	return &concurrentTCPObservedListener{Listener: listener, reads: n.reads, arm: n.arm}, nil
}

// concurrentTCPObservedListener exposes the real host read that transfers the
// partial frame out of the peer connection. The guest fixture then performs its
// next input read and blocks in socket.poll because the frame is incomplete.
type concurrentTCPObservedListener struct {
	net.Listener
	reads chan int
	arm   func()
}

func (l *concurrentTCPObservedListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &concurrentTCPObservedConn{Conn: conn, reads: l.reads, arm: l.arm}, nil
}

type concurrentTCPObservedConn struct {
	net.Conn
	reads     chan int
	arm       func()
	readBytes int
	once      sync.Once
}

func (c *concurrentTCPObservedConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.readBytes += n
		if c.readBytes >= concurrentTCPFrame/2 {
			c.once.Do(func() {
				// Arm before returning the half-frame to the stream driver;
				// the guest can reach its next poll immediately afterward.
				if c.arm != nil {
					c.arm()
				}
				c.reads <- c.readBytes
			})
		}
	}
	return n, err
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

// concurrentTCPActorInstance owns the runtime resources for one actor PID. An
// actor process never recycles this instance; cleanup records the exact close
// result before releasing it.
type concurrentTCPActorInstance struct {
	table      *preview2.ResourceTable
	cleanup    chan struct{}
	cleanupErr error
	used       int
	calls      atomic.Uint32
	closed     atomic.Uint32
	mu         sync.Mutex
}

func (i *concurrentTCPActorInstance) release(rt *wasmrt.Runtime) {
	i.calls.Add(1)
	if !i.closed.CompareAndSwap(0, 1) {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.table.Close(); err != nil {
		i.cleanupErr = fmt.Errorf("close resource table: %w", err)
	} else {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := rt.Close(closeCtx); err != nil {
			i.cleanupErr = fmt.Errorf("close WASM runtime: %w", err)
		}
	}
	i.used = i.table.SocketBudget().Used()
	close(i.cleanup)
}

func (i *concurrentTCPActorInstance) assertClean(t testing.TB) {
	t.Helper()
	select {
	case <-i.cleanup:
	case <-time.After(5 * time.Second):
		t.Fatal("actor runtime resources were not released")
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	require.Equal(t, uint32(1), i.calls.Load(), "actor resource cleanup was called more than once")
	require.Equal(t, uint32(1), i.closed.Load())
	require.NoError(t, i.cleanupErr)
	require.Zero(t, i.used, "actor retained socket reservations at resource cleanup")
}

type concurrentTCPActorFactory struct {
	instances chan *concurrentTCPActorInstance
	wasm      []byte
}

func (f *concurrentTCPActorFactory) create() (process.Process, error) {
	ctx := context.Background()
	rt, err := wasmrt.NewWithConfig(ctx, &wasmrt.Config{CloseOnContextDone: true})
	if err != nil {
		return nil, fmt.Errorf("new WASM runtime: %w", err)
	}
	table := preview2.NewResourceTableWithLimits(128, concurrentTCPClients+2)
	fail := func(err error) (process.Process, error) {
		_ = table.Close()
		_ = rt.Close(context.Background())
		return nil, err
	}
	for _, host := range []wasmrt.Host{
		sockethost.NewTCPCreateSocketHost(table), sockethost.NewTCPHost(table), sockethost.NewInstanceNetworkHost(table), sockethost.NewNetworkHost(table),
		iohost.NewStreamsHost(table), iohost.NewErrorHost(table), pollhost.NewHost(table),
	} {
		if err := rt.RegisterHost(host); err != nil {
			return fail(fmt.Errorf("register %s host: %w", host.Namespace(), err))
		}
	}
	module, err := rt.LoadComponent(ctx, f.wasm)
	if err != nil {
		return fail(fmt.Errorf("load concurrent TCP component: %w", err))
	}
	if err := module.Compile(ctx); err != nil {
		return fail(fmt.Errorf("compile concurrent TCP component: %w", err))
	}
	instance := &concurrentTCPActorInstance{table: table, cleanup: make(chan struct{})}
	actorProc := NewActorProcess(
		NewProcess(module, "", wasmapi.WASIConfig{}, wasmapi.LimitsConfig{MaxExecutionMS: 30000}, nil),
		actor.DefaultLimits(),
		func() { instance.release(rt) },
	)
	actorProc.SetSocketBudget(table.SocketBudget())
	f.instances <- instance
	return actorProc, nil
}

type concurrentTCPPollObservation struct {
	returned chan error
}

type observedConcurrentTCPPollHandler struct {
	next    dispatcher.Handler
	entered chan *concurrentTCPPollObservation
	armed   atomic.Bool
}

func (h *observedConcurrentTCPPollHandler) Handle(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	poll, ok := cmd.(*socketapi.PollWaitCmd)
	if !ok || !h.armed.Load() {
		return h.next.Handle(ctx, cmd, tag, receiver)
	}
	wait := poll.Wait
	observation := &concurrentTCPPollObservation{returned: make(chan error, 1)}
	wrapped := *poll
	wrapped.Wait = func(waitCtx context.Context) ([]uint32, error) {
		select {
		case h.entered <- observation:
		default:
		}
		indexes, err := wait(waitCtx)
		observation.returned <- err
		return indexes, err
	}
	return h.next.Handle(ctx, &wrapped, tag, receiver)
}

func awaitConcurrentTCPAddress(ctx context.Context, t testing.TB, addresses <-chan string) string {
	t.Helper()
	select {
	case address := <-addresses:
		return address
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	return ""
}

func awaitConcurrentTCPInstance(ctx context.Context, t testing.TB, instances <-chan *concurrentTCPActorInstance) *concurrentTCPActorInstance {
	t.Helper()
	select {
	case instance := <-instances:
		return instance
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	return nil
}

// TestConcurrentTCPGuestBlockedReadCancellationViaActorScheduler verifies the
// cancellation path for a real guest read blocked in socket.poll. The actor is
// run by the production scheduler/host/relay route, not by a test Step loop.
func TestConcurrentTCPGuestBlockedReadCancellationViaActorScheduler(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	wasm, err := os.ReadFile("testdata/concurrent_tcp.wasm")
	require.NoError(t, err)
	factory := &concurrentTCPActorFactory{wasm: wasm, instances: make(chan *concurrentTCPActorInstance, 2)}
	network := &concurrentTCPNetwork{address: make(chan string, 2), reads: make(chan int, 2)}
	polls := &observedConcurrentTCPPollHandler{entered: make(chan *concurrentTCPPollObservation, 4)}
	network.arm = func() { polls.armed.Store(true) }

	cluster := newHarnessClusterWithCommands(t, 1, factory.create, func(register func(dispatcher.CommandID, dispatcher.Handler)) {
		socketservice.NewDispatcher(network).RegisterAll(func(id dispatcher.CommandID, handler dispatcher.Handler) {
			if id == socketapi.SocketPollWait {
				polls.next = handler
				register(id, polls)
				return
			}
			register(id, handler)
		})
	})

	blockedPID := cluster.SpawnActor(t, "concurrent-tcp-blocked-read")
	blocked := awaitConcurrentTCPInstance(ctx, t, factory.instances)
	blockedDone := cluster.lifecycle.registerWait(blockedPID)
	address := awaitConcurrentTCPAddress(ctx, t, network.address)

	peer, err := (&net.Dialer{}).DialContext(ctx, "tcp4", address)
	require.NoError(t, err)
	defer peer.Close()
	deadline, _ := ctx.Deadline()
	require.NoError(t, peer.SetDeadline(deadline))
	partial := bytes.Repeat([]byte{0xD4}, concurrentTCPFrame/2)
	_, err = peer.Write(partial)
	require.NoError(t, err)
	select {
	case n := <-network.reads:
		require.Equal(t, len(partial), n, "host did not consume the partial frame")
	case <-ctx.Done():
		t.Fatal("host did not read the peer's partial frame")
	}

	var observation *concurrentTCPPollObservation
	select {
	case observation = <-polls.entered:
	case <-ctx.Done():
		t.Fatal("guest did not enter the blocked socket read poll")
	}

	// The observed host read proves that the checked-in fixture has accepted
	// this peer and has its half-frame. Its next input read cannot complete a
	// 64-byte frame, so this socket.poll is the blocked read barrier.
	require.NoError(t, cluster.host.Terminate(ctx, blockedPID))
	select {
	case result := <-blockedDone:
		require.Error(t, result.Error, "terminated blocked actor completed successfully")
	case <-ctx.Done():
		t.Fatal("blocked actor termination exceeded its bound")
	}
	select {
	case waitErr := <-observation.returned:
		require.ErrorIs(t, waitErr, context.Canceled, "blocked poll did not leave through scheduler cancellation")
	case <-ctx.Done():
		t.Fatal("blocked socket poll did not observe cancellation")
	}
	blocked.assertClean(t)

	// A late frame completion may be accepted by the local TCP stack before it
	// notices closure, but it must never produce a reply or revive this PID.
	_, _ = peer.Write(bytes.Repeat([]byte{0xE5}, concurrentTCPFrame/2))
	var reply [concurrentTCPFrame]byte
	_, readErr := io.ReadFull(peer, reply[:])
	require.Error(t, readErr, "late peer write produced a reply from a terminated actor")
	require.ErrorIs(t, cluster.scheduler.Send(&relayapi.Package{Target: blockedPID}), process.ErrProcessNotFound)
	require.Equal(t, uint32(1), cluster.lifecycle.completionCount(blockedPID), "late peer write revived the terminated actor")

	freshPID := cluster.SpawnActor(t, "concurrent-tcp-fresh")
	fresh := awaitConcurrentTCPInstance(ctx, t, factory.instances)
	freshDone := cluster.lifecycle.registerWait(freshPID)
	freshAddress := awaitConcurrentTCPAddress(ctx, t, network.address)
	_, err = exchangeConcurrentTCP(ctx, freshAddress, 0, 1)
	require.NoError(t, err, "fresh actor did not echo a frame")
	for client := 1; client < concurrentTCPClients; client++ {
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp4", freshAddress)
		require.NoError(t, err)
		require.NoError(t, conn.Close())
	}
	select {
	case result := <-freshDone:
		require.NoError(t, result.Error)
		value, ok := result.Value.Data().(map[string]any)
		require.True(t, ok)
		require.Equal(t, "frames:1", fmt.Sprint(value["ok"]))
	case <-ctx.Done():
		t.Fatal("fresh actor did not complete after blocked actor cancellation")
	}
	fresh.assertClean(t)
}
