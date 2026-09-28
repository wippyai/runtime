// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	apiconfig "github.com/wippyai/runtime/api/service/sql"
)

var testID = registry.NewID("test", "db")

func newTestPool(t *testing.T) *ConnPool {
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)

	pool := &ConnPool{
		kind:   apiconfig.SQLite,
		db:     db,
		driver: func() Driver { d, _ := testDriverFor(apiconfig.SQLite); return d }(),
		status: make(chan any, 1),
	}

	cfg := &apiconfig.SQLiteConfig{
		File: ":memory:",
		Pool: apiconfig.PoolConfig{MaxLifetime: time.Hour},
	}
	var cfgAny any = cfg
	pool.config.Store(&cfgAny)

	return pool
}

func TestConnPool_StartStop(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	statusCh, err := pool.Start(ctx)
	require.NoError(t, err)
	assert.NotNil(t, statusCh)

	select {
	case msg := <-statusCh:
		assert.Equal(t, "database connection established", msg)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for status")
	}

	err = pool.Stop(ctx)
	assert.NoError(t, err)
}

func TestConnPool_StartAfterClose(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	pool.closed.Store(true)

	_, err := pool.Start(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestConnPool_DoubleStop(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	_, err := pool.Start(ctx)
	require.NoError(t, err)

	err = pool.Stop(ctx)
	assert.NoError(t, err)

	err = pool.Stop(ctx)
	assert.NoError(t, err)
}

func TestConnPool_Acquire(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	_, err := pool.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = pool.Stop(ctx) }()

	res, err := pool.Acquire(ctx, testID, resource.ModeNormal)
	require.NoError(t, err)
	assert.NotNil(t, res)

	dbRes, err := res.Get()
	require.NoError(t, err)
	assert.IsType(t, DBResource{}, dbRes)

	res.Release()
}

func TestConnPool_AcquireUnsupportedMode(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	_, err := pool.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = pool.Stop(ctx) }()

	_, err = pool.Acquire(ctx, testID, resource.ModeExclusive)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported access mode")
}

func TestConnPool_AcquireAfterClose(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	_, err := pool.Start(ctx)
	require.NoError(t, err)

	err = pool.Stop(ctx)
	require.NoError(t, err)

	_, err = pool.Acquire(ctx, testID, resource.ModeNormal)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestConnPool_StopWaitsForResources(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	_, err := pool.Start(ctx)
	require.NoError(t, err)

	res, err := pool.Acquire(ctx, testID, resource.ModeNormal)
	require.NoError(t, err)

	stopDone := make(chan error)
	go func() {
		stopCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		defer cancel()
		stopDone <- pool.Stop(stopCtx)
	}()

	time.Sleep(100 * time.Millisecond)
	res.Release()

	select {
	case err := <-stopDone:
		assert.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for stop")
	}
}

func TestConnPool_StopTimeout(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	_, err := pool.Start(ctx)
	require.NoError(t, err)

	res, err := pool.Acquire(ctx, testID, resource.ModeNormal)
	require.NoError(t, err)
	defer res.Release()

	stopCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	err = pool.Stop(stopCtx)
	assert.Error(t, err)
	assert.Equal(t, context.DeadlineExceeded, err)
}

func TestConnPool_StopTimeoutStillCleansUp(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	_, err := pool.Start(ctx)
	require.NoError(t, err)
	res, err := pool.Acquire(ctx, testID, resource.ModeNormal)
	require.NoError(t, err)
	stopCtx, cancel := context.WithTimeout(ctx, 25*time.Millisecond)
	err = pool.Stop(stopCtx)
	cancel()
	require.ErrorIs(t, err, context.DeadlineExceeded)
	res.Release()
	require.NoError(t, pool.Stop(ctx))
	_, err = pool.Start(ctx)
	assert.ErrorIs(t, err, ErrPoolClosed)
}

func TestDBConn_DoubleRelease(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	_, err := pool.Start(ctx)
	require.NoError(t, err)

	res, err := pool.Acquire(ctx, testID, resource.ModeNormal)
	require.NoError(t, err)

	res.Release()
	res.Release()

	err = pool.Stop(ctx)
	assert.NoError(t, err)
}

func TestDBConn_GetAfterRelease(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	_, err := pool.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = pool.Stop(ctx) }()

	res, err := pool.Acquire(ctx, testID, resource.ModeNormal)
	require.NoError(t, err)

	res.Release()

	_, err = res.Get()
	assert.Error(t, err)
	assert.Equal(t, resource.ErrReleased, err)
}

func TestConnPool_ConcurrentAcquireRelease(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	_, err := pool.Start(ctx)
	require.NoError(t, err)

	const goroutines = 50
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				res, err := pool.Acquire(ctx, testID, resource.ModeNormal)
				if err != nil {
					continue
				}
				_, _ = res.Get()
				res.Release()
			}
		}()
	}

	wg.Wait()

	err = pool.Stop(ctx)
	assert.NoError(t, err)
}

