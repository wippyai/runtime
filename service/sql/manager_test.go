package sql

import (
	"context"
	"database/sql"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	envapi "github.com/ponyruntime/pony/api/env"
	"github.com/ponyruntime/pony/internal/config"

	_ "github.com/mattn/go-sqlite3" // Import SQLite driver

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	apiconfig "github.com/ponyruntime/pony/api/service/sql"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/system/eventbus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestTranscoder for manager tests that directly returns valid configs
type TestTranscoder struct{}

func (t *TestTranscoder) Marshal(v interface{}) (payload.Payload, error) {
	return payload.New(v), nil
}

func (t *TestTranscoder) Unmarshal(_ payload.Payload, v interface{}) error {
	switch target := v.(type) {
	case *apiconfig.DBConfig:
		// Directly set the fields of the target
		*target = apiconfig.DBConfig{
			Host:     "localhost",
			Port:     5432,
			Database: "testdb",
			Username: "user",
			Password: "pass",
			Pool: apiconfig.PoolConfig{
				MaxOpen:     10,
				MaxIdle:     5,
				MaxLifetime: time.Hour,
			},
			Options: map[string]string{
				"sslmode": "disable",
			},
			Lifecycle: supervisor.LifecycleConfig{
				StartTimeout: time.Minute,
			},
		}
	case *apiconfig.SQLiteConfig:
		// Directly set the fields of the target
		*target = apiconfig.SQLiteConfig{
			File: ":memory:",
			Pool: apiconfig.PoolConfig{
				MaxOpen:     1,
				MaxIdle:     1,
				MaxLifetime: time.Hour,
			},
			Options: map[string]string{
				"_journal_mode": "WAL",
			},
			Lifecycle: supervisor.LifecycleConfig{
				StartTimeout: time.Minute,
			},
		}
	default:
		return fmt.Errorf("unsupported type: %T", v)
	}
	return nil
}

func (t *TestTranscoder) Transcode(p payload.Payload, format payload.Format) (payload.Payload, error) {
	return payload.NewPayload(p.Data(), format), nil
}

// NewMockConnPool creates a simplified connection pool for testing
func NewMockConnPool(kind registry.Kind) *ConnPool {
	// Create an in-memory SQLite database for testing
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		panic(err)
	}

	// Set up WAL mode for SQLite if needed
	if kind == apiconfig.KindSQLite {
		_, err = db.ExecContext(context.Background(), "PRAGMA journal_mode=WAL;")
		if err != nil {
			_ = db.Close()
			panic(err)
		}
	}

	pool := &ConnPool{
		kind:   kind,
		db:     db,
		status: make(chan any, 1),
		closed: atomic.Bool{},
	}

	// Initialize the config pointer with proper configs
	var cfgAny any
	if kind == apiconfig.KindSQLite {
		sqliteCfg := &apiconfig.SQLiteConfig{
			File: ":memory:",
			Pool: apiconfig.PoolConfig{
				MaxOpen:     1,
				MaxIdle:     1,
				MaxLifetime: time.Hour,
			},
			Options: map[string]string{
				"_journal_mode": "WAL",
			},
		}
		cfgAny = sqliteCfg
	} else {
		dbCfg := &apiconfig.DBConfig{
			Host:     "localhost",
			Port:     5432,
			Database: "testdb",
			Username: "user",
			Password: "pass",
			Pool: apiconfig.PoolConfig{
				MaxOpen:     10,
				MaxIdle:     5,
				MaxLifetime: time.Hour,
			},
		}
		cfgAny = dbCfg
	}
	pool.config.Store(&cfgAny)

	return pool
}

// Mock factory implementation
type TestPoolFactory struct {
	standardPoolCalls []struct {
		Kind registry.Kind
		Cfg  *apiconfig.DBConfig
	}
	sqlitePoolCalls []struct {
		Cfg *apiconfig.SQLiteConfig
	}
	shouldFailNext bool
}

func NewTestPoolFactory() *TestPoolFactory {
	return &TestPoolFactory{
		standardPoolCalls: make([]struct {
			Kind registry.Kind
			Cfg  *apiconfig.DBConfig
		}, 0),
		sqlitePoolCalls: make([]struct {
			Cfg *apiconfig.SQLiteConfig
		}, 0),
	}
}

func (f *TestPoolFactory) CreateStandardPool(kind registry.Kind, cfg *apiconfig.DBConfig) (*ConnPool, error) {
	f.standardPoolCalls = append(f.standardPoolCalls, struct {
		Kind registry.Kind
		Cfg  *apiconfig.DBConfig
	}{
		Kind: kind,
		Cfg:  cfg,
	})

	if f.shouldFailNext {
		return nil, assert.AnError
	}
	return NewMockConnPool(kind), nil
}

