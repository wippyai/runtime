// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	sqlconfig "github.com/wippyai/runtime/api/service/sql"
	sqlstore "github.com/wippyai/runtime/api/service/store/sql"
	"github.com/wippyai/runtime/api/store"
	sqlsvc "github.com/wippyai/runtime/service/sql"
	servicestore "github.com/wippyai/runtime/service/store"
	systemresource "github.com/wippyai/runtime/system/resource"
	"go.uber.org/zap"
)

// MockResource implements resource.Resource[any] interface for testing
type MockResource struct {
	value any
	err   error
}

func (r *MockResource) Get() (any, error) {
	return r.value, r.err
}

func (r *MockResource) Release() {
	// No-op for testing
}

// MockRegistry implements resource.Registry interface for testing
type MockRegistry struct {
	resources map[registry.ID]resource.Resource[any]
	errors    map[registry.ID]error
}

func NewMockRegistry() *MockRegistry {
	return &MockRegistry{
		resources: make(map[registry.ID]resource.Resource[any]),
		errors:    make(map[registry.ID]error),
	}
}

func (r *MockRegistry) Acquire(_ context.Context, id registry.ID, _ resource.AccessMode) (resource.Resource[any], error) {
	if err, exists := r.errors[id]; exists {
		return nil, err
	}
	res, exists := r.resources[id]
	if !exists {
		return nil, errors.New("resource not found")
	}
	return res, nil
}

func (r *MockRegistry) List() ([]registry.ID, error) {
	ids := make([]registry.ID, 0, len(r.resources))
	for id := range r.resources {
		ids = append(ids, id)
	}
	return ids, nil
}

func (r *MockRegistry) Exists(id registry.ID) bool {
	_, ok := r.resources[id]
	return ok
}

// MockPayload implements payload.Payload for testing
type MockPayload struct {
	data   any
	format payload.Format
}

func (p *MockPayload) Data() any {
	return p.data
}

func (p *MockPayload) Format() payload.Format {
	return p.format
}

func (p *MockPayload) Transcode(format payload.Format) (payload.Payload, error) {
	return &MockPayload{data: p.data, format: format}, nil
}

// MockTranscoder implements payload.Transcoder for testing
type MockTranscoder struct{}

func (t *MockTranscoder) Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func (t *MockTranscoder) Unmarshal(p payload.Payload, v any) error {
	data, ok := p.Data().([]byte)
	if !ok {
		return errors.New("expected []byte payload")
	}
	return json.Unmarshal(data, v)
}

func (t *MockTranscoder) Transcode(p payload.Payload, format payload.Format) (payload.Payload, error) {
	if p.Format() == format {
		return p, nil
	}
	return &MockPayload{data: p.Data(), format: format}, nil
}

// setupTestDB creates a in-memory SQLite database for testing
func setupTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite3", "file:memdb1?mode=memory&cache=shared")
	require.NoError(t, err)
	return db
}

// setupSQLStoreTable creates the table required by Store
func setupSQLStoreTable(t *testing.T, db *sql.DB, config *sqlstore.Config) {
	ctx := t.Context()

	createTable := `CREATE TABLE IF NOT EXISTS ` + config.TableName + ` (
		` + config.IDColumnName + ` TEXT PRIMARY KEY,
		` + config.PayloadColumnName + ` BLOB NOT NULL,
		` + config.ExpireColumnName + ` TIMESTAMP NULL
	)`
	_, err := db.ExecContext(ctx, createTable)
	require.NoError(t, err)
}

// insertTestData inserts test key-value pairs into the database
func insertTestData(t *testing.T, db *sql.DB, config *sqlstore.Config, key string, value []byte, expire *time.Time) {
	ctx := t.Context()

	var expireVal any
	if expire != nil {
		expireVal = expire.UTC()
	} else {
		expireVal = nil
	}

	query := `INSERT INTO ` + config.TableName + ` (` +
		config.IDColumnName + `, ` +
		config.PayloadColumnName + `, ` +
		config.ExpireColumnName + `) VALUES (?, ?, ?)`

	_, err := db.ExecContext(ctx, query, key, value, expireVal)
	require.NoError(t, err)
}

// createDefaultConfig creates a default Store config for testing
func createDefaultConfig() *sqlstore.Config {
	return &sqlstore.Config{
		Database:          registry.NewID("test", "db"),
		TableName:         "kv_store",
		IDColumnName:      "key_id",
		PayloadColumnName: "value",
		ExpireColumnName:  "expires_at",
		CleanupInterval:   time.Hour,
	}
}