func TestConnPool_UpdateConfig(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	_, err := pool.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = pool.Stop(ctx) }()

	newCfg := &apiconfig.SQLiteConfig{
		File: ":memory:",
		Pool: apiconfig.PoolConfig{MaxLifetime: 2 * time.Hour},
	}

	err = pool.UpdateConfig(newCfg)
	assert.NoError(t, err)
}

func TestConnPool_UpdateConfigWrongType(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	_, err := pool.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = pool.Stop(ctx) }()

	dbCfg := &apiconfig.DBConfig{
		Host:     "localhost",
		Port:     5432,
		Database: "test",
		Username: "user",
		Password: "pass",
		Pool:     apiconfig.PoolConfig{MaxOpen: 10, MaxIdle: 5, MaxLifetime: time.Hour},
	}

	err = pool.UpdateConfig(dbCfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid config type")
}

func TestConnPool_UpdateConfigAfterClose(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	_, err := pool.Start(ctx)
	require.NoError(t, err)

	err = pool.Stop(ctx)
	require.NoError(t, err)

	newCfg := &apiconfig.SQLiteConfig{
		File: ":memory:",
		Pool: apiconfig.PoolConfig{MaxLifetime: 2 * time.Hour},
	}

	err = pool.UpdateConfig(newCfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestConnPool_UpdateConfigKeepsCurrentDBOnInvalidStandardConfig(t *testing.T) {
	pool := NewMockConnPool(apiconfig.Postgres)
	ctx := context.Background()
	defer func() { _ = pool.Stop(ctx) }()

	oldDB := pool.currentGeneration().db

	err := pool.UpdateConfig(&apiconfig.DBConfig{
		Host:     "",
		Port:     5432,
		Database: "db",
		Username: "user",
		Password: "pass",
		Pool:     apiconfig.PoolConfig{MaxLifetime: time.Hour},
	})
	require.Error(t, err)
	assert.Same(t, oldDB, pool.currentGeneration().db)
	require.NoError(t, oldDB.PingContext(ctx))
}

func TestConnPool_UpdateConfigSwapsStandardDBAndRetiresOldAfterRelease(t *testing.T) {
	pool := NewMockConnPool(apiconfig.Postgres)
	ctx := context.Background()

	res, err := pool.Acquire(ctx, testID, resource.ModeNormal)
	require.NoError(t, err)

	oldResource, err := res.Get()
	require.NoError(t, err)
	oldDB := oldResource.(DBResource).DB

	newCfg := &apiconfig.DBConfig{
		Host:     "updated-host",
		Port:     5432,
		Database: "updated-db",
		Username: "updated-user",
		Password: "updated-pass",
		Pool:     apiconfig.PoolConfig{MaxOpen: 7, MaxIdle: 3, MaxLifetime: time.Hour},
	}

	require.NoError(t, pool.UpdateConfig(newCfg))
	require.NotSame(t, oldDB, pool.db)

	stillBorrowed, err := res.Get()
	require.NoError(t, err)
	assert.Same(t, oldDB, stillBorrowed.(DBResource).DB)
	require.NoError(t, oldDB.PingContext(ctx))

	res.Release()

	require.Eventually(t, func() bool {
		return oldDB.PingContext(ctx) != nil
	}, time.Second, 10*time.Millisecond)

	require.NoError(t, pool.Stop(ctx))
}

// Benchmarks

func newBenchPool(b *testing.B) *ConnPool {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		b.Fatal(err)
	}

	pool := &ConnPool{
		kind:   apiconfig.SQLite,
		db:     db,
		status: make(chan any, 1),
	}

	cfg := &apiconfig.SQLiteConfig{
		File: ":memory:",
		Pool: apiconfig.PoolConfig{MaxLifetime: time.Hour},
	}
	var cfgAny any = cfg
	pool.config.Store(&cfgAny)

	return pool
}

func BenchmarkConnPool_Acquire(b *testing.B) {
	pool := newBenchPool(b)
	ctx := context.Background()
	_, _ = pool.Start(ctx)
	defer func() { _ = pool.Stop(ctx) }()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		res, _ := pool.Acquire(ctx, testID, resource.ModeNormal)
		res.Release()
	}
}

func BenchmarkConnPool_AcquireGet(b *testing.B) {
	pool := newBenchPool(b)
	ctx := context.Background()
	_, _ = pool.Start(ctx)
	defer func() { _ = pool.Stop(ctx) }()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		res, _ := pool.Acquire(ctx, testID, resource.ModeNormal)
		_, _ = res.Get()
		res.Release()
	}
}

func BenchmarkConnPool_ConcurrentAcquire(b *testing.B) {
	pool := newBenchPool(b)
	ctx := context.Background()
	_, _ = pool.Start(ctx)
	defer func() { _ = pool.Stop(ctx) }()

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			res, _ := pool.Acquire(ctx, testID, resource.ModeNormal)
			_, _ = res.Get()
			res.Release()
		}
	})
}
