package engine

import (
	"context"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/dispatcher"
	netapi "github.com/wippyai/runtime/api/net"
	processapi "github.com/wippyai/runtime/api/process"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	socketapi "github.com/wippyai/runtime/api/socket"
	actorhost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/actor"
	clihost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/cli"
	iohost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/io"
	pollhost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/poll"
	sockethost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/sockets"
	stdiohost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/stdio"
	socketservice "github.com/wippyai/runtime/service/socket"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

type observingActorHost struct {
	*actorhost.Host
	subscribed atomic.Bool
}

func (h *observingActorHost) Register() map[string]any {
	functions := h.Host.Register()
	functions["subscribe"] = func(ctx context.Context) uint32 { h.subscribed.Store(true); return h.Subscribe(ctx) }
	return functions
}

type originalMixedNetwork struct {
	netapi.Service
	address chan string
}

func (n *originalMixedNetwork) Listen(ctx context.Context, network, address string) (net.Listener, error) {
	l, e := (&net.ListenConfig{}).Listen(ctx, network, address)
	if e == nil {
		n.address <- l.Addr().String()
	}
	return l, e
}

func TestOriginalMixedGuestThroughScheduler(t *testing.T) {
	for _, mode := range []string{"mailbox", "socket", "terminate"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			rt, err := wasmrt.NewWithConfig(ctx, &wasmrt.Config{CloseOnContextDone: true})
			require.NoError(t, err)
			table := preview2.NewResourceTableWithLimits(64, 4)
			// Cluster joins the actor before table/runtime destruction.
			t.Cleanup(func() { require.NoError(t, table.Close()); require.NoError(t, rt.Close(context.Background())) })
			actorObserver := &observingActorHost{Host: actorhost.NewHost(table)}
			for _, h := range []wasmrt.Host{actorObserver, pollhost.NewHost(table), iohost.NewErrorHost(table), iohost.NewStreamsHost(table), clihost.NewEnvironmentHost(), clihost.NewExitHost(), stdiohost.NewHost(table), stdiohost.NewStdoutHost(table), stdiohost.NewStderrHost(table), stdiohost.NewTerminalStdinHost(), stdiohost.NewTerminalStdoutHost(), stdiohost.NewTerminalStderrHost(), sockethost.NewTCPCreateSocketHost(table), sockethost.NewTCPHost(table), sockethost.NewInstanceNetworkHost(table), sockethost.NewNetworkHost(table)} {
				require.NoError(t, rt.RegisterHost(h))
			}
			code, err := os.ReadFile("testdata/internal-validation/mixed.wasm")
			require.NoError(t, err)
			mod, err := rt.LoadComponent(ctx, code)
			require.NoError(t, err)
			require.NoError(t, mod.Compile(ctx))
			network := &originalMixedNetwork{address: make(chan string, 1)}
			waiting := make(chan struct{})
			var once sync.Once
			cluster := newHarnessClusterWithCommands(t, 1, func() (processapi.Process, error) {
				return NewActorProcess(NewProcess(mod, "", wasmapi.WASIConfig{}, wasmapi.LimitsConfig{}, nil), actorhost.DefaultLimits(), nil), nil
			}, func(register func(dispatcher.CommandID, dispatcher.Handler)) {
				socketservice.NewDispatcher(network).RegisterAll(func(id dispatcher.CommandID, next dispatcher.Handler) {
					if id == socketapi.SocketPollWait {
						register(id, dispatcher.HandlerFunc(func(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
							if actorObserver.subscribed.Load() {
								once.Do(func() { close(waiting) })
							}
							return next.Handle(ctx, cmd, tag, receiver)
						}))
					} else {
						register(id, next)
					}
				})
			})
			target := cluster.SpawnActor(t, "original-mixed")
			done := cluster.lifecycle.registerWait(target)
			var address string
			select {
			case address = <-network.address:
			case out := <-done:
				t.Fatalf("exited before listener: %+v", out)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			select {
			case <-waiting:
			case out := <-done:
				t.Fatalf("exited before mixed poll: %+v", out)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			switch mode {
			case "mailbox":
				c := cluster.NewClient("mixed-sender")
				defer c.Close()
				require.NoError(t, c.SendOnly(target, "stop"))
			case "socket":
				conn, err := (&net.Dialer{}).DialContext(ctx, "tcp4", address)
				require.NoError(t, err)
				require.NoError(t, conn.Close())
			case "terminate":
				require.NoError(t, cluster.host.Terminate(ctx, target))
			}
			select {
			case out := <-done:
				require.NotNil(t, out)
				if mode == "terminate" {
					require.Error(t, out.Error)
				} else {
					require.NoError(t, out.Error)
					require.NotNil(t, out.Value)
					got := out.Value.Data().(map[string]any)["ok"]
					require.Equal(t, mode, got)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			require.Equal(t, uint32(1), cluster.lifecycle.completionCount(target))
			require.NoError(t, cluster.host.Stop(ctx))
			require.NoError(t, table.Close())
			require.Zero(t, table.SocketBudget().Used(), "table close retained socket reservations")
		})
	}
}
