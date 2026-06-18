// SPDX-License-Identifier: MPL-2.0

//go:build sqlite_preupdate_hook

package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	config "github.com/wippyai/runtime/api/service/cdc"
	sqlconfig "github.com/wippyai/runtime/api/service/sql"
	sqlservice "github.com/wippyai/runtime/service/sql"
)

const (
	offsetsTable           = "wippy_cdc_offsets"
	changesCounter         = "wippy_cdc_changes_total"
	walGauge               = "wippy_cdc_wal_size_bytes"
	defaultStatusInterval  = 30 * time.Second
	commitQueueSize        = 256
	auxBusyTimeoutMillisec = 5000
)

type capturedChange struct {
	table string
	old   []any
	new   []any
	op    int
	rowid int64
}

type Source struct {
	poolRes        resource.Resource[any]
	res            resource.Registry
	readDB         *sql.DB
	checkpointDB   *sql.DB
	runDone        chan struct{}
	commits        chan []capturedChange
	subs           *subscribers
	writerDB       *sql.DB
	cols           map[string][]columnInfo
	cancel         context.CancelFunc
	tables         map[string]struct{}
	log            *zap.Logger
	dbResID        registry.ID
	file           string
	name           string
	pending        []capturedChange
	statusInterval time.Duration
	seq            atomic.Uint64
	colMu          sync.RWMutex
	mu             sync.Mutex
	pendMu         sync.Mutex
	stopped        atomic.Bool
	dropCP         atomic.Bool
	snap           bool
}

func buildSource(opts sourceOptions) (sourceHandle, error) {
	log := opts.log
	if log == nil {
		log = zap.NewNop()
	}
	interval := defaultStatusInterval
	if opts.statusInterval != "" {
		d, err := time.ParseDuration(opts.statusInterval)
		if err != nil || d < 0 {
			return nil, fmt.Errorf("invalid status_interval %q", opts.statusInterval)
		}
		if d > 0 {
			interval = d
		}
	}
	return &Source{
		log:            log,
		res:            opts.res,
		subs:           newSubscribers(),
		name:           opts.name,
		statusInterval: interval,
		dbResID:        opts.dbResource,
		tables:         filterSet(opts.tables),
		cols:           make(map[string][]columnInfo),
		snap:           opts.snapshot,
	}, nil
}

func (s *Source) Subscribe(opts config.StreamOptions) config.ChangeStream {
	return s.subs.subscribe(opts)
}

func (s *Source) closeSubscriptions() {
	s.subs.closeAll()
}

func (s *Source) markDrop() {
	s.dropCP.Store(true)
}

func (s *Source) PreUpdate(op int, table string, rowid int64, old, new []any) {
	if !s.tableAllowed(table) {
		return
	}
	s.pendMu.Lock()
	s.pending = append(s.pending, capturedChange{op: op, table: table, rowid: rowid, old: old, new: new})
	s.pendMu.Unlock()
}

func (s *Source) Commit() {
	s.pendMu.Lock()
	batch := s.pending
	s.pending = nil
	s.pendMu.Unlock()
	if len(batch) == 0 {
		return
	}
	select {
	case s.commits <- batch:
	case <-s.runDone:
	}
}

func (s *Source) Rollback() {
	s.pendMu.Lock()
	s.pending = nil
	s.pendMu.Unlock()
}

func (s *Source) tableAllowed(table string) bool {
	lower := strings.ToLower(table)
	if lower == offsetsTable {
		return false
	}
	if len(s.tables) == 0 {
		return true
	}
	_, ok := s.tables[lower]
	return ok
}