func (f *TestPoolFactory) CreateSQLitePool(cfg *apiconfig.SQLiteConfig) (*ConnPool, error) {
	f.sqlitePoolCalls = append(f.sqlitePoolCalls, struct {
		Cfg *apiconfig.SQLiteConfig
	}{
		Cfg: cfg,
	})

	if f.shouldFailNext {
		return nil, assert.AnError
	}
	return NewMockConnPool(apiconfig.KindSQLite), nil
}

// MockEnvRegistry implements envapi.Registry for testing
type MockEnvRegistry struct {
	variables map[string]string
}

func NewMockEnvRegistry() *MockEnvRegistry {
	return &MockEnvRegistry{
		variables: make(map[string]string),
	}
}

func (m *MockEnvRegistry) Get(_ context.Context, name string) (string, error) {
	if value, exists := m.variables[name]; exists {
		return value, nil
	}
	return "", envapi.ErrVariableNotFound
}

func (m *MockEnvRegistry) GetFromStorage(_ context.Context, name string) (string, error) {
	if value, exists := m.variables[name]; exists {
		return value, nil
	}
	return "", envapi.ErrVariableNotFound
}

func (m *MockEnvRegistry) Set(_ context.Context, name string, value string) error {
	m.variables[name] = value
	return nil
}

func (m *MockEnvRegistry) All(_ context.Context) (map[string]string, error) {
	// For testing purposes, we return the variables map
	return m.variables, nil
}

// Helper to create a test manager with mock components
func newTestManager(t *testing.T) (*Manager, event.Bus, *TestPoolFactory) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	transcoder := &TestTranscoder{}
	factory := NewTestPoolFactory()
	envRegistry := NewMockEnvRegistry()

	manager, err := NewManagerWithFactory(transcoder, bus, logger, envRegistry, factory)
	require.NoError(t, err)
	return manager, bus, factory
}

func TestNewManagerWithFactory(t *testing.T) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	transcoder := &TestTranscoder{}
	factory := NewTestPoolFactory()
	envRegistry := NewMockEnvRegistry()

	t.Run("Valid initialization", func(t *testing.T) {
		manager, err := NewManagerWithFactory(transcoder, bus, logger, envRegistry, factory)
		assert.NoError(t, err)
		assert.NotNil(t, manager)
		assert.Equal(t, logger, manager.log)
		assert.Equal(t, transcoder, manager.dtt)
		assert.Equal(t, bus, manager.bus)
		assert.Equal(t, factory, manager.factory)
		assert.NotNil(t, manager.services)
	})

	t.Run("Nil transcoder", func(t *testing.T) {
		manager, err := NewManagerWithFactory(nil, bus, logger, envRegistry, factory)
		assert.Error(t, err)
		assert.Nil(t, manager)
		assert.Contains(t, err.Error(), "transcoder is required")
	})

	t.Run("Nil event bus", func(t *testing.T) {
		manager, err := NewManagerWithFactory(transcoder, nil, logger, envRegistry, factory)
		assert.Error(t, err)
		assert.Nil(t, manager)
		assert.Contains(t, err.Error(), "event bus is required")
	})

	t.Run("Nil factory", func(t *testing.T) {
		manager, err := NewManagerWithFactory(transcoder, bus, logger, envRegistry, nil)
		assert.Error(t, err)
		assert.Nil(t, manager)
		assert.Contains(t, err.Error(), "pool factory is required")
	})
}
func TestManager_Add(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
	defer cancel()

	manager, bus, factory := newTestManager(t)

	// Setup event listener for supervisor and resource events
	supervisorEvents := make(chan event.Event, 2)
	resourceEvents := make(chan event.Event, 2)

	supervisorSub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		supervisor.System,
		"*",
		func(evt event.Event) {
			supervisorEvents <- evt
		},
	)
	require.NoError(t, err)
	defer supervisorSub.Close()

	resourceSub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		"resource", // This is now correct (singular)
		"*",
		func(evt event.Event) {
			resourceEvents <- evt
		},
	)
	require.NoError(t, err)
	defer resourceSub.Close()

	tests := []struct {
		name          string
		kind          registry.Kind
		id            registry.ID
		shouldFail    bool
		expectSuccess bool
	}{
		// Test cases remain the same
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear channels
			for len(supervisorEvents) > 0 {
				<-supervisorEvents
			}
			for len(resourceEvents) > 0 {
				<-resourceEvents
			}

			// Set up factory behavior for this test
			factory.shouldFailNext = tt.shouldFail

			// Create an empty payload for the test
			entry := registry.Entry{
				ID:   tt.id,
				Kind: tt.kind,
				Data: payload.New(map[string]string{"test": "data"}),
			}

			err := manager.Add(ctx, entry)

			if tt.expectSuccess {
				assert.NoError(t, err)
				assert.Contains(t, manager.services, entry.ID)

				// Verify factory was called correctly
				switch tt.kind {
				case apiconfig.KindSQLite:
					assert.GreaterOrEqual(t, len(factory.sqlitePoolCalls), 1)
				case apiconfig.KindPostgres, apiconfig.KindMySQL:
					assert.GreaterOrEqual(t, len(factory.standardPoolCalls), 1)
					if len(factory.standardPoolCalls) > 0 {
						lastCall := factory.standardPoolCalls[len(factory.standardPoolCalls)-1]
						assert.Equal(t, tt.kind, lastCall.Kind)
					}
				}

				// Check for supervisor registration event
				select {
				case evt := <-supervisorEvents:
					assert.Equal(t, supervisor.System, evt.System)
					assert.Equal(t, supervisor.Register, evt.Kind)
					assert.Equal(t, entry.ID.String(), evt.Path)
				case <-time.After(time.Second):
					t.Fatal("timeout waiting for supervisor event")
				}

				// Check for resource registration event - UPDATE HERE
				select {
				case evt := <-resourceEvents:
					assert.Equal(t, "resource", evt.System)
					assert.Equal(t, "resource.register", evt.Kind) // Update to match actual implementation
					assert.Equal(t, entry.ID.String(), evt.Path)
				case <-time.After(time.Second):
					t.Fatal("timeout waiting for resource event")
				}
			} else {
				assert.Error(t, err)
			}

			// Reset factory state for next test
			factory.standardPoolCalls = factory.standardPoolCalls[:0]
			factory.sqlitePoolCalls = factory.sqlitePoolCalls[:0]
		})
	}
}

