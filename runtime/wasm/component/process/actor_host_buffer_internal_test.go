package process

import (
	"context"
	"net"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/runtime/wasm"
	component "github.com/wippyai/runtime/runtime/wasm/component"
	engine "github.com/wippyai/runtime/runtime/wasm/engine"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

func TestActorFactoryHostBufferDomains(t *testing.T) {
	code, err := os.ReadFile("../../engine/testdata/internal-validation/two-core.wasm")
	require.NoError(t, err)
	for _, guestPages := range []int64{5, 8} {
		cfg := &api.ProcessConfig{Method: "func1", Imports: []registry.ID{registry.ParseID("test:budget")}}
		cfg.SetOptions(api.ProcessOptions{Limits: api.ProcessLimitsConfig{MemoryBytes: guestPages * 65536, HostBufferBytes: 131072}})
		hosts := component.NewHostRegistry()
		var tables []*preview2.ResourceTable
		require.NoError(t, hosts.RegisterProfiles(component.HostProfile{Name: "test:budget", Register: func(ctx context.Context, _ *wasmrt.Runtime) error {
			if table, ok := component.GetHostRegistry(ctx).SharedResources().(*preview2.ResourceTable); ok {
				tables = append(tables, table)
			}
			return nil
		}}))
		factory := NewActorFactory(code, true, cfg, hosts, nil)
		defer factory.Close()
		// The factory's frozen ceiling cannot be changed by mutating caller config.
		cfg.SetOptions(api.ProcessOptions{Limits: api.ProcessLimitsConfig{MemoryBytes: guestPages * 65536, HostBufferBytes: 1}})
		var actors []*engine.ActorProcess
		for range 2 {
			raw, err := factory.Create()()
			require.NoError(t, err)
			actor := raw.(*engine.ActorProcess)
			actors = append(actors, actor)
			defer actor.Close()
		}
		require.Len(t, tables, 2)
		require.NotSame(t, actors[0].HostBufferBudget(), actors[1].HostBufferBudget())
		for index, actor := range actors {
			require.Same(t, actor.HostBufferBudget(), tables[index].HostBufferBudget())
			require.Equal(t, uint64(131072), actor.HostBufferBudget().Usage().Limit)
			socket := preview2.NewTCPSocketResource(0)
			left, right := net.Pipe()
			defer right.Close()
			socket.SetConn(left)
			socket.SetState(preview2.TCPStateConnected)
			_, err := tables[index].TryAdd(socket)
			require.NoError(t, err)
			_, _, err = tables[index].NewTCPDuplexStreams(socket)
			require.NoError(t, err)
			require.Equal(t, uint64(131072), actor.HostBufferBudget().Usage().Used)
			require.Zero(t, actor.MemoryBudget().Usage().Used, "host rings must not consume guest memory")
		}
		actors[0].Close()
		require.Zero(t, actors[0].HostBufferBudget().Usage().Used)
		require.Equal(t, uint64(131072), actors[1].HostBufferBudget().Usage().Used)
		actors[1].Close()
		require.Zero(t, actors[1].HostBufferBudget().Usage().Used)
	}
	cfg := &api.ProcessConfig{Method: "func1"}
	factory := NewActorFactory(code, true, cfg, component.NewHostRegistry(), nil)
	defer factory.Close()
	raw, err := factory.Create()()
	require.NoError(t, err)
	actor := raw.(*engine.ActorProcess)
	defer actor.Close()
	require.Nil(t, actor.HostBufferBudget(), "omitted setting gained a new host byte ceiling")
}