// createContext creates a context with the given registry
func createContext(reg resource.Registry) context.Context {
	ctx := ctxapi.NewRootContext()
	return resource.WithRegistry(ctx, reg)
}

// createTranscoderContext creates a context with the given transcoder
func createTranscoderContext(ctx context.Context) context.Context {
	return payload.WithTranscoder(ctx, &MockTranscoder{})
}

func createTestEntry(key string, value any) store.Entry {
	return store.Entry{
		Key:   registry.ParseID(key),
		Value: payload.New(value),
	}
}

func MakeStore(t *testing.T) (*Store, *sql.DB, context.Context) {
	// Create a default config
	config := createDefaultConfig()

	// Set up database
	db := setupTestDB(t)
	setupSQLStoreTable(t, db, config)

	// Set up registry with working resource
	mockReg := NewMockRegistry()
	mockReg.resources[config.Database] = &MockResource{
		value: sqlsvc.DBResource{
			DB:   db,
			Type: sqlconfig.SQLite,
		},
		err: nil,
	}

	// Create context
	ctx := createContext(mockReg)
	ctx = createTranscoderContext(ctx)

	// Create store
	logger := zap.NewNop()

	return NewStore(registry.NewID("test", "store"), config, logger), db, ctx
}

func TestSQLStore_Delete(t *testing.T) {
	ss, db, ctx := MakeStore(t)
	defer func() { _ = db.Close() }()

	// Set a test value
	testKey := registry.ParseID("test:key1")
	testValue := "test value"
	err := ss.Set(ctx, createTestEntry("test:key1", testValue))
	require.NoError(t, err)

	exists, err := ss.Has(ctx, testKey)
	require.NoError(t, err)
	require.True(t, exists)

	// Delete existing key
	err = ss.Delete(ctx, testKey)
	require.NoError(t, err)

	exists, err = ss.Has(ctx, testKey)
	require.NoError(t, err)
	require.False(t, exists)

	// Delete non-existent key should return ErrKeyNotFound
	err = ss.Delete(ctx, registry.ParseID("test:nonexistent"))
	assert.Equal(t, store.ErrKeyNotFound, err)
}

func TestSQLStore_Has(t *testing.T) {
	ss, db, ctx := MakeStore(t)
	defer func() { _ = db.Close() }()

	// Set a test value
	testKey := registry.ParseID("test:key1")
	testValue := "test value"
	err := ss.Set(ctx, createTestEntry("test:key1", testValue))
	require.NoError(t, err)

	result, err := ss.Has(ctx, testKey)
	require.NoError(t, err)
	require.True(t, result)

	testKey = registry.ParseID("test:key2")
	result, err = ss.Has(ctx, testKey)
	require.NoError(t, err)
	require.False(t, result)
}

func TestSQLStore_SetGet(t *testing.T) {
	ss, db, ctx := MakeStore(t)
	defer func() { _ = db.Close() }()

	// Set a test value
	testKey := registry.ParseID("test:key1")
	testValue := "test value"
	err := ss.Set(ctx, createTestEntry("test:key1", testValue))
	require.NoError(t, err)

	result, err := ss.Get(ctx, testKey)

	require.NoError(t, err)
	data, ok := result.Data().([]byte)
	require.True(t, ok)

	assert.Equal(t, testValue, string(data))

	testKey = registry.ParseID("test:keynone")
	result, err = ss.Get(ctx, testKey)
	assert.True(t, errors.Is(err, store.ErrKeyNotFound))
	assert.Nil(t, result)
}

