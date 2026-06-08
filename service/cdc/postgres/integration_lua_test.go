//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"go.uber.org/zap"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	cdcapi "github.com/wippyai/runtime/api/service/cdc"
	apisup "github.com/wippyai/runtime/api/supervisor"
	cdcmod "github.com/wippyai/runtime/runtime/lua/modules/cdc"
	"github.com/wippyai/runtime/system/eventbus"
	syssup "github.com/wippyai/runtime/system/supervisor"
)

const luaSlot = "wippy_cdc_lua"

func TestLuaSeesRealRunningSourceAndItsChanges(t *testing.T) {
	repl, admin := dsns(t)
	db, err := sql.Open("postgres", admin)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	setupSchema(t, db)
	dropNamedSlot(t, repl, luaSlot)
	defer dropNamedSlot(t, repl, luaSlot)
	_, err = db.ExecContext(context.Background(), `DELETE FROM accounts`)
	require.NoError(t, err)

	bus := eventbus.NewBus()
	sup := syssup.NewSupervisor(bus, zap.NewNop())
	supCtx, supCancel := context.WithCancel(context.Background())
	defer supCancel()
	require.NoError(t, sup.Start(supCtx))

	manager := &Manager{
		bus:        bus,
		log:        zap.NewNop(),
		sources:    map[registry.ID]*Source{},
		infos:      map[registry.ID]cdcapi.SourceInfo{},
		infosByKey: map[string]registry.ID{},
	}

	entryID := registry.NewID("test", "cdc-lua-e2e")
	cfg := &cdcapi.Config{
		Host: "localhost", Port: 5432, Username: "u", Password: "p",
		Database: "d", SlotName: luaSlot, Publication: "wippy_cdc_pub",
		Streaming: true, EventSystem: cdcapi.DefaultEventSystem,
	}
	src := NewSource(SourceOptions{
		ReplDSN: repl, AdminDSN: admin, Slot: luaSlot, Publication: "wippy_cdc_pub",
		Bus: bus, StandbyInterval: 200 * time.Millisecond, StatusInterval: time.Hour,
	})
	manager.sources[entryID] = src
	manager.storeInfo(registry.Entry{ID: entryID, Kind: cdcapi.Postgres}, cfg)

	ch := make(chan event.Event, 64)
	subID, err := bus.SubscribeP(supCtx, "postgres_cdc", "change", ch)
	require.NoError(t, err)
	defer bus.Unsubscribe(supCtx, subID)

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

	l := lua.NewState()
	defer l.Close()
	l.SetContext(cdcapi.WithSourceInspector(supCtx, manager))
	tbl, _ := cdcmod.Module.Build()
	l.SetGlobal(cdcmod.Module.Name, tbl)

	require.NoError(t, l.DoString(`
		local rows, err = cdc.list_sources()
		assert(err == nil, "list_sources error: " .. tostring(err))
		assert(#rows == 1, "expected 1 source, got " .. tostring(#rows))
		local r = rows[1]
		assert(r.slot == "`+luaSlot+`", "wrong slot: " .. tostring(r.slot))
		assert(r.publication == "wippy_cdc_pub", "wrong publication: " .. tostring(r.publication))
		assert(r.streaming == true, "expected streaming=true")
		assert(r.failover == false, "expected failover=false")
		assert(r.event_system == "postgres_cdc", "wrong event_system: " .. tostring(r.event_system))

		local by_slot, err2 = cdc.source("`+luaSlot+`")
		assert(err2 == nil, "source error: " .. tostring(err2))
		assert(by_slot ~= nil, "lookup by slot returned nil")
		assert(by_slot.slot == "`+luaSlot+`")

		local by_id, err3 = cdc.source("test:cdc-lua-e2e")
		assert(err3 == nil, "source by id error: " .. tostring(err3))
		assert(by_id ~= nil, "lookup by entry id returned nil")
		assert(by_id.slot == "`+luaSlot+`")
	`))
	t.Log("Lua introspection passed against the live Manager")

	res, err := db.ExecContext(supCtx, `INSERT INTO accounts (email, balance) VALUES ($1, $2)`,
		"lua-e2e@wippy.ai", 42)
	require.NoError(t, err)
	rows, _ := res.RowsAffected()
	require.Equal(t, int64(1), rows)

	deadline := time.After(15 * time.Second)
	var changeFromBus RowChange
	for {
		select {
		case e := <-ch:
			rc, ok := e.Data.(RowChange)
			if !ok {
				continue
			}
			if em, _ := rc.After["email"].(string); em == "lua-e2e@wippy.ai" {
				changeFromBus = rc
				goto received
			}
		case <-deadline:
			t.Fatal("change did not flow through the real bus + supervisor chain")
		}
	}

received:
	require.Equal(t, OpInsert, changeFromBus.Op)
	require.Equal(t, "accounts", changeFromBus.Table)
	t.Logf("Bus received RowChange: op=%s schema=%s table=%s lsn=%s after=%v",
		changeFromBus.Op, changeFromBus.Schema, changeFromBus.Table,
		changeFromBus.LSN, changeFromBus.After)

	require.NoError(t, l.DoString(`
		local row = cdc.source("`+luaSlot+`")
		assert(row ~= nil, "Lua cannot find the source that just emitted a change")
		assert(row.event_system == "postgres_cdc")
	`))
	t.Log("Lua resolves the still-running source by slot after a real PG mutation flowed through the bus")
}
