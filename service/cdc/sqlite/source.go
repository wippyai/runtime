// SPDX-License-Identifier: MPL-2.0

//go:build sqlite_preupdate_hook

package sqlite

import (
	"context"
	"database/sql"
	"errors"
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
	changesCounter         = "wippy_cdc_changes_total"
	walGauge               = "wippy_cdc_wal_size_bytes"
	defaultStatusInterval  = 30 * time.Second
	commitQueueSize        = 256
	maxTxnRows             = 200_000
	maxTxnBytes            = 128 << 20
	auxBusyTimeoutMillisec = 5000
	claimAttempts          = 40
	claimRetryDelay        = 50 * time.Millisecond
	cleanupTimeout         = 5 * time.Second
)

type capturedChange struct {
	schema string
	table  string
	old    []any
	new    []any
	op     int
	rowid  int64
	ncols  int
}

type Source struct {
	res            resource.Registry
	poolRes        resource.Resource[any]
	readDB         *sql.DB
	writerDB       *sql.DB
	runCtx         context.Context
	cancel         context.CancelFunc
	runDone        chan struct{}
	commits        chan []capturedChange
	faultCh        chan struct{}
	subs           *subscribers
	cols           map[string][]columnInfo
	tables         map[string]struct{}
	log            *zap.Logger
	faultMsg       atomic.Pointer[string]
	name           string
	file           string
	epoch          string
	dbResID        registry.ID
	pending        []capturedChange
	statusInterval time.Duration
	token          uint64
	pendingBytes   int
	maxRows        int
	maxBytes       int
	seq            atomic.Uint64
	schemaVer      atomic.Int64
	colMu          sync.RWMutex
	mu             sync.Mutex
	pendMu         sync.Mutex
	faultOnce      sync.Once
	stopped        atomic.Bool
	faulted        atomic.Bool
	defaultSnap    bool
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
		faultCh:        make(chan struct{}),
		maxRows:        maxTxnRows,
		maxBytes:       maxTxnBytes,
		defaultSnap:    opts.snapshot,
	}, nil
}

func (s *Source) Subscribe(opts config.StreamOptions) config.ChangeStream {
	wantSnapshot := opts.Snapshot || s.defaultSnap
	sub := s.subs.subscribe(s.name, opts, wantSnapshot)
	if faulted, reason := s.Faulted(); faulted {
		sub.fail(reason)
		return sub
	}
	if !wantSnapshot {
		return sub
	}

	s.mu.Lock()
	ctx := s.runCtx
	ready := s.readDB != nil && !s.stopped.Load()
	s.mu.Unlock()

	if ready && ctx != nil {
		go s.bootstrapSubscription(ctx, sub)
	} else {
		sub.finishSnapshot()
	}

	return sub
}

func (s *Source) closeSubscriptions() {
	s.subs.closeAll()
}

func (s *Source) Epoch() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.epoch
}

func (s *Source) Faulted() (bool, string) {
	if !s.faulted.Load() {
		return false, ""
	}

	msg := ""
	if p := s.faultMsg.Load(); p != nil {
		msg = *p
	}

	return true, msg
}

func (s *Source) fault(reason string) {
	s.faultOnce.Do(func() {
		s.faulted.Store(true)
		r := reason
		s.faultMsg.Store(&r)
		s.resetPending()
		close(s.faultCh)
	})
}

func (s *Source) resetPending() {
	s.pendMu.Lock()
	s.pending = nil
	s.pendingBytes = 0
	s.pendMu.Unlock()
}

func (s *Source) PreUpdate(op int, schema, table string, rowid int64, ncols int, old, new []any, scanErr error) {
	if s.faulted.Load() {
		return
	}
	if !schemaAllowed(schema) || !s.tableAllowed(table) {
		return
	}
	if scanErr != nil {
		s.fault("read preupdate row: " + scanErr.Error())
		return
	}

	s.pendMu.Lock()
	if len(s.pending) >= s.maxRows || s.pendingBytes >= s.maxBytes {
		s.pendMu.Unlock()
		s.fault(ErrChangeBacklogOverflow.Error())
		return
	}
	s.pending = append(s.pending, capturedChange{op: op, schema: schema, table: table, rowid: rowid, ncols: ncols, old: old, new: new})
	s.pendingBytes += approxRowSize(old) + approxRowSize(new)
	s.pendMu.Unlock()
}

func (s *Source) Commit() {
	if s.faulted.Load() {
		s.resetPending()
		return
	}

	s.pendMu.Lock()
	batch := s.pending
	s.pending = nil
	s.pendingBytes = 0
	s.pendMu.Unlock()
	if len(batch) == 0 {
		return
	}

	select {
	case s.commits <- batch:
	default:
		s.fault(ErrChangeBacklogOverflow.Error())
	}
}

func (s *Source) Rollback() {
	s.resetPending()
}

func schemaAllowed(schema string) bool {
	return schema == "" || strings.EqualFold(schema, "main")
}

