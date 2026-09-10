// SPDX-License-Identifier: MPL-2.0
package engine

import (
	"context"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/process"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	socketapi "github.com/wippyai/runtime/api/socket"
	"github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/actor"
	iohost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/io"
	pollhost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/poll"
	sockethost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/sockets"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

// The first dial cannot complete until the guest starts the second and polls.
// This would deadlock under the former start-connect-until-complete behavior.
func TestTCPGuestStartsTwoConnectionsBeforeCompletion(t *testing.T) {
	t.Run("complete", func(t *testing.T) { testTCPGuestParallelConnections(t, false) })
	t.Run("drop-pending", func(t *testing.T) { testTCPGuestParallelConnections(t, true) })
}

func testTCPGuestParallelConnections(t *testing.T, dropPending bool) {
	t.Helper()
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
	code, err := os.ReadFile("testdata/parallel_tcp.wasm")
	require.NoError(t, err)
	module, err := rt.LoadComponent(ctx, code)
	require.NoError(t, err)
	require.NoError(t, module.Compile(ctx))
	p := NewActorProcess(NewProcess(module, "", wasmapi.WASIConfig{}, wasmapi.LimitsConfig{MaxExecutionMS: 5000}, nil), actor.DefaultLimits(), nil)
	p.SetSocketBudget(table.SocketBudget())
	defer p.Close()
	require.NoError(t, p.Init(ctx, "run", nil))
	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	release := make(chan struct{})
	var operations []*socketapi.PendingOperation
	var peers []net.Conn
	var events []process.Event
	var out process.StepOutput
	polled := false
	for steps := 0; steps < 32; steps++ {
		out.Reset()
		require.NoError(t, p.Step(events, &out))
		if out.IsDone() {
			require.Len(t, operations, 2)
			require.True(t, polled)
			require.Equal(t, map[string]any{"ok": "connected:2"}, out.Result().Data())
			require.Zero(t, table.SocketBudget().Used())
			return
		}
		require.Equal(t, 1, out.Count())
		y := out.Yields()[0]
		var result any
		switch cmd := y.Cmd.(type) {
		case *socketapi.StartConnectCmd:
			require.Less(t, len(operations), 2)
			require.Equal(t, []string{"127.0.0.1:8099", "127.0.0.1:8100"}[len(operations)], cmd.Address)
			if len(operations) == 1 {
				require.False(t, operations[0].Ready(), "first dial completed before second start")
			}
			opCtx, started := cmd.Operation.Start(waitCtx)
			require.True(t, started)
			operations = append(operations, cmd.Operation)
			conn, peer := net.Pipe()
			peers = append(peers, peer)
			t.Cleanup(func() { conn.Close(); peer.Close() })
			go func() {
				select {
				case <-release:
					cmd.Operation.Complete(conn, nil)
				case <-opCtx.Done():
					cmd.Operation.Complete(conn, opCtx.Err())
				}
			}()
			result = &socketapi.StartResult{}
		case *socketapi.PollWaitCmd:
			require.Len(t, operations, 2, "guest waited before starting its second dial")
			if dropPending {
				// Scope close must cancel both dials and join disposal of their late
				// connections before returning either socket reservation.
				table.Close()
				require.Zero(t, table.SocketBudget().Used())
				for _, peer := range peers {
					_ = peer.SetReadDeadline(time.Now().Add(time.Second))
					var data [1]byte
					_, readErr := peer.Read(data[:])
					require.ErrorIs(t, readErr, io.EOF)
				}
				return
			}
			if !polled {
				polled = true
				close(release)
			}
			result, err = cmd.Wait(waitCtx)
			require.NoError(t, err)
		default:
			t.Fatalf("unexpected parallel-connect yield %T", cmd)
		}
		events = []process.Event{{Type: process.EventYieldComplete, Tag: y.Tag, Data: result}}
	}
	t.Fatal("parallel-connect guest exceeded bounded steps")
}