func TestSQLStore_InfoEntryListPut(t *testing.T) {
	ss, db, ctx := MakeStore(t)
	defer func() { _ = db.Close() }()

	info := ss.StoreInfo(ctx)
	assert.Equal(t, registry.NewID("test", "store"), info.ID)
	assert.Equal(t, store.BackendSQL, info.Backend)
	assert.Equal(t, store.ConsistencyLocal, info.Consistency)
	assert.True(t, info.Durable)
	assert.True(t, info.List)
	assert.False(t, info.Versioned)
	assert.False(t, info.ConditionalPut)
	assert.True(t, info.TTL)

	_, err := ss.Put(ctx, registry.ParseID("test:item-b"), payload.New("b"), store.PutOptions{OnlyIfAbsent: true})
	assert.ErrorIs(t, err, store.ErrUnsupported)
	_, err = ss.Put(ctx, registry.ParseID("test:bad-ttl"), payload.New("bad"), store.PutOptions{TTL: -time.Second})
	assert.ErrorIs(t, err, store.ErrInvalidOptions)

	put, err := ss.Put(ctx, registry.ParseID("test:item-b"), payload.New("b"), store.PutOptions{})
	require.NoError(t, err)
	assert.Equal(t, "test:item-b", put.Key.String())
	assert.Zero(t, put.Version)

	entry, err := ss.Entry(ctx, registry.ParseID("test:item-b"))
	require.NoError(t, err)
	assert.Equal(t, "test:item-b", entry.Key.String())
	assert.Zero(t, entry.Version)

	_, err = ss.Put(ctx, registry.ParseID("test:item-a"), payload.New("a"), store.PutOptions{})
	require.NoError(t, err)
	_, err = ss.Put(ctx, registry.ParseID("other:item"), payload.New("other"), store.PutOptions{})
	require.NoError(t, err)

	page, err := ss.List(ctx, store.ListOptions{Prefix: "test:item-", Limit: 1})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, "test:item-a", page.Items[0].Key.String())
	assert.Equal(t, "test:item-a", page.Cursor)
	assert.True(t, page.HasMore)

	next, err := ss.List(ctx, store.ListOptions{Prefix: "test:item-", After: page.Cursor, Limit: 10})
	require.NoError(t, err)
	require.Len(t, next.Items, 1)
	assert.Equal(t, "test:item-b", next.Items[0].Key.String())
	assert.False(t, next.HasMore)
}

// TestSQLStore_Get_ExpiredKey tests retrieval of an expired key
func TestSQLStore_Get_ExpiredKey(t *testing.T) {
	// Create a default config
	config := createDefaultConfig()

	// Set up database
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()
	setupSQLStoreTable(t, db, config)

	// Create test data
	testKey := registry.NewID("test", "expired_key")
	testData := map[string]string{"value": "expired_value"}
	jsonData, err := json.Marshal(testData)
	require.NoError(t, err)

	// Set expiration to a past time
	expiry := time.Now().Add(-1 * time.Hour)

	// Insert test data with expiration
	insertTestData(t, db, config, testKey.String(), jsonData, &expiry)

	// Set up registry with working resource
	mockReg := NewMockRegistry()
	mockReg.resources[config.Database] = &MockResource{
		value: sqlsvc.DBResource{
			DB:   db,
			Type: sqlconfig.SQLite,
		},
		err: nil,
	}

	// Create context
	ctx := createContext(mockReg)

	// Create store
	logger := zap.NewNop()
	ss := NewStore(registry.NewID("test", "store"), config, logger)
	ss.cleanup(ctx)

	// Test Get with expired key
	result, err := ss.Get(ctx, testKey)
	// Verify results - should behave like key not found
	assert.True(t, errors.Is(err, store.ErrKeyNotFound))
	assert.Nil(t, result)
}

// TestSQLStore_Get_ResourceAcquisitionError tests handling of resource acquisition errors
func TestSQLStore_Get_ResourceAcquisitionError(t *testing.T) {
	// Create a default config
	config := createDefaultConfig()

	// Set up registry with acquisition error
	mockReg := NewMockRegistry()
	mockReg.errors[config.Database] = errors.New("failed to acquire database resource")

	// Create context
	ctx := createContext(mockReg)

	// Create store
	logger := zap.NewNop()
	s := NewStore(registry.NewID("test", "store"), config, logger)

	// Test Get
	result, err := s.Get(ctx, registry.NewID("test", "key"))

	// Verify results
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to acquire database resource")
	assert.Nil(t, result)
}

// TestSQLStore_Get_ResourceGetError tests handling of resource.Get errors
func TestSQLStore_Get_ResourceGetError(t *testing.T) {
	// Create a default config
	config := createDefaultConfig()

	// Set up registry with resource that returns error on Get
	mockReg := NewMockRegistry()
	mockReg.resources[config.Database] = &MockResource{
		value: nil,
		err:   errors.New("failed to get database connection"),
	}

	// Create context
	ctx := createContext(mockReg)

	// Create store
	logger := zap.NewNop()
	ss := NewStore(registry.NewID("test", "store"), config, logger)

	// Test Get
	result, err := ss.Get(ctx, registry.NewID("test", "key"))

	// Verify results
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get database connection")
	assert.Nil(t, result)
}

