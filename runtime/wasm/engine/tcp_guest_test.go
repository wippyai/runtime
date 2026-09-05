// SPDX-License-Identifier: MPL-2.0
package engine

import (
	"context"
	"io"
	"net"
	"os"
	"syscall"
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

// Exercise the standard WIT imports and real compiled guest through process
// suspension/resumption. Controlled connections isolate canonical ABI from OS routing.
func TestTCPGuestCanonicalRoundTrip(t *testing.T) {
	t.Run("ping-pong", func(t *testing.T) { testTCPGuestCanonicalRoundTrip(t, false) })
	t.Run("connection-refused", func(t *testing.T) { testTCPGuestCanonicalRoundTrip(t, true) })
}

func testTCPGuestCanonicalRoundTrip(t *testing.T, refused bool) {
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
	code, err := os.ReadFile("testdata/tcp.wasm")
	require.NoError(t, err)
	module, err := rt.LoadComponent(ctx, code)
	require.NoError(t, err)
	require.NoError(t, module.Compile(ctx))
	execution := NewActorProcess(NewProcess(module, "", wasmapi.WASIConfig{}, wasmapi.LimitsConfig{MaxExecutionMS: 5000}, nil), actor.DefaultLimits(), nil)
	execution.SetSocketBudget(table.SocketBudget())
	defer execution.Close()
	require.NoError(t, execution.Init(ctx, "run", nil))
	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var output process.StepOutput
	var events []process.Event
	connected := false
	peerDone := make(chan error, 1)
	for steps := 0; steps < 16; steps++ {
		output.Reset()
		require.NoError(t, execution.Step(events, &output))
		if output.IsDone() {
			require.True(t, connected, "guest never connected")
			require.Zero(t, table.SocketBudget().Used(), "guest socket drop did not return quota")
			if refused {
				value, ok := output.Result().Data().(map[string]any)
				require.True(t, ok)
				require.Len(t, value, 1)
				require.Contains(t, value["err"], `name: "connection-refused"`)
				return
			}
			require.Equal(t, map[string]any{"ok": "pong"}, output.Result().Data())
			select {
			case err := <-peerDone:
				require.NoError(t, err)
			case <-waitCtx.Done():
				t.Fatal("peer did not finish")
			}
			return
		}
		require.Equal(t, 1, output.Count(), "guest did not yield one network operation")
		yielded := output.Yields()[0]
		var result any
		switch command := yielded.Cmd.(type) {
		case *socketapi.ConnectCmd:
			require.False(t, connected)
			require.Equal(t, "127.0.0.1:8099", command.Address)
			connected = true
			if refused {
				result = &socketapi.ConnectResult{Err: syscall.ECONNREFUSED}
				break
			}
			left, right := net.Pipe()
			t.Cleanup(func() { _ = left.Close(); _ = right.Close() })
			_ = right.SetDeadline(time.Now().Add(5 * time.Second))
			go func() {
				data := make([]byte, 4)
				_, err := io.ReadFull(right, data)
				if err == nil && string(data) != "ping" {
					err = io.ErrUnexpectedEOF
				}
				if err == nil {
					_, err = right.Write([]byte("pong"))
				}
				peerDone <- err
			}()
			result = &socketapi.ConnectResult{Conn: left}
		case *socketapi.PollWaitCmd:
			result, err = command.Wait(waitCtx)
			require.NoError(t, err)
		case *socketapi.StreamWaitCmd:
			result = command.Run(waitCtx)
		default:
			t.Fatalf("unexpected guest yield %T", command)
		}
		events = []process.Event{{Type: process.EventYieldComplete, Tag: yielded.Tag, Data: result}}
	}
	t.Fatal("guest exceeded bounded round-trip steps")
}