func (s *Source) tableAllowed(table string) bool {
	if len(s.tables) == 0 {
		return true
	}
	_, ok := s.tables[strings.ToLower(table)]

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

	file, token, err := s.installWithRetry(ctx, conn)
	if err != nil {
		_ = conn.Close()
		res.Release()
		return nil, err
	}

	readDB, err := openReadConn(file)
	if err != nil {
		_ = conn.Close()
		s.detachHooks(ctx, writerDB, file, token)
		res.Release()
		return nil, err
	}
	_ = conn.Close()

	runCtx, cancel := context.WithCancel(ctx)
	status := make(chan any, 8)
	runDone := make(chan struct{})
	commits := make(chan []capturedChange, commitQueueSize)

	s.mu.Lock()
	if s.stopped.Load() {
		s.mu.Unlock()
		cancel()
		_ = readDB.Close()
		s.detachHooks(ctx, writerDB, file, token)
		res.Release()
		return nil, ErrSourceClosed
	}
	epoch := strconv.FormatInt(time.Now().UnixNano(), 10)
	s.poolRes = res
	s.writerDB = writerDB
	s.readDB = readDB
	s.file = file
	s.token = token
	s.epoch = epoch
	s.cancel = cancel
	s.runCtx = runCtx
	s.runDone = runDone
	s.commits = commits
	s.mu.Unlock()

	select {
	case status <- "sqlite cdc started":
	default:
	}

	go s.run(runCtx, status, runDone, metrics.GetCollector(ctx))

	s.log.Info("sqlite cdc source started", zap.String("file", file), zap.String("epoch", epoch))

	return status, nil
}

func (s *Source) installWithRetry(ctx context.Context, conn *sql.Conn) (string, uint64, error) {
	var file string
	var token uint64
	for attempt := 0; attempt < claimAttempts; attempt++ {
		err := conn.Raw(func(dc any) error {
			f, t, e := installHooksOnRaw(dc, s)
			file, token = f, t

			return e
		})
		if err == nil {
			return file, token, nil
		}
		if !errors.Is(err, errCaptureOwned) {
			return "", 0, err
		}

		select {
		case <-ctx.Done():
			return "", 0, ctx.Err()
		case <-time.After(claimRetryDelay):
		}
	}

	return "", 0, errCaptureOwned
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
	token := s.token
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if runDone != nil {
		select {
		case <-runDone:
		case <-ctx.Done():
			<-runDone
		}
	}

	if writerDB != nil {
		s.detachHooks(ctx, writerDB, file, token)
	}
	s.releaseResources()

	return nil
}

func (s *Source) detachHooks(ctx context.Context, writerDB *sql.DB, file string, token uint64) {
	releaseCapture(file, token)

	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()

	conn, err := writerDB.Conn(cleanupCtx)
	if err != nil {
		return
	}
	_ = conn.Raw(func(dc any) error { return applyOwnerOnRaw(dc, file) })
	_ = conn.Close()
}

func (s *Source) releaseResources() {
	s.mu.Lock()
	readDB := s.readDB
	res := s.poolRes
	s.readDB = nil
	s.poolRes = nil
	s.mu.Unlock()

	if readDB != nil {
		_ = readDB.Close()
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

	faultCh := s.faultCh
	for {
		select {
		case batch := <-s.commits:
			if !s.faulted.Load() {
				s.process(ctx, batch, mc)
			}
		case <-faultCh:
			s.emitFault(ctx)
			faultCh = nil
		case <-ticker.C:
			s.onTick(mc)
		case <-ctx.Done():
			if !s.faulted.Load() {
				s.drainRemaining(ctx, mc)
			}

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

func (s *Source) emitFault(ctx context.Context) {
	msg := "sqlite cdc source faulted"
	if p := s.faultMsg.Load(); p != nil {
		msg = *p
	}

	s.log.Error("sqlite cdc source faulted", zap.String("source", s.name), zap.String("reason", msg))
	s.subs.publish(ctx, config.Change{Source: s.name, Op: "error", Error: msg})
}

func (s *Source) process(ctx context.Context, batch []capturedChange, mc metrics.Collector) {
	s.refreshSchemaVersion(ctx)
	for _, ch := range batch {
		cols := s.columnsFor(ctx, ch.table)
		if ch.ncols > 0 && len(cols) > 0 && len(cols) != ch.ncols {
			s.invalidateColumns(ch.table)
			cols = s.columnsFor(ctx, ch.table)
		}

		op := opString(ch.op)
		seq := s.seq.Add(1)
		change := config.Change{
			Source:   s.name,
			Op:       op,
			Schema:   normalizeSchema(ch.schema),
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

func (s *Source) refreshSchemaVersion(ctx context.Context) {
	var ver int64
	if err := s.readDB.QueryRowContext(ctx, "PRAGMA schema_version").Scan(&ver); err != nil {
		return
	}

	prev := s.schemaVer.Swap(ver)
	if prev != 0 && prev != ver {
		s.colMu.Lock()
		s.cols = make(map[string][]columnInfo)
		s.colMu.Unlock()
	}
}

func (s *Source) invalidateColumns(table string) {
	s.colMu.Lock()
	delete(s.cols, table)
	s.colMu.Unlock()
}

func (s *Source) onTick(mc metrics.Collector) {
	if mc == nil {
		return
	}
	if info, err := os.Stat(s.file + "-wal"); err == nil {
		mc.GaugeSet(walGauge, float64(info.Size()), metrics.Labels{"source": s.name})
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

func normalizeSchema(schema string) string {
	if schema == "" {
		return "main"
	}

	return schema
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

func openReadConn(file string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", "file:"+file+"?mode=rwc&_busy_timeout="+strconv.Itoa(auxBusyTimeoutMillisec)+"&_query_only=ON")
	if err != nil {
		return nil, fmt.Errorf("open read connection: %w", err)
	}

	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)

	return db, nil
}

func approxRowSize(vals []any) int {
	size := 0
	for _, v := range vals {
		switch t := v.(type) {
		case []byte:
			size += len(t)
		case string:
			size += len(t)
		default:
			size += 8
		}
	}

	return size
}

func openSnapshotConn(file string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", "file:"+file+"?mode=rwc&_busy_timeout="+strconv.Itoa(auxBusyTimeoutMillisec)+"&_query_only=ON")
	if err != nil {
		return nil, fmt.Errorf("open snapshot connection: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	return db, nil
}
