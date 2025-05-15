package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/store"

	_ "github.com/mattn/go-sqlite3"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	sqlconfig "github.com/ponyruntime/pony/api/service/sql"
	"github.com/ponyruntime/pony/api/service/sqlstore"
	sqlsvc "github.com/ponyruntime/pony/service/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	data   interface{}
	format payload.Format
}

func (p *MockPayload) Data() interface{} {
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

// setupSQLStoreTable creates the table required by SQLStore
func setupSQLStoreTable(t *testing.T, db *sql.DB, config *sqlstore.SQLConfig) {
	createTable := `CREATE TABLE IF NOT EXISTS ` + config.TableName + ` (
		` + config.IDColumnName + ` TEXT PRIMARY KEY,
		` + config.PayloadColumnName + ` BLOB NOT NULL,
		` + config.ExpireColumnName + ` TIMESTAMP NULL
	)`
	_, err := db.Exec(createTable)
	require.NoError(t, err)
}

// insertTestData inserts test key-value pairs into the database
func insertTestData(t *testing.T, db *sql.DB, config *sqlstore.SQLConfig, key string, value []byte, expire *time.Time) {
	var expireVal interface{}
	if expire != nil {
		expireVal = expire.UTC()
	} else {
		expireVal = nil
	}

	//nolint:gosec // FIXME variables should be verified at the config.Validate() step
	query := `INSERT INTO ` + config.TableName + ` (` +
		config.IDColumnName + `, ` +
		config.PayloadColumnName + `, ` +
		config.ExpireColumnName + `) VALUES (?, ?, ?)`

	_, err := db.Exec(query, key, value, expireVal)
	require.NoError(t, err)
}

// createDefaultConfig creates a default SQLStore config for testing
func createDefaultConfig() *sqlstore.SQLConfig {
	return &sqlstore.SQLConfig{
		Database:          registry.ID{NS: "test", Name: "db"},
		TableName:         "kv_store",
		IDColumnName:      "key_id",
		PayloadColumnName: "value",
		ExpireColumnName:  "expires_at",
		CleanupInterval:   time.Hour,
	}
}

// createContext creates a context with the given registry
func createContext(reg resource.Registry) context.Context {
	return resource.WithResources(context.Background(), reg)
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

func MakeStore(t *testing.T) (*SQLStore, *sql.DB, context.Context) {
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
			Type: sqlconfig.KindSQLite,
		},
		err: nil,
	}

	// Create context
	ctx := createContext(mockReg)
	ctx = createTranscoderContext(ctx)

	// Create store
	logger := zap.NewNop()

	return NewSQLStore(registry.ID{NS: "test", Name: "store"}, config, logger), db, ctx
}

func TestSQLStore_Delete(t *testing.T) {
	ss, db, ctx := MakeStore(t)
	defer db.Close()

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

	err = ss.Delete(ctx, testKey)
	require.NoError(t, err)

	result, err = ss.Has(ctx, testKey)
	require.NoError(t, err)
	require.False(t, result)
}

func TestSQLStore_Has(t *testing.T) {
	ss, db, ctx := MakeStore(t)
	defer db.Close()

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
	defer db.Close()

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
	assert.Equal(t, errors.New("key not found"), err)
	assert.Nil(t, result)
}

// TestSQLStore_Get_ExpiredKey tests retrieval of an expired key
func TestSQLStore_Get_ExpiredKey(t *testing.T) {
	// Create a default config
	config := createDefaultConfig()

	// Set up database
	db := setupTestDB(t)
	defer db.Close()
	setupSQLStoreTable(t, db, config)

	// Create test data
	testKey := registry.ID{NS: "test", Name: "expired_key"}
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
			Type: sqlconfig.KindSQLite,
		},
		err: nil,
	}

	// Create context
	ctx := createContext(mockReg)

	// Create store
	logger := zap.NewNop()
	ss := NewSQLStore(registry.ID{NS: "test", Name: "store"}, config, logger)

	// Test Get with expired key
	result, err := ss.Get(ctx, testKey)
	// Verify results - should behave like key not found
	assert.Equal(t, errors.New("key not found"), err)
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
	store := NewSQLStore(registry.ID{NS: "test", Name: "store"}, config, logger)

	// Test Get
	result, err := store.Get(ctx, registry.ID{NS: "test", Name: "key"})

	// Verify results
	assert.Error(t, err)
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
	ss := NewSQLStore(registry.ID{NS: "test", Name: "store"}, config, logger)

	// Test Get
	result, err := ss.Get(ctx, registry.ID{NS: "test", Name: "key"})

	// Verify results
	assert.Error(t, err)
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
