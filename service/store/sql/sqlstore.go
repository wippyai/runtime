// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	sq "github.com/Masterminds/squirrel"
	sqlconfig "github.com/wippyai/runtime/api/service/sql"
	sqlstore "github.com/wippyai/runtime/api/service/store/sql"
	"github.com/wippyai/runtime/api/supervisor"
	servicesql "github.com/wippyai/runtime/service/sql"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/store"
	servicestore "github.com/wippyai/runtime/service/store"
	systemresource "github.com/wippyai/runtime/system/resource"
	"go.uber.org/zap"
)

var (
	_ store.Store        = (*Store)(nil)
	_ store.InfoProvider = (*Store)(nil)
	_ store.EntryReader  = (*Store)(nil)
	_ store.Lister       = (*Store)(nil)
	_ store.Putter       = (*Store)(nil)
	_ resource.Provider  = (*Store)(nil)
	_ supervisor.Service = (*Store)(nil)
)

// Store is a SQL-backed key-value store that also functions as a resource.Provider
type Store struct {
	config     *sqlstore.Config
	log        *zap.Logger
	statusChan chan any
	stopChan   chan struct{}
	id         registry.ID
	wg         sync.WaitGroup
	mu         sync.RWMutex
	closed     bool
}

// dbHandle holds an acquired database connection and its release function.
type dbHandle struct {
	db      *sql.DB
	release func()
	dbType  registry.Kind
}

// acquireDB acquires a database connection from the resource registry.
// Returns a dbHandle that must have release() called when done.
func (s *Store) acquireDB(ctx context.Context) (*dbHandle, error) {
	reg := resource.GetRegistry(ctx)
	res, err := reg.Acquire(ctx, s.config.Database, resource.ModeNormal)
	if err != nil {
		s.log.Error("failed to acquire database resource",
			zap.String("error", err.Error()),
			zap.String("resource", s.config.Database.Name))
		return nil, err
	}

	conn, err := res.Get()
	if err != nil {
		res.Release()
		s.log.Error("failed to get database connection",
			zap.String("error", err.Error()),
			zap.String("resource", s.config.Database.Name))
		return nil, err
	}

	dbRes, ok := conn.(servicesql.DBResource)
	if !ok {
		res.Release()
		return nil, ErrInvalidResourceType
	}

	return &dbHandle{
		db:      dbRes.DB,
		dbType:  dbRes.Type,
		release: res.Release,
	}, nil
}

// NewStore creates a new SQL-based key-value store
func NewStore(id registry.ID, config *sqlstore.Config, log *zap.Logger) *Store {
	if config == nil {
		config = &sqlstore.Config{}
	}
	if log == nil {
		log = zap.NewNop()
	}

	return &Store{
		id:       id,
		config:   config,
		log:      log.With(zap.String("component", "sqlstore"), zap.String("id", id.String())),
		stopChan: make(chan struct{}),
	}
}

// Get retrieves a value by key
// Returns the payload associated with the given registry.ID or ErrKeyNotFound if not present
func (s *Store) Get(ctx context.Context, key registry.ID) (payload.Payload, error) {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return nil, servicestore.ErrStoreClosed
	}
	s.mu.RUnlock()

	h, err := s.acquireDB(ctx)
	if err != nil {
		return nil, err
	}
	defer h.release()

	qb := statementBuilder(h.dbType)

	// Build query to retrieve value and check expiration
	query := qb.
		Select(s.config.PayloadColumnName).
		From(s.config.TableName).
		Where(sq.Eq{s.config.IDColumnName: key.String()}).
		Where(sq.Or{
			sq.Eq{s.config.ExpireColumnName: nil},
			sq.Gt{s.config.ExpireColumnName: time.Now().UTC()},
		})

	querySQL, args, err := query.ToSql()
	if err != nil {
		s.log.Error("failed to build get query",
			zap.String("error", err.Error()),
			zap.String("key", key.String()))
		return nil, err
	}

	var data []byte
	err = h.db.QueryRowContext(ctx, querySQL, args...).Scan(&data)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrKeyNotFound
		}
		s.log.Error("failed to execute get query",
			zap.String("error", err.Error()),
			zap.String("key", key.String()))
		return nil, err
	}
	p := payload.NewPayload(data, payload.JSON)

	return p, nil
}

