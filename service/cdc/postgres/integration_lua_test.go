//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"go.uber.org/zap"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	apiruntime "github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/security"
	cdcapi "github.com/wippyai/runtime/api/service/cdc"
	apisup "github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/runtime/lua/engine"
	luapayload "github.com/wippyai/runtime/runtime/lua/engine/payload"
	cdcmod "github.com/wippyai/runtime/runtime/lua/modules/cdc"
	"github.com/wippyai/runtime/system/eventbus"
	systempayload "github.com/wippyai/runtime/system/payload"
	sysrelay "github.com/wippyai/runtime/system/relay"
	"github.com/wippyai/runtime/system/scheduler"
	"github.com/wippyai/runtime/system/scheduler/pool/inline"
	syssup "github.com/wippyai/runtime/system/supervisor"
)

const luaSlot = "wippy_cdc_lua"

type signalingSourceStreamer struct {
	inner cdcapi.SourceStreamer
	ready chan struct{}
	once  sync.Once
}

func newSignalingSourceStreamer(inner cdcapi.SourceStreamer) *signalingSourceStreamer {
	return &signalingSourceStreamer{
		inner: inner,
		ready: make(chan struct{}),
	}
}

func (s *signalingSourceStreamer) Stream(ctx context.Context, source string, opts cdcapi.StreamOptions) (cdcapi.ChangeStream, cdcapi.SourceInfo, error) {
	stream, info, err := s.inner.Stream(ctx, source, opts)
	if err == nil {
		s.once.Do(func() { close(s.ready) })
	}
	return stream, info, err
}

