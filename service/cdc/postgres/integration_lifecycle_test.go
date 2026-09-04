//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	freshFailureSlot    = "wippy_cdc_fresh_failure"
	autoPublicationSlot = "wippy_cdc_auto_pub"
)

func TestMissingPublicationFailsBeforeCreatingSlot(t *testing.T) {
	repl, admin := dsns(t)
	db, err := sql.Open("postgres", admin)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	setupSchema(t, db)
	dropNamedSlot(t, repl, freshFailureSlot)
	defer dropNamedSlot(t, repl, freshFailureSlot)

	src := NewSource(SourceOptions{
		ReplDSN: repl, AdminDSN: admin, Slot: freshFailureSlot,
		Publication:     "wippy_cdc_missing_publication",
		StandbyInterval: time.Millisecond, StatusInterval: time.Hour,
	})
	_, err = src.Start(context.Background())
	defer func() { _ = src.Stop(context.Background()) }()
	require.ErrorContains(t, err, "does not exist")
	assert.Eventually(t, func() bool {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM pg_replication_slots WHERE slot_name=$1`, freshFailureSlot).Scan(&count); err != nil {
			return false
		}
		return count == 0
	}, 5*time.Second, 100*time.Millisecond, "invalid publication must not leave a replication slot")
}

func TestMissingSlotDeletesStaleCheckpointBeforeRecreate(t *testing.T) {
	repl, admin := dsns(t)
	db, err := sql.Open("postgres", admin)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	setupSchema(t, db)
	dropNamedSlot(t, repl, freshFailureSlot)
	defer dropNamedSlot(t, repl, freshFailureSlot)

	_, err = NewDBCheckpointer(context.Background(), db)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO wippy_cdc_offsets (slot, lsn) VALUES ($1, $2)`, freshFailureSlot, "F/FFFFFFF")
	require.NoError(t, err)

	src := NewSource(SourceOptions{
		ReplDSN: repl, AdminDSN: admin, Slot: freshFailureSlot,
		Publication: "wippy_cdc_pub", StandbyInterval: 200 * time.Millisecond,
		StatusInterval: time.Hour,
	})
	status, err := src.Start(context.Background())
	require.NoError(t, err)
	defer func() { _ = src.Stop(context.Background()) }()

	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM wippy_cdc_offsets WHERE slot=$1`, freshFailureSlot).Scan(&count))
	assert.Zero(t, count, "an offset from a removed slot must not survive recreation")
	_ = status
}

func TestAutoPublicationReconcilesTableMembership(t *testing.T) {
	repl, admin := dsns(t)
	db, err := sql.Open("postgres", admin)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	setupSchema(t, db)
	const extraTable = "wippy_cdc_auto_extra"
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS ` + pq.QuoteIdentifier(extraTable) + ` (id bigint PRIMARY KEY)`)
	require.NoError(t, err)
	pubName := autoPublicationSlot + "_pub"
	_, err = db.Exec(`DROP PUBLICATION IF EXISTS ` + pq.QuoteIdentifier(pubName))
	require.NoError(t, err)
	dropNamedSlot(t, repl, autoPublicationSlot)
	defer func() {
		dropNamedSlot(t, repl, autoPublicationSlot)
		_, _ = db.Exec(`DROP PUBLICATION IF EXISTS ` + pq.QuoteIdentifier(pubName))
		_, _ = db.Exec(`DROP TABLE IF EXISTS ` + pq.QuoteIdentifier(extraTable))
	}()

	first := NewSource(SourceOptions{
		ReplDSN: repl, AdminDSN: admin, Slot: autoPublicationSlot,
		Tables: []string{"public.accounts"}, StandbyInterval: 200 * time.Millisecond,
		StatusInterval: time.Hour,
	})
	_, err = first.Start(context.Background())
	require.NoError(t, err)
	require.NoError(t, first.Stop(context.Background()))

	second := NewSource(SourceOptions{
		ReplDSN: repl, AdminDSN: admin, Slot: autoPublicationSlot,
		Tables: []string{extraTable}, StandbyInterval: 200 * time.Millisecond,
		StatusInterval: time.Hour,
	})
	_, err = second.Start(context.Background())
	require.NoError(t, err)
	defer func() { _ = second.Stop(context.Background()) }()

	rows, err := db.Query(`SELECT schemaname || '.' || tablename FROM pg_publication_tables WHERE pubname=$1 ORDER BY 1`, pubName)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var table string
		require.NoError(t, rows.Scan(&table))
		got = append(got, table)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"public." + extraTable}, got)
}