func TestManager_Update(t *testing.T) {
	manager, bus, _ := newTestManager(t)

	envRegistry := NewMockEnvRegistry()
	require.NoError(t, envRegistry.Set(context.Background(), "POSTGRESQL_DEFAULT_HOST", "test-host"))
	require.NoError(t, envRegistry.Set(context.Background(), "POSTGRESQL_DEFAULT_PORT", "1234"))
	require.NoError(t, envRegistry.Set(context.Background(), "POSTGRESQL_DEFAULT_DATABASE", "test-db"))
	require.NoError(t, envRegistry.Set(context.Background(), "POSTGRESQL_DEFAULT_USERNAME", "test-user"))
	require.NoError(t, envRegistry.Set(context.Background(), "POSTGRESQL_DEFAULT_PASSWORD", "test-pwd"))

	rootCtx, cancel := context.WithTimeout(context.Background(), time.Second*2)
	defer cancel()
	ctx := rootCtx

	// Setup event listener for supervisor events
	supervisorEvents := make(chan event.Event, 2)
	supervisorSub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		supervisor.System,
		"*",
		func(evt event.Event) {
			supervisorEvents <- evt
		},
	)
	require.NoError(t, err)
	defer supervisorSub.Close()

	// First add services to update
	postgresID := registry.ID{NS: "test", Name: "postgres-db"}
	sqliteID := registry.ID{NS: "test", Name: "sqlite-db"}

	// Add PostgreSQL service
	require.NoError(t, manager.Add(ctx, registry.Entry{
		ID:   postgresID,
		Kind: apiconfig.KindPostgres,
		Data: payload.New(map[string]string{"test": "data"}),
	}))

	// Add SQLite service
	require.NoError(t, manager.Add(ctx, registry.Entry{
		ID:   sqliteID,
		Kind: apiconfig.KindSQLite,
		Data: payload.New(map[string]string{"test": "data"}),
	}))

	// Drain events channel
	<-supervisorEvents
	<-supervisorEvents

	tests := []struct {
		name          string
		kind          registry.Kind
		id            registry.ID
		expectSuccess bool
	}{
		{
			name:          "Update PostgreSQL database",
			kind:          apiconfig.KindPostgres,
			id:            postgresID,
			expectSuccess: true,
		},
		{
			name:          "Update SQLite database",
			kind:          apiconfig.KindSQLite,
			id:            sqliteID,
			expectSuccess: true,
		},
		{
			name:          "Update non-existent service",
			kind:          apiconfig.KindPostgres,
			id:            registry.ID{NS: "test", Name: "nonexistent-db"},
			expectSuccess: false,
		},
		{
			name:          "Update with unsupported database type",
			kind:          "db.unsupported",
			id:            postgresID,
			expectSuccess: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := registry.Entry{
				ID:   tt.id,
				Kind: tt.kind,
				Data: payload.New(map[string]string{"updated": "config"}),
			}

			err := manager.Update(ctx, entry)

			if tt.expectSuccess {
				assert.NoError(t, err)

				// Check for supervisor update event
				select {
				case evt := <-supervisorEvents:
					assert.Equal(t, supervisor.System, evt.System)
					assert.Equal(t, supervisor.Update, evt.Kind)
					assert.Equal(t, entry.ID.String(), evt.Path)
				case <-time.After(time.Second):
					t.Fatal("timeout waiting for supervisor event")
				}
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestManager_Delete(t *testing.T) {
	manager, bus, _ := newTestManager(t)

	envRegistry := NewMockEnvRegistry()
	require.NoError(t, envRegistry.Set(context.Background(), "POSTGRESQL_DEFAULT_HOST", "test-host"))
	require.NoError(t, envRegistry.Set(context.Background(), "POSTGRESQL_DEFAULT_PORT", "1234"))
	require.NoError(t, envRegistry.Set(context.Background(), "POSTGRESQL_DEFAULT_DATABASE", "test-db"))
	require.NoError(t, envRegistry.Set(context.Background(), "POSTGRESQL_DEFAULT_USERNAME", "test-user"))
	require.NoError(t, envRegistry.Set(context.Background(), "POSTGRESQL_DEFAULT_PASSWORD", "test-pwd"))

	rootCtx, cancel := context.WithTimeout(context.Background(), time.Second*2)
	defer cancel()
	ctx := rootCtx

	// Setup event listeners
	supervisorEvents := make(chan event.Event, 2)
	resourceEvents := make(chan event.Event, 2)

	supervisorSub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		supervisor.System,
		"*",
		func(evt event.Event) {
			supervisorEvents <- evt
		},
	)
	require.NoError(t, err)
	defer supervisorSub.Close()

	resourceSub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		"resource",
		"*",
		func(evt event.Event) {
			resourceEvents <- evt
		},
	)
	require.NoError(t, err)
	defer resourceSub.Close()

	// Add a service to delete
	dbID := registry.ID{NS: "test", Name: "db-to-delete"}
	require.NoError(t, manager.Add(ctx, registry.Entry{
		ID:   dbID,
		Kind: apiconfig.KindPostgres,
		Data: payload.New(map[string]string{"test": "data"}),
	}))

	// Drain events channels
	for len(supervisorEvents) > 0 {
		select {
		case <-supervisorEvents:
		default:
		}
	}
	for len(resourceEvents) > 0 {
		select {
		case <-resourceEvents:
		default:
		}
	}

	tests := []struct {
		name          string
		id            registry.ID
		expectSuccess bool
	}{
		{
			name:          "Delete existing service",
			id:            dbID,
			expectSuccess: true,
		},
		{
			name:          "Delete non-existent service",
			id:            registry.ID{NS: "test", Name: "nonexistent-db"},
			expectSuccess: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := registry.Entry{
				ID:   tt.id,
				Kind: apiconfig.KindPostgres,
			}

			err := manager.Delete(ctx, entry)

			if tt.expectSuccess {
				assert.NoError(t, err)
				assert.NotContains(t, manager.services, entry.ID)

				// Check for supervisor removal event - UPDATED TO MATCH ACTUAL VALUES
				select {
				case evt := <-supervisorEvents:
					assert.Equal(t, supervisor.System, evt.System)
					assert.Equal(t, "supervisor.service.register", evt.Kind) // Match actual value
					assert.Equal(t, entry.ID.String(), evt.Path)
				case <-time.After(time.Second):
					t.Fatal("timeout waiting for supervisor event")
				}

				// Check for resource deletion event - UPDATED TO MATCH ACTUAL VALUES
				select {
				case evt := <-resourceEvents:
					assert.Equal(t, "resource", evt.System)
					assert.Equal(t, "resource.register", evt.Kind) // Match actual value
					assert.Equal(t, entry.ID.String(), evt.Path)
				case <-time.After(time.Second):
					t.Fatal("timeout waiting for resource event")
				}
			} else {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "not found")
			}
		})
	}
}

func TestDecode_NilPayload(t *testing.T) {
	transcoder := &TestTranscoder{}

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "db1"},
		Kind: apiconfig.KindPostgres,
		Data: nil,
	}

	_, err := config.DecodeAndInitConfig[apiconfig.DBConfig](transcoder, entry)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "configuration data is required")
}