func TestLuaSeesRealRunningSourceAndItsChanges(t *testing.T) {
	repl, admin := dsns(t)
	db, err := sql.Open("postgres", admin)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	setupSchema(t, db)
	dropNamedSlot(t, repl, luaSlot)
	var src *Source
	defer func() {
		if src != nil {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = src.Stop(stopCtx)
			stopCancel()
		}
		dropNamedSlot(t, repl, luaSlot)
	}()
	_, err = db.ExecContext(context.Background(), `DELETE FROM accounts`)
	require.NoError(t, err)

	bus := eventbus.NewBus()
	sup := syssup.NewSupervisor(bus, zap.NewNop())
	supCtx, supCancel := context.WithCancel(context.Background())
	defer supCancel()
	require.NoError(t, sup.Start(supCtx))
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, sup.StopContext(stopCtx))
	}()

	manager := &Manager{
		bus:     bus,
		log:     zap.NewNop(),
		sources: map[registry.ID]*Source{},
		infos:   map[registry.ID]cdcapi.SourceInfo{},
	}

	entryID := registry.NewID("test", "cdc-lua-e2e")
	cfg := &cdcapi.Config{
		Host: "localhost", Port: 5432, Username: "u", Password: "p",
		Database: "d", SlotName: luaSlot, Publication: "wippy_cdc_pub",
		Streaming: true,
	}
	src = NewSource(SourceOptions{
		ReplDSN: repl, AdminDSN: admin, Slot: luaSlot, Publication: "wippy_cdc_pub",
		Name: entryID.String(), Streaming: true, StandbyInterval: 200 * time.Millisecond, StatusInterval: time.Hour,
	})
	manager.sources[entryID] = src
	manager.storeInfo(registry.Entry{ID: entryID, Kind: cdcapi.Postgres}, cfg)

	lc := apisup.LifecycleConfig{AutoStart: true}
	lc.InitDefaults()
	bus.Send(supCtx, event.Event{System: registry.System, Kind: registry.TxBegin, Path: "tx"})
	bus.Send(supCtx, event.Event{
		System: apisup.System,
		Kind:   apisup.ServiceRegister,
		Path:   entryID.String(),
		Data:   &apisup.Entry{Service: src, Config: lc},
	})
	bus.Send(supCtx, event.Event{System: registry.System, Kind: registry.TxCommit, Path: "tx"})

	require.Eventually(t, func() bool {
		var n int
		_ = db.QueryRowContext(supCtx, `SELECT count(*) FROM pg_replication_slots WHERE slot_name=$1`, luaSlot).Scan(&n)
		return n == 1
	}, 15*time.Second, 100*time.Millisecond, "supervisor must auto-start the registered source")

	streamer := newSignalingSourceStreamer(manager)
	root := security.SetStrictMode(ctxapi.NewRootContext(), false)
	transcoder := systempayload.NewTranscoder()
	luapayload.Register(transcoder)
	payload.WithTranscoder(root, transcoder)
	root = cdcapi.WithSourceInspector(root, manager)
	root = cdcapi.WithSourceStreamer(root, streamer)

	node := sysrelay.NewNode("cdc-lua-test-node")
	root = relay.WithNode(root, node)

	runCtx, runCancel := context.WithTimeout(root, 30*time.Second)
	defer runCancel()

	dispReg := scheduler.NewRegistry()
	cdcDisp := NewDispatcher(WithWorkers(1))
	require.NoError(t, cdcDisp.Start(runCtx))
	defer func() { require.NoError(t, cdcDisp.Stop(context.Background())) }()
	cdcDisp.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		dispReg.Register(id, h)
	})

	const expectedEmail = "lua-e2e@wippy.ai"
	const hostID = "test.cdc.lua"
	factory := func() (process.Process, error) {
		cfg := engine.FactoryConfig{
			ScriptName: "cdc_lua_e2e",
			Script: `
local cdc = require("cdc")

local function main()
    local rows, err = cdc.list_sources()
    if err ~= nil then return nil, "list_sources error: " .. tostring(err) end
    if #rows ~= 1 then return nil, "expected 1 source, got " .. tostring(#rows) end

    local r = rows[1]
    if r.slot ~= "` + luaSlot + `" then return nil, "wrong slot: " .. tostring(r.slot) end
    if r.publication ~= "wippy_cdc_pub" then return nil, "wrong publication: " .. tostring(r.publication) end
    if r.streaming ~= true then return nil, "expected streaming=true" end

    local by_slot, source_err = cdc.source("` + entryID.String() + `")
    if source_err ~= nil then return nil, "source error: " .. tostring(source_err) end
    if by_slot == nil then return nil, "lookup by registry ID returned nil" end

    local stream, stream_err = cdc.stream("` + entryID.String() + `", {
        tables = {"public.accounts"},
        ops = {"insert"},
        buffer = 8,
    })
    if stream_err ~= nil then return nil, "stream error: " .. tostring(stream_err) end

    local ch = stream:channel()
    local change, ok = ch:receive()
    local released, release_err = stream:release()
    if released ~= true or release_err ~= nil then
        return nil, "release error: " .. tostring(release_err)
    end
    if ok ~= true then return nil, "stream closed before change" end
    if change.op ~= "insert" then return nil, "wrong op: " .. tostring(change.op) end
    if change.source ~= "test:cdc-lua-e2e" then return nil, "wrong source: " .. tostring(change.source) end
    if change.relation ~= "public.accounts" then return nil, "wrong relation: " .. tostring(change.relation) end
    if change.after == nil then return nil, "missing after table" end
    if change.after.email ~= "` + expectedEmail + `" then
        return nil, "wrong email: " .. tostring(change.after.email)
    end

    return change.after.email
end

return { main = main }
`,
			ModuleBinders: append(engine.CoreBinders(), func(l *lua.LState) error {
				engine.LoadModuleDef(l, cdcmod.Module)
				return nil
			}),
		}
		return engine.NewFactory(cfg)()
	}

	pool, err := inline.New(factory, dispReg)
	require.NoError(t, err)
	defer pool.Stop()
	require.NoError(t, node.RegisterHost(hostID, pool))

	frameCtx, frame := ctxapi.OpenFrameContext(runCtx)
	defer ctxapi.ReleaseFrameContext(frame)
	testPID := pid.PID{Host: hostID, UniqID: "cdc-lua-e2e"}
	testPID = testPID.Precomputed()
	require.NoError(t, apiruntime.SetFramePID(frameCtx, testPID))

	resultCh := make(chan *apiruntime.Result, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := pool.Call(frameCtx, "main", nil)
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- result
	}()

	select {
	case <-streamer.ready:
	case result := <-resultCh:
		t.Fatalf("Lua exited before subscribing: error=%v value=%v", result.Error, result.Value)
	case err := <-errCh:
		require.NoError(t, err)
	case <-runCtx.Done():
		t.Fatal("timed out waiting for Lua CDC stream subscription")
	}

	res, err := db.ExecContext(runCtx, `INSERT INTO accounts (email, balance) VALUES ($1, $2)`,
		expectedEmail, 42)
	require.NoError(t, err)
	rows, _ := res.RowsAffected()
	require.Equal(t, int64(1), rows)

	var result *apiruntime.Result
	select {
	case result = <-resultCh:
	case err := <-errCh:
		require.NoError(t, err)
	case <-runCtx.Done():
		t.Fatal("timed out waiting for Lua to receive CDC change")
	}
	require.NotNil(t, result)
	require.NoError(t, result.Error)
	require.NotNil(t, result.Value)
	got, ok := result.Value.Data().(lua.LString)
	require.True(t, ok, "expected Lua string result, got %T", result.Value.Data())
	require.Equal(t, expectedEmail, string(got))

	require.Eventually(t, func() bool {
		src.subMu.RLock()
		defer src.subMu.RUnlock()
		return len(src.subs) == 0
	}, 2*time.Second, 10*time.Millisecond, "Lua stream release must stop the source subscription")
}