func TestSQLStore_sanitizeTCNames(t *testing.T) {
	c := createDefaultConfig()

	injectionTests := []struct {
		name     string
		input    string
		expected bool
	}{
		// Basic SQL injections
		{"Basic injection", "users'; DROP TABLE users; --", false},
		{"Union injection", "name UNION SELECT username,password FROM users", false},
		{"Comment injection", "admin'--", false},
		{"Batch query", "users'; INSERT INTO users VALUES('hacker','123'); --", false},

		// SQL reserved words
		{"Reserved word", "select", false},
		{"Reserved word", "update", false},

		// Special characters and edge cases
		{"Special chars", "table`~!@#$%^&*()+=[]{}|\\:;\"'<>,.?/", false},
		{"Empty input", "", false},
		{"Numeric start", "1table", false},
		{"Excessive length",
			"aaaaaaaaaabbbbbbbbbbccccccccccddddddddddeeeeeeeeeeffffffffff1234567890",
			false},

		// More advanced injection techniques
		{"Time-based", "admin' AND (SELECT 1 FROM pg_sleep(10))--", false},
		{"Error-based", "x' AND updatexml(1,concat(0x7e,(SELECT @@version),0x7e),1) AND '", false},
		{"Boolean-based", "admin' AND 1=1--", false},
		{"LIKE operator", "data LIKE '%admin%'", false},
	}

	for _, tt := range injectionTests {
		t.Run(tt.name, func(t *testing.T) {
			result := c.IsSafe(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSQLStore_StartStop(t *testing.T) {
	ss, db, ctx := MakeStore(t)
	defer func() { _ = db.Close() }()

	// Start the store
	statusChan, err := ss.Start(ctx)
	require.NoError(t, err)
	require.NotNil(t, statusChan)

	// Set some data
	err = ss.Set(ctx, createTestEntry("test:key1", "value1"))
	require.NoError(t, err)

	// Stop the store
	err = ss.Stop(ctx)
	require.NoError(t, err)

	// Stop again should be no-op
	err = ss.Stop(ctx)
	require.NoError(t, err)
}

func TestSQLStore_SetUpdate(t *testing.T) {
	ss, db, ctx := MakeStore(t)
	defer func() { _ = db.Close() }()

	key := registry.ParseID("test:updatekey")

	// First set
	err := ss.Set(ctx, createTestEntry("test:updatekey", "original"))
	require.NoError(t, err)

	val, err := ss.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, "original", string(val.Data().([]byte)))

	// Update
	err = ss.Set(ctx, createTestEntry("test:updatekey", "updated"))
	require.NoError(t, err)

	val, err = ss.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, "updated", string(val.Data().([]byte)))
}

func TestSQLStore_SetWithTTL(t *testing.T) {
	ss, db, ctx := MakeStore(t)
	defer func() { _ = db.Close() }()

	key := registry.ParseID("test:ttlkey")
	entry := store.Entry{
		Key:   key,
		Value: payload.New("ttl value"),
		TTL:   50 * time.Millisecond,
	}

	err := ss.Set(ctx, entry)
	require.NoError(t, err)

	// Should exist immediately
	exists, err := ss.Has(ctx, key)
	require.NoError(t, err)
	assert.True(t, exists)

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	// Should be gone (TTL expired)
	exists, err = ss.Has(ctx, key)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestSQLStore_Cleanup(t *testing.T) {
	config := &sqlstore.Config{
		Database:          registry.NewID("test", "db"),
		TableName:         "kv_cleanup",
		IDColumnName:      "key_id",
		PayloadColumnName: "value",
		ExpireColumnName:  "expires_at",
		CleanupInterval:   50 * time.Millisecond,
	}

	db := setupTestDB(t)
	defer func() { _ = db.Close() }()
	setupSQLStoreTable(t, db, config)

	mockReg := NewMockRegistry()
	mockReg.resources[config.Database] = &MockResource{
		value: sqlsvc.DBResource{
			DB:   db,
			Type: sqlconfig.SQLite,
		},
		err: nil,
	}

	ctx := createContext(mockReg)
	ctx = createTranscoderContext(ctx)

	logger := zap.NewNop()
	ss := NewStore(registry.NewID("test", "cleanup-store"), config, logger)

	_, err := ss.Start(ctx)
	require.NoError(t, err)

	// Set a key with short TTL
	entry := store.Entry{
		Key:   registry.ParseID("test:expiring"),
		Value: payload.New("will expire"),
		TTL:   20 * time.Millisecond,
	}
	err = ss.Set(ctx, entry)
	require.NoError(t, err)

	// Wait for cleanup
	time.Sleep(150 * time.Millisecond)

	// Should be cleaned up
	exists, err := ss.Has(ctx, registry.ParseID("test:expiring"))
	require.NoError(t, err)
	assert.False(t, exists)

	err = ss.Stop(ctx)
	require.NoError(t, err)
}

func TestSQLStore_Acquire(t *testing.T) {
	ss, db, ctx := MakeStore(t)
	defer func() { _ = db.Close() }()

	// Acquire in normal mode
	res, err := ss.Acquire(ctx, registry.ParseID("test:resource"), resource.ModeNormal)
	require.NoError(t, err)
	require.NotNil(t, res)

	// Get store from resource
	storeInterface, err := res.Get()
	require.NoError(t, err)
	_, ok := storeInterface.(store.Store)
	assert.True(t, ok)

	// Release
	res.Release()

	// Get after release should fail
	_, err = res.Get()
	assert.Equal(t, resource.ErrReleased, err)

	// Exclusive mode not supported
	_, err = ss.Acquire(ctx, registry.ParseID("test:resource"), resource.ModeExclusive)
	assert.Equal(t, systemresource.ErrLocked, err)
}

func TestSQLStore_ConcurrentReads(t *testing.T) {
	ss, db, ctx := MakeStore(t)
	defer func() { _ = db.Close() }()

	// Pre-populate with data
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("test:key%d", i)
		err := ss.Set(ctx, createTestEntry(key, fmt.Sprintf("value%d", i)))
		require.NoError(t, err)
	}

	const numReaders = 10
	var wg sync.WaitGroup
	errChan := make(chan error, numReaders*50)

	// Concurrent reads should work fine with SQLite
	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				keyID := registry.ParseID(fmt.Sprintf("test:key%d", i))
				val, err := ss.Get(ctx, keyID)
				if err != nil {
					errChan <- fmt.Errorf("get error: %w", err)
					continue
				}
				expected := fmt.Sprintf("value%d", i)
				if string(val.Data().([]byte)) != expected {
					errChan <- fmt.Errorf("value mismatch: got %s, want %s", val.Data(), expected)
				}

				_, err = ss.Has(ctx, keyID)
				if err != nil {
					errChan <- fmt.Errorf("has error: %w", err)
				}
			}
		}()
	}

	wg.Wait()
	close(errChan)

	var errs []error
	for err := range errChan {
		errs = append(errs, err)
	}
	require.Empty(t, errs, "unexpected errors: %v", errs)
}