// Entry retrieves a value with metadata. The current SQL schema has no version
// column, so Version is always 0 and StoreInfo reports Versioned=false.
func (s *Store) Entry(ctx context.Context, key registry.ID) (store.VersionedEntry, error) {
	value, err := s.Get(ctx, key)
	if err != nil {
		return store.VersionedEntry{}, err
	}
	return store.VersionedEntry{Entry: store.Entry{Key: key, Value: value}}, nil
}

// Set stores or updates a value with the given key
// Overwrites any existing value if the key already exists
func (s *Store) Set(ctx context.Context, entry store.Entry) error {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return servicestore.ErrStoreClosed
	}
	s.mu.RUnlock()

	h, err := s.acquireDB(ctx)
	if err != nil {
		return err
	}
	defer h.release()

	qb := statementBuilder(h.dbType)

	// Check if entry already exists
	existsQuery := qb.
		Select("1").
		From(s.config.TableName).
		Where(sq.Eq{s.config.IDColumnName: entry.Key.String()})

	existsSQL, existsArgs, err := existsQuery.ToSql()
	if err != nil {
		s.log.Error("failed to build exists query",
			zap.String("error", err.Error()),
			zap.String("key", entry.Key.String()))
		return err
	}

	t := payload.GetTranscoder(ctx)
	value, err := t.Transcode(entry.Value, payload.JSON)
	if err != nil {
		s.log.Error("failed to Transcode payload",
			zap.String("error", err.Error()),
			zap.String("resource", s.config.Database.Name))
		return err
	}
	valueBytes := value.Data()

	// Determine expiration time if TTL is set
	var expiryDate *time.Time
	if entry.TTL > 0 {
		t := time.Now().Add(entry.TTL).UTC()
		expiryDate = &t
	}

	var exists bool
	err = h.db.QueryRowContext(ctx, existsSQL, existsArgs...).Scan(&exists)

	var querySQL string
	var args []any

	// Insert or update based on existence
	if errors.Is(err, sql.ErrNoRows) {
		// Insert a new entry
		insertQuery := qb.
			Insert(s.config.TableName).
			Columns(s.config.IDColumnName, s.config.PayloadColumnName, s.config.ExpireColumnName).
			Values(entry.Key.String(), valueBytes, expiryDate)

		querySQL, args, err = insertQuery.ToSql()
		if err != nil {
			s.log.Error("failed to build insert query",
				zap.String("error", err.Error()),
				zap.String("key", entry.Key.String()))
			return err
		}
	} else {
		// Update existing entry
		updateQuery := qb.
			Update(s.config.TableName).
			Set(s.config.PayloadColumnName, valueBytes).
			Set(s.config.ExpireColumnName, expiryDate).
			Where(sq.Eq{s.config.IDColumnName: entry.Key.String()})

		querySQL, args, err = updateQuery.ToSql()
		if err != nil {
			s.log.Error("failed to build update query",
				zap.String("error", err.Error()),
				zap.String("key", entry.Key.String()))
			return err
		}
	}

	// Execute the query
	_, err = h.db.ExecContext(ctx, querySQL, args...)
	if err != nil {
		s.log.Error("failed to execute set query",
			zap.String("error", err.Error()),
			zap.String("key", entry.Key.String()))
	}

	return err
}

// Put stores a value and returns the resulting entry. Conditional puts are not
// exposed for the SQL store because the current table schema is unversioned.
func (s *Store) Put(ctx context.Context, key registry.ID, value payload.Payload, opts store.PutOptions) (store.VersionedEntry, error) {
	if opts.TTL < 0 {
		return store.VersionedEntry{}, store.ErrInvalidOptions
	}
	if opts.OnlyIfAbsent || opts.HasVersion {
		return store.VersionedEntry{}, store.ErrUnsupported
	}
	entry := store.Entry{Key: key, Value: value, TTL: opts.TTL}
	if err := s.Set(ctx, entry); err != nil {
		return store.VersionedEntry{}, err
	}
	return store.VersionedEntry{Entry: entry}, nil
}