func (s *Source) Start(ctx context.Context) (<-chan any, error) {
	if s.stopped.Load() {
		return nil, ErrSourceClosed
	}

	dbRes, res, err := s.acquirePool(ctx)
	if err != nil {
		return nil, err
	}
	writerDB := dbRes.DB

	conn, err := writerDB.Conn(ctx)
	if err != nil {
		res.Release()
		return nil, fmt.Errorf("acquire writer conn: %w", err)
	}

	var file string
	if rawErr := conn.Raw(func(dc any) error {
		f, e := installHooksOnRaw(dc, s)
		file = f
		return e
	}); rawErr != nil {
		_ = conn.Close()
		res.Release()
		return nil, rawErr
	}
	registerSink(file, s)

	s.mu.Lock()
	s.poolRes = res
	s.writerDB = writerDB
	s.file = file
	s.mu.Unlock()

	readDB, checkpointDB, err := openAuxConns(file)
	if err != nil {
		s.abortStart(ctx, conn, writerDB, file)
		return nil, err
	}

	s.mu.Lock()
	s.readDB = readDB
	s.checkpointDB = checkpointDB
	s.mu.Unlock()

	if err := ensureOffsets(ctx, checkpointDB); err != nil {
		s.abortStart(ctx, conn, writerDB, file)
		return nil, err
	}
	snapDone, lastSeq, loadErr := loadOffset(ctx, checkpointDB, s.name)
	if loadErr != nil {
		s.log.Warn("load cdc offset failed; treating as fresh", zap.Error(loadErr))
	}
	if lastSeq > s.seq.Load() {
		s.seq.Store(lastSeq)
	}

	runCtx, cancel := context.WithCancel(ctx)
	status := make(chan any, 8)
	runDone := make(chan struct{})
	commits := make(chan []capturedChange, commitQueueSize)

	s.mu.Lock()
	if s.stopped.Load() {
		s.mu.Unlock()
		cancel()
		s.abortStart(ctx, conn, writerDB, file)
		return nil, ErrSourceClosed
	}
	s.cancel = cancel
	s.runDone = runDone
	s.commits = commits
	s.mu.Unlock()

	doSnapshot := s.snap && !snapDone
	if doSnapshot {
		if err := s.runSnapshot(runCtx, conn); err != nil {
			cancel()
			s.abortStart(ctx, conn, writerDB, file)
			return nil, fmt.Errorf("snapshot: %w", err)
		}
		if serr := saveSnapshotDone(runCtx, checkpointDB, s.name); serr != nil {
			s.log.Warn("persist snapshot completion failed; restart may re-snapshot", zap.Error(serr))
		}
	}

	go s.run(runCtx, status, runDone, metrics.GetCollector(ctx))

	_ = conn.Close()

	select {
	case status <- "sqlite cdc started":
	default:
	}
	s.log.Info("sqlite cdc source started",
		zap.String("file", s.file),
		zap.Bool("snapshot", doSnapshot))
	return status, nil
}

func (s *Source) abortStart(ctx context.Context, conn *sql.Conn, writerDB *sql.DB, file string) {
	_ = conn.Close()
	s.detachHooks(ctx, writerDB, file)
	s.releaseResources(ctx)
}

func (s *Source) acquirePool(ctx context.Context) (sqlservice.DBResource, resource.Resource[any], error) {
	res, err := s.res.Acquire(ctx, s.dbResID, resource.ModeNormal)
	if err != nil {
		return sqlservice.DBResource{}, nil, fmt.Errorf("acquire db resource: %w", err)
	}
	dbAny, err := res.Get()
	if err != nil {
		res.Release()
		return sqlservice.DBResource{}, nil, fmt.Errorf("get db resource: %w", err)
	}
	dbRes, ok := dbAny.(sqlservice.DBResource)
	if !ok {
		res.Release()
		return sqlservice.DBResource{}, nil, fmt.Errorf("resource %s is not a database", s.name)
	}
	if dbRes.Type != sqlconfig.SQLite {
		res.Release()
		return sqlservice.DBResource{}, nil, fmt.Errorf("resource %s is not a sqlite database (kind %s)", s.name, dbRes.Type)
	}
	return dbRes, res, nil
}