func TestSQLStore_SequentialOperations(t *testing.T) {
	ss, db, ctx := MakeStore(t)
	defer func() { _ = db.Close() }()

	// Sequential Set/Get/Delete cycle to ensure correctness
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("test:seq%d", i)
		keyID := registry.ParseID(key)
		value := fmt.Sprintf("value-%d", i)

		err := ss.Set(ctx, createTestEntry(key, value))
		require.NoError(t, err)

		val, err := ss.Get(ctx, keyID)
		require.NoError(t, err)
		assert.Equal(t, value, string(val.Data().([]byte)))

		exists, err := ss.Has(ctx, keyID)
		require.NoError(t, err)
		assert.True(t, exists)

		if i%3 == 0 {
			err = ss.Delete(ctx, keyID)
			require.NoError(t, err)

			exists, err = ss.Has(ctx, keyID)
			require.NoError(t, err)
			assert.False(t, exists)
		}
	}
}

// Benchmarks

func BenchmarkSQLStore_Set(b *testing.B) {
	config := createDefaultConfig()
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(b, err)
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	createTable := `CREATE TABLE IF NOT EXISTS ` + config.TableName + ` (
		` + config.IDColumnName + ` TEXT PRIMARY KEY,
		` + config.PayloadColumnName + ` BLOB NOT NULL,
		` + config.ExpireColumnName + ` TIMESTAMP NULL
	)`
	_, err = db.ExecContext(ctx, createTable)
	require.NoError(b, err)

	mockReg := NewMockRegistry()
	mockReg.resources[config.Database] = &MockResource{
		value: sqlsvc.DBResource{
			DB:   db,
			Type: sqlconfig.SQLite,
		},
	}

	ctx = createContext(mockReg)
	ctx = createTranscoderContext(ctx)

	logger := zap.NewNop()
	ss := NewStore(registry.NewID("bench", "store"), config, logger)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("test:key%d", i)
		_ = ss.Set(ctx, createTestEntry(key, i))
	}
}