// Delete removes a value with the given key
// Returns ErrKeyNotFound if the key doesn't exist
func (s *Store) Delete(ctx context.Context, key registry.ID) error {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return servicestore.ErrStoreClosed
	}
	s.mu.RUnlock()

	h, err := s.acquireDB(ctx)
	if err != nil {
		return err
	}
	defer h.release()

	qb := statementBuilder(h.dbType)

	// Delete the key
	deleteQuery := qb.
		Delete(s.config.TableName).
		Where(sq.Eq{s.config.IDColumnName: key.String()})

	querySQL, args, err := deleteQuery.ToSql()
	if err != nil {
		s.log.Error("failed to build delete query",
			zap.String("error", err.Error()),
			zap.String("key", key.String()))
		return err
	}

	result, err := h.db.ExecContext(ctx, querySQL, args...)
	if err != nil {
		s.log.Error("failed to execute delete query",
			zap.String("error", err.Error()),
			zap.String("key", key.String()))
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return store.ErrKeyNotFound
	}

	return nil
}

// Has checks if a key exists without retrieving the value
// Returns true if the key exists, false otherwise
func (s *Store) Has(ctx context.Context, key registry.ID) (bool, error) {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return false, servicestore.ErrStoreClosed
	}
	s.mu.RUnlock()

	h, err := s.acquireDB(ctx)
	if err != nil {
		return false, err
	}
	defer h.release()

	qb := statementBuilder(h.dbType)

	// Build query to check if key exists and is not expired
	query := qb.
		Select("1").
		From(s.config.TableName).
		Where(sq.Eq{s.config.IDColumnName: key.String()}).
		Where(sq.Or{
			sq.Eq{s.config.ExpireColumnName: nil},
			sq.Gt{s.config.ExpireColumnName: time.Now().UTC()},
		})

	querySQL, args, err := query.ToSql()
	if err != nil {
		s.log.Error("failed to build has query",
			zap.String("error", err.Error()),
			zap.String("key", key.String()))
		return false, err
	}

	var exists bool
	err = h.db.QueryRowContext(ctx, querySQL, args...).Scan(&exists)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		s.log.Error("failed to execute has query",
			zap.String("error", err.Error()),
			zap.String("key", key.String()))
		return false, err
	}

	return true, nil
}

// List returns a deterministic key-ordered page of non-expired entries.
func (s *Store) List(ctx context.Context, opts store.ListOptions) (store.Page, error) {
	opts = store.NormalizeListOptions(opts)

	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return store.Page{}, servicestore.ErrStoreClosed
	}
	s.mu.RUnlock()

	h, err := s.acquireDB(ctx)
	if err != nil {
		return store.Page{}, err
	}
	defer h.release()

	qb := statementBuilder(h.dbType)
	query := qb.
		Select(s.config.IDColumnName, s.config.PayloadColumnName).
		From(s.config.TableName).
		Where(sq.Or{
			sq.Eq{s.config.ExpireColumnName: nil},
			sq.Gt{s.config.ExpireColumnName: time.Now().UTC()},
		}).
		OrderBy(s.config.IDColumnName + " ASC").
		Limit(uint64(opts.Limit + 1))

	if opts.Prefix != "" {
		query = query.Where(sq.Expr(
			s.config.IDColumnName+" LIKE ? ESCAPE '\\'",
			escapeSQLLike(opts.Prefix)+"%",
		))
	}
	if opts.After != "" {
		query = query.Where(sq.Gt{s.config.IDColumnName: opts.After})
	}

	querySQL, args, err := query.ToSql()
	if err != nil {
		s.log.Error("failed to build list query", zap.String("error", err.Error()))
		return store.Page{}, err
	}

	rows, err := h.db.QueryContext(ctx, querySQL, args...)
	if err != nil {
		s.log.Error("failed to execute list query", zap.String("error", err.Error()))
		return store.Page{}, err
	}
	defer func() { _ = rows.Close() }()

	items := make([]store.VersionedEntry, 0, opts.Limit+1)
	for rows.Next() {
		var key string
		var data []byte
		if err := rows.Scan(&key, &data); err != nil {
			return store.Page{}, err
		}
		items = append(items, store.VersionedEntry{
			Entry: store.Entry{
				Key:   registry.ParseID(key),
				Value: payload.NewPayload(data, payload.JSON),
			},
		})
	}
	if err := rows.Err(); err != nil {
		return store.Page{}, err
	}

	hasMore := len(items) > opts.Limit
	if hasMore {
		items = items[:opts.Limit]
	}
	cursor := ""
	if len(items) > 0 {
		cursor = items[len(items)-1].Key.String()
	}
	return store.Page{Items: items, Cursor: cursor, HasMore: hasMore}, nil
}

