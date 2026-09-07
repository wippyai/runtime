package engine

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/dispatcher"
	processapi "github.com/wippyai/runtime/api/process"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	socketapi "github.com/wippyai/runtime/api/socket"
	actorhost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/actor"
	pollhost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/poll"
	socketservice "github.com/wippyai/runtime/service/socket"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

func TestMailboxPollThroughActorScheduler(t *testing.T) {
	for _, terminate := range []bool{false, true} {
		t.Run(map[bool]string{false: "delivery", true: "cancellation"}[terminate], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			rt, err := wasmrt.NewWithConfig(ctx, &wasmrt.Config{CloseOnContextDone: true})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, rt.Close(context.Background())) })
			table := preview2.NewResourceTableWithLimits(16, 1)
			t.Cleanup(func() { require.NoError(t, table.Close()) })
			require.NoError(t, rt.RegisterHost(actorhost.NewHost(table)))
			require.NoError(t, rt.RegisterHost(pollhost.NewHost(table)))
			code, err := os.ReadFile("testdata/internal-validation/actor_direct_poll.wasm")
			require.NoError(t, err)
			mod, err := rt.LoadComponent(ctx, code)
			require.NoError(t, err)
			require.NoError(t, mod.Compile(ctx))
			waiting := make(chan struct{})
			var once sync.Once
			cluster := newHarnessClusterWithCommands(t, 1, func() (processapi.Process, error) {
				return NewActorProcess(NewProcess(mod, "", wasmapi.WASIConfig{}, wasmapi.LimitsConfig{}, nil), actorhost.DefaultLimits(), nil), nil
			}, func(register func(dispatcher.CommandID, dispatcher.Handler)) {
				socketservice.NewDispatcher(nil).RegisterAll(func(id dispatcher.CommandID, next dispatcher.Handler) {
					if id == socketapi.SocketPollWait {
						register(id, dispatcher.HandlerFunc(func(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
							once.Do(func() { close(waiting) })
							return next.Handle(ctx, cmd, tag, receiver)
						}))
					} else {
						register(id, next)
					}
				})
			})
			target := cluster.SpawnActor(t, "mailbox-poll")
			done := cluster.lifecycle.registerWait(target)
			select {
			case <-waiting:
			case outcome := <-done:
				t.Fatalf("exited before poll: %+v", outcome)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if terminate {
				require.NoError(t, cluster.host.Terminate(ctx, target))
			} else {
				client := cluster.NewClient("mailbox-poll-sender")
				defer client.Close()
				require.NoError(t, client.SendOnly(target, "wake"))
			}
			select {
			case outcome := <-done:
				require.NotNil(t, outcome)
				if terminate {
					require.Error(t, outcome.Error)
				} else {
					require.NoError(t, outcome.Error)
					require.NotNil(t, outcome.Value)
					require.Equal(t, map[string]any{"ok": nil}, outcome.Value.Data())
					_, exists := table.Get(1)
					require.False(t, exists, "guest must drop mailbox pollable")
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			require.Equal(t, uint32(1), cluster.lifecycle.completionCount(target))
			require.NoError(t, cluster.host.Stop(ctx))
		})
	}
}
