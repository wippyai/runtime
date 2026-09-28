// SPDX-License-Identifier: MPL-2.0
package engine

import (
	"context"
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

func TestTCPGuestConfiguredStartupDeadline(t *testing.T) {
	for _, fixture := range []string{"tcp", "mqtt"} {
		t.Run(fixture, func(t *testing.T) {
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
			code, err := os.ReadFile("testdata/" + fixture + ".wasm")
			require.NoError(t, err)
			module, err := rt.LoadComponent(ctx, code)
			require.NoError(t, err)
			require.NoError(t, module.Compile(ctx))
			limits := wasmapi.LimitsConfig{MaxExecutionMS: 5000, SocketTimeoutMS: 25}
			p := NewActorProcess(NewProcess(module, "", wasmapi.WASIConfig{}, limits, nil), actor.DefaultLimits(), nil)
			p.SetSocketBudget(table.SocketBudget())
			defer p.Close()
			require.NoError(t, p.Init(ctx, "run", nil))
			waitCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			network := &startupDeadlineNetwork{observed: make(chan error, 1)}
			handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
			socketservice.NewDispatcher(network).RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) { handlers[id] = h })
			var events []process.Event
			var out process.StepOutput
			started := false
			for steps := 0; steps < 16; steps++ {
				out.Reset()
				require.NoError(t, p.Step(events, &out))
				if out.IsDone() {
					require.True(t, started)
					result, ok := out.Result().Data().(map[string]any)
					require.True(t, ok)
					require.Contains(t, result["err"], `name: "timeout"`)
					require.Zero(t, table.SocketBudget().Used())
					select {
					case err := <-network.observed:
						require.ErrorIs(t, err, context.DeadlineExceeded)
					case <-waitCtx.Done():
						t.Fatal("network job did not terminate on startup deadline")
					}
					require.NoError(t, waitCtx.Err(), "operation timeout must not cancel the actor")
					return
				}
				require.Equal(t, 1, out.Count())
				y := out.Yields()[0]
				switch cmd := y.Cmd.(type) {
				case *socketapi.StartConnectCmd:
					require.Equal(t, "tcp", fixture)
					require.Equal(t, 25*time.Millisecond, cmd.Timeout)
					started = true
				case *socketapi.StartListenCmd:
					require.Equal(t, "mqtt", fixture)
					require.Equal(t, 25*time.Millisecond, cmd.Timeout)
					started = true
				}
				h := handlers[y.Cmd.CmdID()]
				require.NotNil(t, h)
				receiver := &mqttGuestReceiver{done: make(chan process.Event, 1)}
				require.NoError(t, h.Handle(waitCtx, y.Cmd, y.Tag, receiver))
				select {
				case event := <-receiver.done:
					require.NoError(t, receiver.err)
					events = []process.Event{event}
				case <-waitCtx.Done():
					t.Fatal("guest did not finish its bounded startup")
				}
			}
			t.Fatal("guest exceeded bounded startup steps")
		})
	}
}

type startupDeadlineNetwork struct {
	netapi.Service
	observed chan error
}

func (n *startupDeadlineNetwork) DialContext(ctx context.Context, _, _ string) (net.Conn, error) {
	<-ctx.Done()
	n.observed <- ctx.Err()
	return nil, ctx.Err()
}

func (n *startupDeadlineNetwork) Listen(ctx context.Context, _, _ string) (net.Listener, error) {
	<-ctx.Done()
	n.observed <- ctx.Err()
	return nil, ctx.Err()
}
