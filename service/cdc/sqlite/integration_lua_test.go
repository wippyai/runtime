// SPDX-License-Identifier: MPL-2.0

//go:build integration && sqlite_preupdate_hook

package sqlite

import (
	"context"
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
	pgcdc "github.com/wippyai/runtime/service/cdc/postgres"
	"github.com/wippyai/runtime/system/eventbus"
	systempayload "github.com/wippyai/runtime/system/payload"
	sysrelay "github.com/wippyai/runtime/system/relay"
	"github.com/wippyai/runtime/system/scheduler"
	"github.com/wippyai/runtime/system/scheduler/pool/inline"
	syssup "github.com/wippyai/runtime/system/supervisor"
)

type signalingStreamer struct {
	inner cdcapi.SourceStreamer
	ready chan struct{}
	once  sync.Once
}

func (s *signalingStreamer) Stream(ctx context.Context, source string, opts cdcapi.StreamOptions) (cdcapi.ChangeStream, cdcapi.SourceInfo, error) {
	stream, info, err := s.inner.Stream(ctx, source, opts)
	if err == nil {
		s.once.Do(func() { close(s.ready) })
	}
	return stream, info, err
}

func TestLuaSeesRealRunningSQLiteSourceAndItsChanges(t *testing.T) {
	db, _ := openPool(t)
	_, err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT)`)
	require.NoError(t, err)

	bus := eventbus.NewBus()
	sup := syssup.NewSupervisor(bus, zap.NewNop())
	supCtx, supCancel := context.WithCancel(context.Background())
	defer supCancel()
	require.NoError(t, sup.Start(supCtx))

	transcoder := systempayload.NewTranscoder()
	luapayload.Register(transcoder)

	manager, err := NewManager(transcoder, bus, zap.NewNop(), &fakeRegistry{db: db})
	require.NoError(t, err)

	entryID := registry.NewID("test", "cdc-lua-e2e")
	src, err := buildSource(sourceOptions{
		res:            &fakeRegistry{db: db},
		dbResource:     registry.NewID("app", "db"),
		name:           entryID.String(),
		statusInterval: "1h",
	})
	require.NoError(t, err)
	srcImpl := src.(*Source)
	manager.sources[entryID] = src
	manager.storeInfo(registry.Entry{ID: entryID, Kind: cdcapi.SQLite}, &cdcapi.SQLiteConfig{DBResource: "app:db"})

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
		return srcImpl.Epoch() != ""
	}, 15*time.Second, 50*time.Millisecond, "supervisor must auto-start the registered source")
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = srcImpl.Stop(stopCtx)
		stopCancel()
	}()

	streamer := &signalingStreamer{inner: manager, ready: make(chan struct{})}
	root := security.SetStrictMode(ctxapi.NewRootContext(), false)
	payload.WithTranscoder(root, transcoder)
	root = cdcapi.WithSourceInspector(root, manager)
	root = cdcapi.WithSourceStreamer(root, streamer)

	node := sysrelay.NewNode("cdc-lua-sqlite-node")
	root = relay.WithNode(root, node)

	runCtx, runCancel := context.WithTimeout(root, 30*time.Second)
	defer runCancel()

	dispReg := scheduler.NewRegistry()
	cdcDisp := pgcdc.NewDispatcher(pgcdc.WithWorkers(1))
	require.NoError(t, cdcDisp.Start(runCtx))
	defer func() { require.NoError(t, cdcDisp.Stop(context.Background())) }()
	cdcDisp.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		dispReg.Register(id, h)
	})

	const expectedEmail = "lua-sqlite-e2e@wippy.ai"
	const hostID = "test.cdc.sqlite.lua"
	factory := func() (process.Process, error) {
		cfg := engine.FactoryConfig{
			ScriptName: "cdc_sqlite_lua_e2e",
			Script: `
local cdc = require("cdc")

local function main()
    local rows, err = cdc.list_sources()
    if err ~= nil then return nil, "list_sources error: " .. tostring(err) end
    if #rows ~= 1 then return nil, "expected 1 source, got " .. tostring(#rows) end

    local r = rows[1]
    if r.engine ~= "sqlite" then return nil, "wrong engine: " .. tostring(r.engine) end

    local stream, stream_err = cdc.stream("test:cdc-lua-e2e", {
        tables = {"users"},
        ops = {"insert"},
        buffer = 8,
    })
    if stream_err ~= nil then return nil, "stream error: " .. tostring(stream_err) end

    local ch = stream:channel()
    local change, ok = ch:receive()
    stream:release()
    if ok ~= true then return nil, "stream closed before change" end
    if change.op ~= "insert" then return nil, "wrong op: " .. tostring(change.op) end
    if change.source ~= "test:cdc-lua-e2e" then return nil, "wrong source: " .. tostring(change.source) end
    if change.schema ~= "main" then return nil, "wrong schema: " .. tostring(change.schema) end
    if change.table ~= "users" then return nil, "wrong table: " .. tostring(change.table) end
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
	case err := <-errCh:
		require.NoError(t, err)
	case result := <-resultCh:
		t.Fatalf("Lua returned before subscribing: value=%v err=%v", func() any {
			if result != nil && result.Value != nil {
				return result.Value.Data()
			}
			return nil
		}(), func() any {
			if result != nil {
				return result.Error
			}
			return nil
		}())
	case <-runCtx.Done():
		t.Fatal("timed out waiting for Lua CDC stream subscription")
	}

	_, err = db.Exec(`INSERT INTO users (email) VALUES (?)`, expectedEmail)
	require.NoError(t, err)

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
}