func BenchmarkSQLStore_Get(b *testing.B) {
	config := createDefaultConfig()
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(b, err)
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	createTable := `CREATE TABLE IF NOT EXISTS ` + config.TableName + ` (
		` + config.IDColumnName + ` TEXT PRIMARY KEY,
		` + config.PayloadColumnName + ` BLOB NOT NULL,
		` + config.ExpireColumnName + ` TIMESTAMP NULL
	)`
	_, err = db.ExecContext(ctx, createTable)
	require.NoError(b, err)

	mockReg := NewMockRegistry()
	mockReg.resources[config.Database] = &MockResource{
		value: sqlsvc.DBResource{
			DB:   db,
			Type: sqlconfig.SQLite,
		},
	}

	ctx = createContext(mockReg)
	ctx = createTranscoderContext(ctx)

	logger := zap.NewNop()
	ss := NewStore(registry.NewID("bench", "store"), config, logger)

	// Pre-populate
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("test:key%d", i)
		_ = ss.Set(ctx, createTestEntry(key, i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := registry.ParseID(fmt.Sprintf("test:key%d", i%1000))
		_, _ = ss.Get(ctx, key)
	}
}

func BenchmarkSQLStore_Has(b *testing.B) {
	config := createDefaultConfig()
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(b, err)
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	createTable := `CREATE TABLE IF NOT EXISTS ` + config.TableName + ` (
		` + config.IDColumnName + ` TEXT PRIMARY KEY,
		` + config.PayloadColumnName + ` BLOB NOT NULL,
		` + config.ExpireColumnName + ` TIMESTAMP NULL
	)`
	_, err = db.ExecContext(ctx, createTable)
	require.NoError(b, err)

	mockReg := NewMockRegistry()
	mockReg.resources[config.Database] = &MockResource{
		value: sqlsvc.DBResource{
			DB:   db,
			Type: sqlconfig.SQLite,
		},
	}

	ctx = createContext(mockReg)
	ctx = createTranscoderContext(ctx)

	logger := zap.NewNop()
	ss := NewStore(registry.NewID("bench", "store"), config, logger)

	// Pre-populate
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("test:key%d", i)
		_ = ss.Set(ctx, createTestEntry(key, i))
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		key := registry.ParseID(fmt.Sprintf("test:key%d", i%1000))
		_, _ = ss.Has(ctx, key)
	}
}

func BenchmarkSQLStore_Delete(b *testing.B) {
	config := createDefaultConfig()
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(b, err)
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	createTable := `CREATE TABLE IF NOT EXISTS ` + config.TableName + ` (
		` + config.IDColumnName + ` TEXT PRIMARY KEY,
		` + config.PayloadColumnName + ` BLOB NOT NULL,
		` + config.ExpireColumnName + ` TIMESTAMP NULL
	)`
	_, err = db.ExecContext(ctx, createTable)
	require.NoError(b, err)

	mockReg := NewMockRegistry()
	mockReg.resources[config.Database] = &MockResource{
		value: sqlsvc.DBResource{
			DB:   db,
			Type: sqlconfig.SQLite,
		},
	}

	ctx = createContext(mockReg)
	ctx = createTranscoderContext(ctx)

	logger := zap.NewNop()
	ss := NewStore(registry.NewID("bench", "store"), config, logger)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		key := fmt.Sprintf("test:key%d", i)
		_ = ss.Set(ctx, createTestEntry(key, i))
		b.StartTimer()

		_ = ss.Delete(ctx, registry.ParseID(key))
	}
}

// Correctness tests for API contracts

func TestSQLStore_GetReturnsCorrectPayloadFormat(t *testing.T) {
	ss, db, ctx := MakeStore(t)
	defer func() { _ = db.Close() }()

	// Use a simple string value that the transcoder handles
	testValue := "test value"

	err := ss.Set(ctx, store.Entry{
		Key:   registry.ParseID("test:structured"),
		Value: payload.New(testValue),
	})
	require.NoError(t, err)

	result, err := ss.Get(ctx, registry.ParseID("test:structured"))
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify it's JSON format
	assert.Equal(t, payload.JSON, result.Format())

	// Verify data can be unmarshaled
	data, ok := result.Data().([]byte)
	require.True(t, ok)
	assert.Equal(t, testValue, string(data))
}