// StoreInfo reports SQL store capabilities for Lua callers.
func (s *Store) StoreInfo(_ context.Context) store.Info {
	return store.Info{
		ID:             s.id,
		Backend:        store.BackendSQL,
		Consistency:    store.ConsistencyLocal,
		Durable:        true,
		List:           true,
		Versioned:      false,
		ConditionalPut: false,
		TTL:            true,
	}
}

// Acquire implements resource.Provider interface
func (s *Store) Acquire(_ context.Context, _ registry.ID, mode resource.AccessMode) (resource.Resource[any], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Only support normal mode for now
	if mode != resource.ModeNormal {
		return nil, systemresource.ErrLocked
	}

	return &storeResource{store: s}, nil
}

// storeResource represents an acquired store resource
type storeResource struct {
	store  *Store
	closed bool
	mu     sync.Mutex
}

// Get implements resource.Resource interface
func (r *storeResource) Get() (any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil, resource.ErrReleased
	}

	return store.Store(r.store), nil
}

// Release implements resource.Resource interface
func (r *storeResource) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return
	}

	r.closed = true
}

func (s *Store) cleanupLoop(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(s.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.cleanup(ctx)
		case <-s.stopChan:
			s.log.Debug("cleanup routine stopped")
			return
		case <-ctx.Done():
			s.log.Debug("cleanup routine stopped by context")
			return
		}
	}
}

func (s *Store) cleanup(ctx context.Context) {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return
	}
	s.mu.RUnlock()

	h, err := s.acquireDB(ctx)
	if err != nil {
		return
	}
	defer h.release()

	qb := statementBuilder(h.dbType)

	// Build cleanup query using Squirrel
	cleanupQuery := qb.
		Delete(s.config.TableName).
		Where(sq.NotEq{s.config.ExpireColumnName: nil}).
		Where(sq.Lt{s.config.ExpireColumnName: time.Now().UTC()})

	querySQL, args, err := cleanupQuery.ToSql()
	if err != nil {
		s.log.Error("failed to build cleanup query",
			zap.String("error", err.Error()),
			zap.String("resource", s.config.Database.Name))
		return
	}

	ret, err := h.db.ExecContext(ctx, querySQL, args...)
	if err != nil {
		s.log.Error("failed to execute cleanup query",
			zap.String("error", err.Error()),
			zap.String("resource", s.config.Database.Name))
		return
	}
	rows, _ := ret.RowsAffected()

	if rows > 0 {
		s.log.Info(fmt.Sprintf("sqlstore store cleanup cycle. %d rows affected", rows), zap.String("time", time.Now().String()))
	}
}

func (s *Store) Start(ctx context.Context) (<-chan any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil, servicestore.ErrStoreClosed
	}

	s.statusChan = make(chan any, 1)

	if s.config.CleanupInterval > 0 {
		s.wg.Add(1)
		go s.cleanupLoop(ctx)
		s.log.Info("started cleanup routine",
			zap.Duration("interval", s.config.CleanupInterval))
	}

	select {
	case s.statusChan <- "sqlstore store started":
	default:
	}

	return s.statusChan, nil
}

func (s *Store) Stop(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}

	s.closed = true
	close(s.stopChan)
	s.mu.Unlock()
	// Wait for cleanup goroutine to finish with timeout
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.log.Info("sqlstore store stopped cleanly")
		return nil
	case <-ctx.Done():
		s.log.Warn("sqlstore store stop timed out")
		return ctx.Err()
	}
}

// statementBuilder returns a squirrel query builder with appropriate placeholder format
func statementBuilder(dbType registry.Kind) sq.StatementBuilderType {
	switch dbType {
	case sqlconfig.Postgres:
		return sq.StatementBuilder.PlaceholderFormat(sq.Dollar)
	case sqlconfig.MySQL, sqlconfig.SQLite:
		return sq.StatementBuilder.PlaceholderFormat(sq.Question)
	default:
		// Default to PostgreSQL-style for unknown types
		return sq.StatementBuilder.PlaceholderFormat(sq.Dollar)
	}
}

func escapeSQLLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}
