//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	apisup "github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/system/eventbus"
	syssup "github.com/wippyai/runtime/system/supervisor"
)

const superSlot = "wippy_cdc_super"

func TestSupervisorStartsSourceAndDelivers(t *testing.T) {
	repl, admin := dsns(t)
	db, err := sql.Open("postgres", admin)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	setupSchema(t, db)
	dropNamedSlot(t, repl, superSlot)
	defer dropNamedSlot(t, repl, superSlot)
	_, err = db.Exec(`DELETE FROM accounts`)
	require.NoError(t, err)

	bus := eventbus.NewBus()
	sup := syssup.NewSupervisor(bus, zap.NewNop())
	supCtx, supCancel := context.WithCancel(context.Background())
	defer supCancel()
	require.NoError(t, sup.Start(supCtx))

	src := NewSource(SourceOptions{
		ReplDSN: repl, AdminDSN: admin, Slot: superSlot, Publication: "wippy_cdc_pub",
		Bus: bus, StandbyInterval: 200 * time.Millisecond, StatusInterval: time.Hour,
	})

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
		Path:   "cdc-super-test",
		Data:   &apisup.Entry{Service: src, Config: lc},
	})
	bus.Send(supCtx, event.Event{System: registry.System, Kind: registry.TxCommit, Path: "tx"})

	require.Eventually(t, func() bool {
		var n int
		_ = db.QueryRow(`SELECT count(*) FROM pg_replication_slots WHERE slot_name=$1`, superSlot).Scan(&n)
		return n == 1
	}, 15*time.Second, 100*time.Millisecond, "supervisor must auto-start the registered source")

	_, err = db.Exec(`INSERT INTO accounts (email, balance) VALUES ('super@w.ai', 1)`)
	require.NoError(t, err)

	deadline := time.After(15 * time.Second)
	for {
		select {
		case e := <-ch:
			if rc, ok := e.Data.(RowChange); ok {
				if em, _ := rc.After["email"].(string); em == "super@w.ai" {
					return
				}
			}
		case <-deadline:
			t.Fatal("change did not flow through the real bus + supervisor chain")
		}
	}
}
