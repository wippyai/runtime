// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/sqliteconn"
	"github.com/wippyai/runtime/internal/version"
	"github.com/wippyai/runtime/system/registry/history/checkpoint"
	"github.com/wippyai/runtime/system/registry/history/postgres"
	"github.com/wippyai/runtime/system/registry/history/sqlite"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

// BenchmarkPersistedLoadState measures authored history independently of module
// expansion and application startup. Setup and fixture writes are untimed.
// lineage-data-B counts encoded changeset bytes, excluding protocol/page overhead.
// replay-data-B counts the nearest supported checkpoint and its replay tail.
// peak-heap-B samples live Go heap every millisecond during LoadState; B/op is
// total allocation. Set WIPPY_POSTGRES_HISTORY_TEST_DSN to a disposable database.
func BenchmarkPersistedLoadState(b *testing.B) {
	for _, backend := range []string{"sqlite", "postgres"} {
		for _, profile := range []string{"config", "lua50k"} {
			for _, count := range []int{1000, 5000, 20000} {
				b.Run(fmt.Sprintf("%s/%s/versions=%d", backend, profile, count), func(b *testing.B) {
					benchmarkPersistedLoadState(b, backend, profile, count)
				})
			}
		}
	}
}

func benchmarkPersistedLoadState(b *testing.B, backend, profile string, count int) {
	b.StopTimer()
	ctx := context.Background()
	entryAt := func(revision int) registry.Entry {
		entry := registry.Entry{ID: registry.NewID("bench", "setting"), Kind: "config"}
		if profile == "lua50k" {
			entry.Kind = "function.lua"
			entry.Data = payload.NewString(fmt.Sprintf("-- edit %d\n", revision%2) + strings.Repeat("-", 50*1024) + "\nreturn 1")
			return entry
		}
		entry.Data = payload.New(map[string]any{"enabled": true, "revision": revision % 2})
		return entry
	}
	var hist registry.History
	var db *sql.DB
	b.Cleanup(func() {
		if db != nil {
			if err := db.Close(); err != nil {
				b.Error(err)
			}
		}
	})
	var byteQuery, replayByteQuery, versionsTable, changesetsTable, checkpointsTable, parameter string
	if backend == "sqlite" {
		path := filepath.Join(b.TempDir(), "history.db")
		h, err := sqlite.NewSQLite(path, zap.NewNop())
		if err != nil {
			b.Fatal(err)
		}
		b.Cleanup(func() {
			if err := h.Close(); err != nil {
				b.Error(err)
			}
		})
		hist = h
		db = sqliteconn.Open("file:" + path)
		versionsTable, changesetsTable, parameter = "versions", "changesets", "?"
		checkpointsTable = "replay_checkpoints"
		byteQuery = "SELECT COALESCE(SUM(length(data)), 0) FROM changesets"
	} else {
		dsn := os.Getenv("WIPPY_POSTGRES_HISTORY_TEST_DSN")
		if dsn == "" {
			b.Fatal("WIPPY_POSTGRES_HISTORY_TEST_DSN must name a disposable database")
		}
		var err error
		db, err = sql.Open("postgres", dsn)
		if err != nil {
			b.Fatal(err)
		}
		schema := fmt.Sprintf("replay_bench_%d_%d", os.Getpid(), time.Now().UnixNano())
		h, err := postgres.NewPostgres(dsn, schema, zap.NewNop())
		if err != nil {
			b.Fatal(err)
		}
		hist = h
		b.Cleanup(func() {
			if err := h.Close(); err != nil {
				b.Error(err)
			}
			if _, err := db.ExecContext(ctx, fmt.Sprintf("DROP SCHEMA %q CASCADE", schema)); err != nil {
				b.Error(err)
			}
		})
		versionsTable, changesetsTable, checkpointsTable, parameter = fmt.Sprintf("%q.versions", schema), fmt.Sprintf("%q.changesets", schema), fmt.Sprintf("%q.replay_checkpoints", schema), "$1"
		byteQuery = fmt.Sprintf("SELECT COALESCE(SUM(octet_length(data)), 0) FROM %q.changesets", schema)
	}

	head, err := hist.Head()
	if err != nil {
		b.Fatal(err)
	}
	var previous registry.Entry
	// Three real saves provide create and alternating update encodings. Bulk
	// insertion copies those immutable wire records in one fixture transaction.
	for i := 1; i <= 3; i++ {
		entry := entryAt(i)
		op := registry.Operation{Kind: registry.EntryCreate, Entry: entry}
		if i > 1 {
			op.Kind = registry.EntryUpdate
			op.OriginalEntry = &previous
		}
		head = version.FromParent(head, uint(i))
		if err := hist.Save(head, registry.ChangeSet{op}, true); err != nil {
			b.Fatal(err)
		}
		previous = entry
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			b.Error(err)
		}
	}()
	secondParameter := "?"
	if backend == "postgres" {
		secondParameter = "$2"
	}
	insertVersion, err := tx.PrepareContext(ctx, "INSERT INTO "+versionsTable+" (id, parent_id) VALUES ("+parameter+", "+secondParameter+")")
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		if err := insertVersion.Close(); err != nil {
			b.Error(err)
		}
	}()
	insertChanges, err := tx.PrepareContext(ctx, "INSERT INTO "+changesetsTable+" (version_id, data) SELECT "+parameter+", data FROM "+changesetsTable+" WHERE version_id = "+secondParameter)
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		if err := insertChanges.Close(); err != nil {
			b.Error(err)
		}
	}()
	for i := 4; i < count; i++ {
		if _, err := insertVersion.ExecContext(ctx, i, i-1); err != nil {
			b.Fatal(err)
		}
		if _, err := insertChanges.ExecContext(ctx, i, 2+i%2); err != nil {
			b.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	parent, err := hist.(registry.VersionLookup).GetVersion(uint(count - 1))
	if err != nil {
		b.Fatal(err)
	}
	if err := hist.SetHead(parent); err != nil {
		b.Fatal(err)
	}
	entry := entryAt(count)
	previousEntry := entryAt(count - 1)
	head = version.FromParent(parent, uint(count))
	if err := hist.Save(head, registry.ChangeSet{{Kind: registry.EntryUpdate, Entry: entry, OriginalEntry: &previousEntry}}, true); err != nil {
		b.Fatal(err)
	}
	expected, err := hist.Get(head)
	if err != nil || len(expected) != 1 {
		b.Fatalf("read fixture head: %v", err)
	}
	var dataBytes int64
	if err := db.QueryRowContext(ctx, byteQuery).Scan(&dataBytes); err != nil {
		b.Fatal(err)
	}
	replayByteQuery = `WITH RECURSIVE lineage(id,parent_id,depth) AS (
		SELECT id,parent_id,0 FROM ` + versionsTable + ` WHERE id = ` + parameter + `
		UNION
		SELECT v.id,v.parent_id,lineage.depth+1 FROM ` + versionsTable + ` v JOIN lineage ON v.id = lineage.parent_id
	), checkpoint AS (
		SELECT lineage.depth,length(p.data) AS bytes FROM lineage JOIN ` + checkpointsTable + ` p ON p.version_id = lineage.id
		WHERE p.format_version = ` + strconv.Itoa(checkpoint.FormatVersion) + ` ORDER BY lineage.depth LIMIT 1
	)
	SELECT COALESCE((SELECT bytes FROM checkpoint),0) + COALESCE((
		SELECT SUM(length(c.data)) FROM lineage JOIN ` + changesetsTable + ` c ON c.version_id = lineage.id
		WHERE lineage.id != 0 AND lineage.depth < COALESCE((SELECT depth FROM checkpoint), 9223372036854775807)
	),0)`
	var replayBytes int64
	if err := db.QueryRowContext(ctx, replayByteQuery, head.ID()).Scan(&replayBytes); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	var peak uint64
	for i := 0; i < b.N; i++ {
		runtime.GC()
		done := make(chan struct{})
		sampled := make(chan uint64, 1)
		go func() {
			ticker := time.NewTicker(time.Millisecond)
			defer ticker.Stop()
			var maximum uint64
			for {
				var mem runtime.MemStats
				runtime.ReadMemStats(&mem)
				maximum = max(maximum, mem.HeapAlloc)
				select {
				case <-done:
					sampled <- maximum
					return
				case <-ticker.C:
				}
			}
		}()
		builder := topology.NewStateBuilder(zap.NewNop(), nil)
		reg := NewRegistry(hist, newApplyingMockRunner(builder), builder, nil, zap.NewNop())
		b.StartTimer()
		err := reg.LoadState(ctx, nil, head)
		b.StopTimer()
		close(done)
		peak = max(peak, <-sampled)
		if err != nil {
			b.Fatal(err)
		}
		entry, err := reg.GetEntry(previous.ID)
		if err != nil || entry.Kind != expected[0].Entry.Kind || entry.Data == nil ||
			fmt.Sprint(entry.Data.Data()) != fmt.Sprint(expected[0].Entry.Data.Data()) {
			b.Fatalf("restored entry does not match fixture head: %v", err)
		}
	}
	b.ReportMetric(float64(dataBytes), "lineage-data-B")
	b.ReportMetric(float64(replayBytes), "replay-data-B")
	b.ReportMetric(float64(peak), "peak-heap-B")
}
