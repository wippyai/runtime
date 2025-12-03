package sql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	"go.uber.org/zap"
)

// SQLStore that also functions as a resource.Provider
type SQLStore struct {
	id     registry.ID
	config *sqlstore.SQLConfig
	log    *zap.Logger
	mu     sync.RWMutex

	closed     bool
	statusChan chan any
	stopChan   chan struct{}
	wg         sync.WaitGroup // For tracking active goroutines
}

// NewSQLStore creates a new SQL-based key-value store
func NewSQLStore(id registry.ID, config *sqlstore.SQLConfig, log *zap.Logger) *SQLStore {
	if config == nil {
		config = &sqlstore.SQLConfig{}
	}

	return &SQLStore{
		id:       id,
		config:   config,
		log:      log.With(zap.String("component", "sqlstore"), zap.String("id", id.String())),
		stopChan: make(chan struct{}),
	}
}

// Get retrieves a value by key
// Returns the payload associated with the given registry.ID or ErrKeyNotFound if not present
func (s *SQLStore) Get(ctx context.Context, key registry.ID) (payload.Payload, error) {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return nil, store.ErrStoreClosed
	}
	s.mu.RUnlock()

	reg := resource.GetRegistry(ctx)
	res, err := reg.Acquire(ctx, s.config.Database, resource.ModeNormal)
	if err != nil {
		s.log.Error("failed to acquire database resource",
			zap.String("error", err.Error()),
			zap.String("resource", s.config.Database.Name))
		return nil, err
	}
	defer res.Release()

	conn, err := res.Get()
	if err != nil {
		s.log.Error("failed to get database connection",
			zap.String("error", err.Error()),
			zap.String("resource", s.config.Database.Name))
		return nil, err
	}

	db := conn.(servicesql.DBResource).DB
	dbType := conn.(servicesql.DBResource).Type
	qb := statementBuilder(dbType)

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
	err = db.QueryRowContext(ctx, querySQL, args...).Scan(&data)
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

// Set stores or updates a value with the given key
// Overwrites any existing value if the key already exists
func (s *SQLStore) Set(ctx context.Context, entry store.Entry) error {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return store.ErrStoreClosed
	}
	s.mu.RUnlock()

	reg := resource.GetRegistry(ctx)
	res, err := reg.Acquire(ctx, s.config.Database, resource.ModeNormal)
	if err != nil {
		s.log.Error("failed to acquire database resource",
			zap.String("error", err.Error()),
			zap.String("resource", s.config.Database.Name))
		return err
	}
	defer res.Release()

	conn, err := res.Get()
	if err != nil {
		s.log.Error("failed to get database connection",
			zap.String("error", err.Error()),
			zap.String("resource", s.config.Database.Name))
		return err
	}

	db := conn.(servicesql.DBResource).DB
	dbType := conn.(servicesql.DBResource).Type
	qb := statementBuilder(dbType)

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
	err = db.QueryRowContext(ctx, existsSQL, existsArgs...).Scan(&exists)

	var querySQL string
	var args []interface{}

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
	_, err = db.ExecContext(ctx, querySQL, args...)
	if err != nil {
		s.log.Error("failed to execute set query",
			zap.String("error", err.Error()),
			zap.String("key", entry.Key.String()))
	}

	return err
}

// Delete removes a value with the given key
// Returns ErrKeyNotFound if the key doesn't exist
func (s *SQLStore) Delete(ctx context.Context, key registry.ID) error {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return store.ErrStoreClosed
	}
	s.mu.RUnlock()

	reg := resource.GetRegistry(ctx)
	res, err := reg.Acquire(ctx, s.config.Database, resource.ModeNormal)
	if err != nil {
		s.log.Error("failed to acquire database resource",
			zap.String("error", err.Error()),
			zap.String("resource", s.config.Database.Name))
		return err
	}
	defer res.Release()

	conn, err := res.Get()
	if err != nil {
		s.log.Error("failed to get database connection",
			zap.String("error", err.Error()),
			zap.String("resource", s.config.Database.Name))
		return err
	}

	db := conn.(servicesql.DBResource).DB
	dbType := conn.(servicesql.DBResource).Type
	qb := statementBuilder(dbType)

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

	result, err := db.ExecContext(ctx, querySQL, args...)
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
func (s *SQLStore) Has(ctx context.Context, key registry.ID) (bool, error) {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return false, store.ErrStoreClosed
	}
	s.mu.RUnlock()

	reg := resource.GetRegistry(ctx)
	res, err := reg.Acquire(ctx, s.config.Database, resource.ModeNormal)
	if err != nil {
		s.log.Error("failed to acquire database resource",
			zap.String("error", err.Error()),
			zap.String("resource", s.config.Database.Name))
		return false, err
	}
	defer res.Release()

	conn, err := res.Get()
	if err != nil {
		s.log.Error("failed to get database connection",
			zap.String("error", err.Error()),
			zap.String("resource", s.config.Database.Name))
		return false, err
	}

	db := conn.(servicesql.DBResource).DB
	dbType := conn.(servicesql.DBResource).Type
	qb := statementBuilder(dbType)

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
	err = db.QueryRowContext(ctx, querySQL, args...).Scan(&exists)
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

// Acquire implements resource.Provider interface
func (s *SQLStore) Acquire(_ context.Context, _ registry.ID, mode resource.AccessMode) (resource.Resource[any], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Only support normal mode for now
	if mode != resource.ModeNormal {
		return nil, resource.ErrLocked
	}

	return &storeResource{store: s}, nil
}

// storeResource represents an acquired store resource
type storeResource struct {
	store  *SQLStore
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

func (s *SQLStore) cleanupLoop(ctx context.Context) {
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

func (s *SQLStore) cleanup(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	reg := resource.GetRegistry(ctx)
	res, err := reg.Acquire(ctx, s.config.Database, resource.ModeNormal)
	if err != nil {
		s.log.Error("failed to acquire database resource",
			zap.String("error", err.Error()),
			zap.String("resource", s.config.Database.Name))
		return
	}
	defer res.Release()

	conn, err := res.Get()
	if err != nil {
		s.log.Error("failed to get database connection",
			zap.String("error", err.Error()),
			zap.String("resource", s.config.Database.Name))
		return
	}

	db := conn.(servicesql.DBResource).DB
	dbType := conn.(servicesql.DBResource).Type

	if s.closed {
		return
	}

	qb := statementBuilder(dbType)

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

	ret, err := db.ExecContext(ctx, querySQL, args...)
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

func (s *SQLStore) Start(ctx context.Context) (<-chan any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil, store.ErrStoreClosed
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

func (s *SQLStore) Stop(ctx context.Context) error {
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
	case sqlconfig.KindPostgres:
		return sq.StatementBuilder.PlaceholderFormat(sq.Dollar)
	case sqlconfig.KindMySQL, sqlconfig.KindSQLite, sqlconfig.KindMSSQL:
		return sq.StatementBuilder.PlaceholderFormat(sq.Question)
	case sqlconfig.KindOracle:
		return sq.StatementBuilder.PlaceholderFormat(sq.Colon)
	default:
		// Default to PostgreSQL-style for unknown types
		return sq.StatementBuilder.PlaceholderFormat(sq.Dollar)
	}
}

// Ensure SQLStore implements all required interfaces
var (
	_ store.Store        = (*SQLStore)(nil)
	_ resource.Provider  = (*SQLStore)(nil)
	_ supervisor.Service = (*SQLStore)(nil)
)
