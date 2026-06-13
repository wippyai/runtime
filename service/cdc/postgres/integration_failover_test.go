//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const failoverSlot = "wippy_cdc_failover"

func TestFailoverSlotIsMarked(t *testing.T) {
	repl, admin := dsns(t)
	db, err := sql.Open("postgres", admin)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	setupSchema(t, db)
	dropNamedSlot(t, repl, failoverSlot)
	defer dropNamedSlot(t, repl, failoverSlot)

	bus := newCaptureBus()
	src := NewSource(SourceOptions{
		ReplDSN: repl, AdminDSN: admin, Slot: failoverSlot, Publication: "wippy_cdc_pub",
		Bus: bus, Failover: true, StandbyInterval: 200 * time.Millisecond, StatusInterval: time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err = src.Start(ctx)
	require.NoError(t, err)

	var failover bool
	require.NoError(t, db.QueryRow(
		`SELECT failover FROM pg_replication_slots WHERE slot_name = $1`, failoverSlot).Scan(&failover))
	assert.True(t, failover, "failover-enabled source must mark its slot for failover")

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	require.NoError(t, src.Stop(stopCtx))
	stopCancel()
}
