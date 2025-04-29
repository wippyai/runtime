package sqlstore

import (
	"context"
	sql2 "database/sql"
	"encoding/json"
	"fmt"
	"github.com/ponyruntime/pony/api/service/sqlstore"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/service/sql"
	"sync"
	"time"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/api/store"
	"go.uber.org/zap"
)

// SQLStore is a SQL-based implementation of the store.Store interface

const CleanupInterval = time.Second * 60

// that also functions as a resource.Provider
type SQLStore struct {
	id     registry.ID
	config *sqlstore.SQLConfig
	log    *zap.Logger
	mu     sync.RWMutex

	data       map[string]*store.Entry
	closed     bool
	statusChan chan any
	stopChan   chan struct{}
	wg         sync.WaitGroup // For tracking active goroutines
}

// NewSQLStore creates a new SQL-based key-value store
func NewSQLStore(id registry.ID, config *sqlstore.SQLConfig, log *zap.Logger) *SQLStore {
	if config == nil {
		config = &sqlstore.SQLConfig{}
		config.InitDefaults()
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
	reg := resource.GetResources(ctx)
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

	db := conn.(sql.DBResource).DB

	// Build query to retrieve value and check expiration
	query := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s = $1 AND (%s IS NULL OR %s > now())",
		s.config.PayloadColumnName,
		s.config.TableName,
		s.config.IDColumnName,
		s.config.ExpireColumnName,
		s.config.ExpireColumnName,
	)

	var data []byte
	err = db.QueryRowContext(ctx, query, key.String()).Scan(&data)
	if err != nil {
		if err == sql2.ErrNoRows {
			return nil, store.ErrKeyNotFound
		}
		s.log.Error("failed to execute get query",
			zap.String("error", err.Error()),
			zap.String("key", key.String()))
		return nil, err
	}
	p := payload.NewPayload(data, payload.JSON)
	err = json.Unmarshal(data, p)

	return p, nil
}

// Set stores or updates a value with the given key
// Overwrites any existing value if the key already exists
func (s *SQLStore) Set(ctx context.Context, entry store.Entry) error {

	reg := resource.GetResources(ctx)
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

	db := conn.(sql.DBResource).DB

	// Check if entry already exists
	existsQuery := fmt.Sprintf(
		"SELECT 1 FROM %s WHERE %s = $1",
		s.config.TableName,
		s.config.IDColumnName,
	)

	t := payload.GetTranscoder(ctx)
	value, err := t.Transcode(entry.Value, payload.JSON)
	valueBytes := value.Data()
	if err != nil {
		s.log.Error("failed to Transcode payload",
			zap.String("error", err.Error()),
			zap.String("resource", s.config.Database.Name))
		return err
	}

	s.log.Debug(fmt.Sprintf("%#v\n", valueBytes))

	var query string
	var args []interface{}

	// Determine expiration time if TTL is set
	var expiryTime *time.Time
	if entry.TTL > 0 {
		t := time.Now().Add(entry.TTL)
		expiryTime = &t
	}

	var exists bool
	err = db.QueryRowContext(ctx, existsQuery, entry.Key.String()).Scan(&exists)

	// Insert or update based on existence
	if err == sql2.ErrNoRows {
		// Insert a new entry
		query = fmt.Sprintf(
			"INSERT INTO %s (%s, %s, %s) VALUES ($1, $2, $3)",
			s.config.TableName,
			s.config.IDColumnName,
			s.config.PayloadColumnName,
			s.config.ExpireColumnName,
		)
		args = []interface{}{entry.Key.String(), valueBytes, nil}
	} else {
		// Update existing entry
		query = fmt.Sprintf(
			"UPDATE %s SET %s = $1, %s = $2 WHERE %s = $3",
			s.config.TableName,
			s.config.PayloadColumnName,
			s.config.ExpireColumnName,
			s.config.IDColumnName,
		)
		args = []interface{}{valueBytes, expiryTime, entry.Key.String()}
	}

	// Execute the query
	_, err = db.ExecContext(ctx, query, args...)
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
	// First, check if the key exists
	has, err := s.Has(ctx, key)
	if has {
		if err != nil {
			s.log.Error("failed to check if key exists",
				zap.String("error", err.Error()),
				zap.String("resource", s.config.Database.Name))
			return err
		}
	}

	reg := resource.GetResources(ctx)
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

	db := conn.(sql.DBResource).DB

	// Delete the key
	deleteQuery := fmt.Sprintf(
		"DELETE FROM %s WHERE %s = ?",
		s.config.TableName,
		s.config.IDColumnName,
	)

	_, err = db.ExecContext(ctx, deleteQuery, key.String())
	if err != nil {
		s.log.Error("failed to execute delete query",
			zap.String("error", err.Error()),
			zap.String("key", key.String()))
		return err
	}

	return nil
}

// Has checks if a key exists without retrieving the value
// Returns true if the key exists, false otherwise
func (s *SQLStore) Has(ctx context.Context, key registry.ID) (bool, error) {
	reg := resource.GetResources(ctx)
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

	db := conn.(sql.DBResource).DB

	// Build query to check if key exists and is not expired
	query := fmt.Sprintf(
		"SELECT 1 FROM %s WHERE %s = ? AND (%s IS NULL OR %s > datetime('now'))",
		s.config.TableName,
		s.config.IDColumnName,
		s.config.ExpireColumnName,
		s.config.ExpireColumnName,
	)

	var exists bool
	err = db.QueryRowContext(ctx, query, key.String()).Scan(&exists)
	if err != nil {
		if err == sql2.ErrNoRows {
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
func (s *SQLStore) Acquire(ctx context.Context, id registry.ID, mode resource.AccessMode) (resource.Resource[any], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Only support normal mode for now
	if mode != resource.ModeNormal {
		return nil, resource.ErrResourceLocked
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
		return nil, resource.ErrResourceReleased
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
	return
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

	reg := resource.GetResources(ctx)
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

	db := conn.(sql.DBResource).DB

	if s.closed {
		return
	}

	query := fmt.Sprintf(
		"DELETE FROM %s WHERE %s IS NOT NULL AND %s < now()",
		s.config.TableName,
		s.config.ExpireColumnName,
		s.config.ExpireColumnName,
	)

	ret, err := db.ExecContext(ctx, query)
	if err != nil {
		s.log.Error("failed to execute cleanup query",
			zap.String("error", err.Error()),
			zap.String("resource", s.config.Database.Name))
		return
	}
	rows, _ := ret.RowsAffected()

	s.log.Info(fmt.Sprintf("sqlstore store cleanup cycle. %d rows affected", rows), zap.String("time", time.Now().String()))
}

func (s *SQLStore) Start(ctx context.Context) (<-chan any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil, store.ErrStoreClosed
	}

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

// Ensure SQLStore implements all required interfaces
var (
	_ store.Store        = (*SQLStore)(nil)
	_ resource.Provider  = (*SQLStore)(nil)
	_ supervisor.Service = (*SQLStore)(nil)
)
