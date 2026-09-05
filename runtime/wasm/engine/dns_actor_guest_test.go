// SPDX-License-Identifier: MPL-2.0
package engine

import (
	"context"
	"fmt"
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
	pollhost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/poll"
	sockethost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/sockets"
	socketservice "github.com/wippyai/runtime/service/socket"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

type dnsActorNetwork struct {
	netapi.Service
	release chan struct{}
	names   chan string
	active  atomic.Int32
}

func (n *dnsActorNetwork) LookupHost(ctx context.Context, name string) ([]string, error) {
	n.active.Add(1)
	defer n.active.Add(-1)
	select {
	case n.names <- name:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	switch name {
	case "xn--bcher-kva.example", "second.example":
		select {
		case <-n.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if name == "second.example" {
			return []string{"203.0.113.8"}, nil
		}
		return []string{"192.0.2.1", "2001:db8::1", "::ffff:198.51.100.7"}, nil
	case "cancel.example", "timeout.example":
		<-ctx.Done()
		return nil, ctx.Err()
	default:
		return nil, fmt.Errorf("unexpected resolver request %q", name)
	}
}

func startDNSActorGuest(t *testing.T, entry string, timeoutMS int) (context.CancelFunc, <-chan struct{}, <-chan concurrentTCPResult, *dnsActorNetwork) {
	t.Helper()
	root := ctxapi.NewRootContext()
	securityapi.SetStrictMode(root, false)
	base, frame := ctxapi.OpenFrameContext(root)
	ctx, cancel := context.WithTimeout(base, 10*time.Second)
	rt, err := wasmrt.NewWithConfig(ctx, &wasmrt.Config{CloseOnContextDone: true})
	require.NoError(t, err)
	table := preview2.NewResourceTableWithLimits(128, 4)
	network := &dnsActorNetwork{release: make(chan struct{}), names: make(chan string, 8)}
	var process *ActorProcess
	var stopped chan struct{}
	t.Cleanup(func() {
		cancel()
		if stopped != nil {
			select {
			case <-stopped:
			case <-time.After(5 * time.Second):
				t.Error("DNS actor driver did not stop")
				return
			}
		}
		if process != nil {
			process.Close()
		}
		require.NoError(t, table.Close())
		require.Zero(t, network.active.Load(), "lookup outlived resource cleanup")
		require.NoError(t, rt.Close(context.Background()))
		require.NoError(t, frame.Close())
	})
	for _, host := range []wasmrt.Host{sockethost.NewIPNameLookupHost(table), sockethost.NewInstanceNetworkHost(table), sockethost.NewNetworkHost(table), pollhost.NewHost(table)} {
		require.NoError(t, rt.RegisterHost(host))
	}
	code, err := os.ReadFile("testdata/dns_actor.wasm")
	require.NoError(t, err)
	module, err := rt.LoadComponent(ctx, code)
	require.NoError(t, err)
	require.NoError(t, module.Compile(ctx))
	process = NewActorProcess(NewProcess(module, "", wasmapi.WASIConfig{}, wasmapi.LimitsConfig{MaxExecutionMS: 10000, SocketTimeoutMS: timeoutMS}, nil), actor.DefaultLimits(), nil)
	process.SetSocketBudget(table.SocketBudget())
	require.NoError(t, process.Init(ctx, entry, nil))
	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	socketservice.NewDispatcher(network).RegisterAll(func(id dispatcher.CommandID, handler dispatcher.Handler) { handlers[id] = handler })
	polled := make(chan struct{})
	var once sync.Once
	poll := handlers[socketapi.SocketPollWait]
	handlers[socketapi.SocketPollWait] = dispatcher.HandlerFunc(func(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
		once.Do(func() { close(polled); close(network.release) })
		return poll.Handle(ctx, cmd, tag, receiver)
	})
	result := make(chan concurrentTCPResult, 1)
	stopped = make(chan struct{})
	go func() { defer close(stopped); result <- driveConcurrentTCPGuest(ctx, process, table, handlers) }()
	return cancel, polled, result, network
}

func TestDNSActorGuestParallelLookups(t *testing.T) {
	_, _, result, network := startDNSActorGuest(t, "run", 5000)
	select {
	case outcome := <-result:
		require.NoError(t, outcome.err)
		require.Equal(t, "dns:4", outcome.value)
		require.Positive(t, outcome.waits)
	case <-time.After(8 * time.Second):
		t.Fatal("DNS guest did not finish")
	}
	names := make([]string, 0, 2)
	for len(network.names) > 0 {
		names = append(names, <-network.names)
	}
	require.ElementsMatch(t, []string{"xn--bcher-kva.example", "second.example"}, names, "literals and invalid names must not reach provider")
}

func TestDNSActorGuestDeadline(t *testing.T) {
	cancel, _, result, _ := startDNSActorGuest(t, "timeout", 50)
	defer cancel()
	select {
	case outcome := <-result:
		require.NoError(t, outcome.err)
		require.Equal(t, "timeout", outcome.value)
	case <-time.After(5 * time.Second):
		t.Fatal("DNS deadline did not reach guest")
	}
}

func TestDNSActorGuestCancellationJoinsLookup(t *testing.T) {
	cancel, polled, result, network := startDNSActorGuest(t, "wait", 5000)
	select {
	case <-polled:
	case outcome := <-result:
		t.Fatalf("guest exited before polling: %v %s", outcome.err, outcome.value)
	case <-time.After(5 * time.Second):
		t.Fatal("guest did not poll")
	}
	require.Eventually(t, func() bool { return network.active.Load() == 1 }, time.Second, time.Millisecond)
	cancel()
	select {
	case outcome := <-result:
		require.Error(t, outcome.err)
	case <-time.After(5 * time.Second):
		t.Fatal("canceled DNS guest did not stop")
	}
	// Cleanup closes the process/resource table and asserts all resolver jobs joined.
}
