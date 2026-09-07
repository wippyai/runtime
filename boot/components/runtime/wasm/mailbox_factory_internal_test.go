package wasm

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
	processapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	component "github.com/wippyai/runtime/runtime/wasm/component"
	actorcomponent "github.com/wippyai/runtime/runtime/wasm/component/process"
	engine "github.com/wippyai/runtime/runtime/wasm/engine"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
	"go.uber.org/zap"
)

func TestActorFactoryMailboxPollDefaultProfiles(t *testing.T) {
	code, err := os.ReadFile("../../../../runtime/wasm/engine/testdata/internal-validation/actor_direct_poll.wasm")
	require.NoError(t, err)
	hosts := component.NewHostRegistry()
	require.NoError(t, hosts.RegisterProfiles(DefaultHostProfiles(zap.NewNop(), nil)...))
	var tables []*preview2.ResourceTable
	require.NoError(t, hosts.RegisterProfiles(component.HostProfile{Name: "test:capture", Register: func(ctx context.Context, _ *wasmrt.Runtime) error {
		table, ok := component.GetHostRegistry(ctx).SharedResources().(*preview2.ResourceTable)
		if ok {
			tables = append(tables, table)
		}
		return nil
	}}))
	cfg := &wasmapi.ProcessConfig{Method: "run", Imports: []registry.ID{registry.ParseID("wippy:actor"), registry.ParseID(component.HostProfileWASIPoll), registry.ParseID("test:capture")}}
	factory := actorcomponent.NewActorFactory(code, true, cfg, hosts, nil)
	defer factory.Close()
	var actors []*engine.ActorProcess
	for range 2 {
		raw, err := factory.Create()()
		require.NoError(t, err)
		p := raw.(*engine.ActorProcess)
		actors = append(actors, p)
		defer p.Close()
	}
	// Pin profile registration order is unspecified. Identify actor-owned tables
	// by the exact socket ledger installed by ActorFactory, not callback order.
	actorTables := make([]*preview2.ResourceTable, 2)
	for index, p := range actors {
		for _, table := range tables {
			if table.SocketBudget() == p.SocketBudget() {
				require.Nil(t, actorTables[index])
				actorTables[index] = table
			}
		}
		require.NotNil(t, actorTables[index])
	}
	require.NotSame(t, actorTables[0], actorTables[1])
	tables = actorTables
	for index, p := range actors {
		ctx, frame := ctxapi.OpenFrameContext(ctxapi.NewRootContext())
		defer frame.Close()
		require.NoError(t, p.Init(ctx, "run", nil))
		from := pid.PID{Node: "local", Host: "test", UniqID: "sender"}
		to := pid.PID{Node: "local", Host: "test", UniqID: "target"}
		event, err := p.EventAdmission().AdmitEvent(processapi.Event{Type: processapi.EventMessage, Data: relay.NewPackage(from, to, "wake")})
		require.NoError(t, err)
		var out processapi.StepOutput
		require.NoError(t, p.Step([]processapi.Event{event}, &out))
		require.True(t, out.IsDone())
		require.Equal(t, map[string]any{"ok": nil}, out.Result().Data())
		_, exists := tables[index].Get(1)
		require.False(t, exists, "guest failed to drop pollable from its factory table")
		p.Close()
		require.Zero(t, p.MemoryBudget().Usage().Used)
	}
}