func TestSQLStore_HasDoesNotModifyData(t *testing.T) {
	ss, db, ctx := MakeStore(t)
	defer func() { _ = db.Close() }()

	key := registry.ParseID("test:immutable")
	value := "original value"

	err := ss.Set(ctx, createTestEntry("test:immutable", value))
	require.NoError(t, err)

	// Call Has multiple times
	for i := 0; i < 10; i++ {
		exists, err := ss.Has(ctx, key)
		require.NoError(t, err)
		assert.True(t, exists)
	}

	// Verify value unchanged
	result, err := ss.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, value, string(result.Data().([]byte)))
}

func TestSQLStore_DeleteIsIdempotent(t *testing.T) {
	ss, db, ctx := MakeStore(t)
	defer func() { _ = db.Close() }()

	key := registry.ParseID("test:deleteme")

	err := ss.Set(ctx, createTestEntry("test:deleteme", "value"))
	require.NoError(t, err)

	// First delete should succeed
	err = ss.Delete(ctx, key)
	require.NoError(t, err)

	// Second delete should return ErrKeyNotFound
	err = ss.Delete(ctx, key)
	assert.Equal(t, store.ErrKeyNotFound, err)

	// Third delete should also return ErrKeyNotFound
	err = ss.Delete(ctx, key)
	assert.Equal(t, store.ErrKeyNotFound, err)
}

func TestSQLStore_SetOverwritesExistingValue(t *testing.T) {
	ss, db, ctx := MakeStore(t)
	defer func() { _ = db.Close() }()

	key := registry.ParseID("test:overwrite")

	// Set initial value
	err := ss.Set(ctx, createTestEntry("test:overwrite", "initial"))
	require.NoError(t, err)

	val, err := ss.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, "initial", string(val.Data().([]byte)))

	// Overwrite with new value
	err = ss.Set(ctx, createTestEntry("test:overwrite", "overwritten"))
	require.NoError(t, err)

	val, err = ss.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, "overwritten", string(val.Data().([]byte)))
}

func TestSQLStore_TTLExpirationIsRespected(t *testing.T) {
	ss, db, ctx := MakeStore(t)
	defer func() { _ = db.Close() }()

	key := registry.ParseID("test:ttl")
	entry := store.Entry{
		Key:   key,
		Value: payload.New("expires soon"),
		TTL:   30 * time.Millisecond,
	}

	err := ss.Set(ctx, entry)
	require.NoError(t, err)

	// Immediately should exist
	exists, err := ss.Has(ctx, key)
	require.NoError(t, err)
	assert.True(t, exists)

	val, err := ss.Get(ctx, key)
	require.NoError(t, err)
	assert.NotNil(t, val)

	// Wait for expiration
	time.Sleep(50 * time.Millisecond)

	// Should not exist
	exists, err = ss.Has(ctx, key)
	require.NoError(t, err)
	assert.False(t, exists)

	_, err = ss.Get(ctx, key)
	assert.Equal(t, store.ErrKeyNotFound, err)
}