func (s *Source) Stop(ctx context.Context) error {
	if !s.stopped.CompareAndSwap(false, true) {
		return nil
	}
	defer s.closeSubscriptions()

	s.mu.Lock()
	cancel := s.cancel
	runDone := s.runDone
	writerDB := s.writerDB
	file := s.file
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if runDone != nil {
		select {
		case <-runDone:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if writerDB != nil {
		s.detachHooks(ctx, writerDB, file)
	}
	if s.dropCP.Load() {
		s.mu.Lock()
		cpDB := s.checkpointDB
		s.mu.Unlock()
		if cpDB != nil {
			_ = deleteOffset(ctx, cpDB, s.name)
		}
	}
	s.releaseResources(ctx)
	return nil
}

func (s *Source) detachHooks(ctx context.Context, writerDB *sql.DB, file string) {
	unregisterSink(file)
	conn, err := writerDB.Conn(ctx)
	if err != nil {
		return
	}
	_ = conn.Raw(clearHooksOnRaw)
	_ = conn.Close()
}

func (s *Source) releaseResources(_ context.Context) {
	s.mu.Lock()
	readDB := s.readDB
	cpDB := s.checkpointDB
	res := s.poolRes
	s.readDB = nil
	s.checkpointDB = nil
	s.poolRes = nil
	s.mu.Unlock()

	if readDB != nil {
		_ = readDB.Close()
	}
	if cpDB != nil {
		_ = cpDB.Close()
	}
	if res != nil {
		res.Release()
	}
}

func (s *Source) run(ctx context.Context, status chan any, runDone chan struct{}, mc metrics.Collector) {
	defer close(runDone)
	defer close(status)

	ticker := time.NewTicker(s.statusInterval)
	defer ticker.Stop()

	for {
		select {
		case batch := <-s.commits:
			s.process(ctx, batch, mc)
		case <-ticker.C:
			s.onTick(ctx, mc)
		case <-ctx.Done():
			s.drainRemaining(ctx, mc)
			return
		}
	}
}

func (s *Source) drainRemaining(ctx context.Context, mc metrics.Collector) {
	for {
		select {
		case batch := <-s.commits:
			s.process(ctx, batch, mc)
		default:
			return
		}
	}
}

func (s *Source) process(ctx context.Context, batch []capturedChange, mc metrics.Collector) {
	for _, ch := range batch {
		cols := s.columnsFor(ctx, ch.table)
		op := opString(ch.op)
		seq := s.seq.Add(1)
		change := config.Change{
			Source:   s.name,
			Op:       op,
			Table:    ch.table,
			Relation: ch.table,
			Before:   mapRow(cols, ch.old),
			After:    mapRow(cols, ch.new),
			LSN:      strconv.FormatUint(seq, 10),
		}
		s.subs.publish(ctx, change)
		if mc != nil {
			mc.CounterInc(changesCounter, metrics.Labels{"source": s.name, "op": op})
		}
	}
}

func (s *Source) onTick(ctx context.Context, mc metrics.Collector) {
	if mc != nil {
		if info, err := os.Stat(s.file + "-wal"); err == nil {
			mc.GaugeSet(walGauge, float64(info.Size()), metrics.Labels{"source": s.name})
		}
	}
	if seq := s.seq.Load(); seq > 0 {
		if err := saveOffset(ctx, s.checkpointDB, s.name, seq); err != nil {
			s.log.Warn("persist cdc offset failed", zap.Error(err))
		}
	}
}

func (s *Source) columnsFor(ctx context.Context, table string) []columnInfo {
	s.colMu.RLock()
	cols, ok := s.cols[table]
	s.colMu.RUnlock()
	if ok {
		return cols
	}

	cols, err := resolveColumns(ctx, s.readDB, table)
	if err != nil {
		s.log.Warn("resolve columns failed; emitting positional column names",
			zap.String("table", table), zap.Error(err))
		return nil
	}
	s.colMu.Lock()
	s.cols[table] = cols
	s.colMu.Unlock()
	return cols
}

func opString(op int) string {
	switch op {
	case cdcInsert:
		return "insert"
	case cdcUpdate:
		return "update"
	case cdcDelete:
		return "delete"
	default:
		return "unknown"
	}
}

func openAuxConns(file string) (read, checkpoint *sql.DB, err error) {
	read, err = sql.Open("sqlite3", "file:"+file+"?mode=rwc&_busy_timeout="+strconv.Itoa(auxBusyTimeoutMillisec)+"&_query_only=ON")
	if err != nil {
		return nil, nil, fmt.Errorf("open read connection: %w", err)
	}
	read.SetMaxOpenConns(1)
	read.SetMaxIdleConns(1)

	checkpoint, err = sql.Open("sqlite3", "file:"+file+"?mode=rwc&_busy_timeout="+strconv.Itoa(auxBusyTimeoutMillisec))
	if err != nil {
		_ = read.Close()
		return nil, nil, fmt.Errorf("open checkpoint connection: %w", err)
	}
	checkpoint.SetMaxOpenConns(1)
	checkpoint.SetMaxIdleConns(1)
	return read, checkpoint, nil
}
