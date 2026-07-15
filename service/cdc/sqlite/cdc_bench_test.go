// SPDX-License-Identifier: MPL-2.0

//go:build sqlite_preupdate_hook

package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	config "github.com/wippyai/runtime/api/service/cdc"
	sqlconfig "github.com/wippyai/runtime/api/service/sql"
	sqlservice "github.com/wippyai/runtime/service/sql"
)

var benchCtx = context.Background()

type benchRegistry struct{ db *sql.DB }

func (r *benchRegistry) Acquire(context.Context, registry.ID, resource.AccessMode) (resource.Resource[any], error) {
	return &benchResource{res: sqlservice.DBResource{DB: r.db, Type: sqlconfig.SQLite}}, nil
}
func (r *benchRegistry) List() ([]registry.ID, error) { return nil, nil }
func (r *benchRegistry) Exists(registry.ID) bool      { return true }

type benchResource struct{ res sqlservice.DBResource }

func (b *benchResource) Get() (any, error) { return b.res, nil }
func (b *benchResource) Release()          {}

func benchPool(b *testing.B, driver string) *sql.DB {
	b.Helper()
	file := filepath.Join(b.TempDir(), "bench.db")
	db, err := sql.Open(driver, "file:"+file+"?mode=rwc")
	if err != nil {
		b.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.ExecContext(benchCtx, "PRAGMA journal_mode=WAL"); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = db.Close() })
	return db
}

func benchStartSource(b *testing.B, db *sql.DB) *Source {
	b.Helper()
	h, err := buildSource(sourceOptions{
		res:            &benchRegistry{db: db},
		dbResource:     registry.NewID("app", "db"),
		name:           "bench-src",
		statusInterval: "1h",
	})
	if err != nil {
		b.Fatal(err)
	}
	src := h.(*Source)
	if _, err := src.Start(context.Background()); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = src.Stop(context.Background()) })
	return src
}

func drainStream(stream config.ChangeStream) func() {
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			case _, ok := <-stream.Changes():
				if !ok {
					return
				}
			}
		}
	}()
	return func() { close(stop) }
}

func reportLatencies(b *testing.B, lat []time.Duration) {
	if len(lat) == 0 {
		return
	}
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	p := func(q float64) time.Duration {
		idx := int(q * float64(len(lat)-1))
		return lat[idx]
	}
	b.ReportMetric(float64(p(0.50).Nanoseconds()), "p50-ns/commit")
	b.ReportMetric(float64(p(0.99).Nanoseconds()), "p99-ns/commit")
	b.ReportMetric(float64(p(0.999).Nanoseconds()), "p999-ns/commit")
}

func benchWrite(b *testing.B, db *sql.DB, payload string) {
	if _, err := db.ExecContext(benchCtx, "CREATE TABLE IF NOT EXISTS t (id INTEGER PRIMARY KEY, v TEXT)"); err != nil {
		b.Fatal(err)
	}
	lat := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		if _, err := db.ExecContext(benchCtx, "INSERT INTO t (v) VALUES (?)", payload); err != nil {
			b.Fatal(err)
		}
		lat = append(lat, time.Since(start))
	}
	b.StopTimer()
	reportLatencies(b, lat)
}

func BenchmarkWriteHooksDisabled(b *testing.B) {
	db := benchPool(b, "sqlite3")
	benchWrite(b, db, "payload")
}

func BenchmarkWriteHooksEnabledNoSubscribers(b *testing.B) {
	db := benchPool(b, sqliteCDCDriver)
	benchStartSource(b, db)
	benchWrite(b, db, "payload")
}

func BenchmarkWriteOneSubscriber(b *testing.B) {
	db := benchPool(b, sqliteCDCDriver)
	src := benchStartSource(b, db)
	stop := drainStream(src.Subscribe(config.StreamOptions{Buffer: 1024}))
	defer stop()
	benchWrite(b, db, "payload")
}

func BenchmarkWriteLargeBlobOneSubscriber(b *testing.B) {
	db := benchPool(b, sqliteCDCDriver)
	src := benchStartSource(b, db)
	stop := drainStream(src.Subscribe(config.StreamOptions{Buffer: 1024}))
	defer stop()
	blob := make([]byte, 64*1024)
	if _, err := db.ExecContext(benchCtx, "CREATE TABLE t (id INTEGER PRIMARY KEY, v BLOB)"); err != nil {
		b.Fatal(err)
	}
	lat := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		if _, err := db.ExecContext(benchCtx, "INSERT INTO t (v) VALUES (?)", blob); err != nil {
			b.Fatal(err)
		}
		lat = append(lat, time.Since(start))
	}
	b.StopTimer()
	reportLatencies(b, lat)
}

func BenchmarkWriteSaturatedSubscriber(b *testing.B) {
	db := benchPool(b, sqliteCDCDriver)
	src := benchStartSource(b, db)
	_ = src.Subscribe(config.StreamOptions{Buffer: 1})
	benchWrite(b, db, "payload")
}

func BenchmarkLargeTransaction(b *testing.B) {
	db := benchPool(b, sqliteCDCDriver)
	src := benchStartSource(b, db)
	stop := drainStream(src.Subscribe(config.StreamOptions{Buffer: 4096}))
	defer stop()
	if _, err := db.ExecContext(benchCtx, "CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)"); err != nil {
		b.Fatal(err)
	}
	const rowsPerTxn = 1000
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tx, err := db.BeginTx(benchCtx, nil)
		if err != nil {
			b.Fatal(err)
		}
		for j := 0; j < rowsPerTxn; j++ {
			if _, err := tx.ExecContext(benchCtx, "INSERT INTO t (v) VALUES (?)", "payload"); err != nil {
				b.Fatal(err)
			}
		}
		if err := tx.Commit(); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(rowsPerTxn), "rows/txn")
}