func TestSQLStore_EmptyKeyBehavior(t *testing.T) {
	ss, db, ctx := MakeStore(t)
	defer func() { _ = db.Close() }()

	// Empty key
	emptyKey := registry.ID{}

	// Set with empty key
	err := ss.Set(ctx, store.Entry{
		Key:   emptyKey,
		Value: payload.New("empty key value"),
	})
	require.NoError(t, err)

	// Get with empty key
	val, err := ss.Get(ctx, emptyKey)
	require.NoError(t, err)
	assert.Equal(t, "empty key value", string(val.Data().([]byte)))

	// Has with empty key
	exists, err := ss.Has(ctx, emptyKey)
	require.NoError(t, err)
	assert.True(t, exists)

	// Delete with empty key
	err = ss.Delete(ctx, emptyKey)
	require.NoError(t, err)

	exists, err = ss.Has(ctx, emptyKey)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestSQLStore_LargePayload(t *testing.T) {
	ss, db, ctx := MakeStore(t)
	defer func() { _ = db.Close() }()

	// Create a large string payload (100KB)
	largeString := make([]byte, 100*1024)
	for i := range largeString {
		largeString[i] = byte('a' + (i % 26))
	}

	key := registry.ParseID("test:large")
	err := ss.Set(ctx, store.Entry{
		Key:   key,
		Value: payload.New(string(largeString)),
	})
	require.NoError(t, err)

	val, err := ss.Get(ctx, key)
	require.NoError(t, err)

	resultData, ok := val.Data().([]byte)
	require.True(t, ok)

	// Verify the data integrity
	assert.Equal(t, len(largeString), len(resultData))
	assert.Equal(t, string(largeString), string(resultData))
}

func TestSQLStore_SpecialCharactersInKey(t *testing.T) {
	ss, db, ctx := MakeStore(t)
	defer func() { _ = db.Close() }()

	specialKeys := []string{
		"test:with spaces",
		"test:with/slashes",
		"test:with:colons",
		"test:with.dots",
		"test:with-dashes",
		"test:with_underscores",
		"test:unicode:日本語",
	}

	for _, keyStr := range specialKeys {
		t.Run(keyStr, func(t *testing.T) {
			key := registry.ParseID(keyStr)
			value := "value for " + keyStr

			err := ss.Set(ctx, createTestEntry(keyStr, value))
			require.NoError(t, err)

			exists, err := ss.Has(ctx, key)
			require.NoError(t, err)
			assert.True(t, exists)

			val, err := ss.Get(ctx, key)
			require.NoError(t, err)
			assert.Equal(t, value, string(val.Data().([]byte)))

			err = ss.Delete(ctx, key)
			require.NoError(t, err)

			exists, err = ss.Has(ctx, key)
			require.NoError(t, err)
			assert.False(t, exists)
		})
	}
}

func TestSQLStore_ResourceInterface(t *testing.T) {
	ss, db, ctx := MakeStore(t)
	defer func() { _ = db.Close() }()

	// Test Acquire returns correct type
	res, err := ss.Acquire(ctx, registry.ParseID("test:resource"), resource.ModeNormal)
	require.NoError(t, err)
	require.NotNil(t, res)

	// Get should return store.Store
	storeInterface, err := res.Get()
	require.NoError(t, err)

	// Type assert to store.Store
	s, ok := storeInterface.(store.Store)
	require.True(t, ok, "expected store.Store interface")

	// Should be able to use the store through the interface
	err = s.Set(ctx, createTestEntry("test:via-interface", "value"))
	require.NoError(t, err)

	val, err := s.Get(ctx, registry.ParseID("test:via-interface"))
	require.NoError(t, err)
	assert.NotNil(t, val)

	// Release and verify Get fails
	res.Release()
	_, err = res.Get()
	assert.Equal(t, resource.ErrReleased, err)

	// Double release should be safe
	res.Release()
}

func TestSQLStore_OperationsAfterClose(t *testing.T) {
	ss, db, ctx := MakeStore(t)
	defer func() { _ = db.Close() }()

	// Start store
	_, err := ss.Start(ctx)
	require.NoError(t, err)

	// Set a value while open
	key := registry.ParseID("test:before-close")
	err = ss.Set(ctx, createTestEntry("test:before-close", "value"))
	require.NoError(t, err)

	// Stop store
	err = ss.Stop(ctx)
	require.NoError(t, err)

	// All operations should return ErrStoreClosed

	// Get after close
	_, err = ss.Get(ctx, key)
	assert.Equal(t, servicestore.ErrStoreClosed, err)

	// Set after close
	err = ss.Set(ctx, createTestEntry("test:after-close", "value"))
	assert.Equal(t, servicestore.ErrStoreClosed, err)

	// Has after close
	_, err = ss.Has(ctx, key)
	assert.Equal(t, servicestore.ErrStoreClosed, err)

	// Delete after close
	err = ss.Delete(ctx, key)
	assert.Equal(t, servicestore.ErrStoreClosed, err)

	// Start after close should fail
	_, err = ss.Start(ctx)
	assert.Equal(t, servicestore.ErrStoreClosed, err)
}

func TestSQLStore_DoubleStop(t *testing.T) {
	ss, db, ctx := MakeStore(t)
	defer func() { _ = db.Close() }()

	_, err := ss.Start(ctx)
	require.NoError(t, err)

	// First stop
	err = ss.Stop(ctx)
	assert.NoError(t, err)

	// Second stop should be no-op
	err = ss.Stop(ctx)
	assert.NoError(t, err)

	// Third stop should also be no-op
	err = ss.Stop(ctx)
	assert.NoError(t, err)
}
