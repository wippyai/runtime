// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"database/sql"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	envapi "github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	apiconfig "github.com/wippyai/runtime/api/service/sql"
	"github.com/wippyai/runtime/api/supervisor"
	entryutil "github.com/wippyai/runtime/internal/entry"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// TestTranscoder for manager tests that directly returns valid configs
type TestTranscoder struct{}

func (t *TestTranscoder) Marshal(v any) (payload.Payload, error) {
	return payload.New(v), nil
}

func (t *TestTranscoder) Unmarshal(_ payload.Payload, v any) error {
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
	if kind == apiconfig.SQLite {
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
	if kind == apiconfig.SQLite {
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
		Cfg  *apiconfig.DBConfig
		Kind registry.Kind
	}
	sqlitePoolCalls []struct {
		Cfg *apiconfig.SQLiteConfig
	}
	shouldFailNext bool
}

func NewTestPoolFactory() *TestPoolFactory {
	return &TestPoolFactory{
		standardPoolCalls: nil,
		sqlitePoolCalls:   nil,
	}
}

func (f *TestPoolFactory) CreateStandardPool(_ context.Context, kind registry.Kind, cfg *apiconfig.DBConfig) (*ConnPool, error) {
	f.standardPoolCalls = append(f.standardPoolCalls, struct {
		Cfg  *apiconfig.DBConfig
		Kind registry.Kind
	}{
		Kind: kind,
		Cfg:  cfg,
	})

	if f.shouldFailNext {
		return nil, assert.AnError
	}
	return NewMockConnPool(kind), nil
}

func (f *TestPoolFactory) CreateSQLitePool(_ context.Context, cfg *apiconfig.SQLiteConfig) (*ConnPool, error) {
	f.sqlitePoolCalls = append(f.sqlitePoolCalls, struct {
		Cfg *apiconfig.SQLiteConfig
	}{
		Cfg: cfg,
	})

	if f.shouldFailNext {
		return nil, assert.AnError
	}
	return NewMockConnPool(apiconfig.SQLite), nil
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

func (m *MockEnvRegistry) Lookup(_ context.Context, name string) (string, bool, error) {
	if value, exists := m.variables[name]; exists {
		return value, true, nil
	}
	return "", false, nil
}

func (m *MockEnvRegistry) GetStorage(_ context.Context, _ registry.ID) (envapi.Storage, error) {
	return nil, envapi.ErrVariableNotFound
}

func (m *MockEnvRegistry) RegisterStorage(_ registry.ID, _ envapi.Storage) {}

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
		require.Error(t, err)
		assert.Nil(t, manager)
		assert.Contains(t, err.Error(), "transcoder is required")
	})

	t.Run("Nil event bus", func(t *testing.T) {
		manager, err := NewManagerWithFactory(transcoder, nil, logger, envRegistry, factory)
		require.Error(t, err)
		assert.Nil(t, manager)
		assert.Contains(t, err.Error(), "event bus is required")
	})

	t.Run("Nil factory", func(t *testing.T) {
		manager, err := NewManagerWithFactory(transcoder, bus, logger, envRegistry, nil)
		require.Error(t, err)
		assert.Nil(t, manager)
		assert.Contains(t, err.Error(), "pool factory is required")
	})
}

func expectEvent(t *testing.T, events <-chan event.Event, system event.System, kind event.Kind) {
	t.Helper()

	select {
	case evt := <-events:
		require.Equal(t, system, evt.System)
		require.Equal(t, kind, evt.Kind)
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for %s/%s event", system, kind)
	}
}

func TestManager_Add(t *testing.T) {
	ctx := ctxapi.NewRootContext()

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
		resource.System,
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
	}{}

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
				case apiconfig.SQLite:
					assert.GreaterOrEqual(t, len(factory.sqlitePoolCalls), 1)
				case apiconfig.Postgres, apiconfig.MySQL:
					assert.GreaterOrEqual(t, len(factory.standardPoolCalls), 1)
					if len(factory.standardPoolCalls) > 0 {
						lastCall := factory.standardPoolCalls[len(factory.standardPoolCalls)-1]
						assert.Equal(t, tt.kind, lastCall.Kind)
					}
				}

				expectEvent(t, supervisorEvents, supervisor.System, supervisor.ServiceRegister)
				expectEvent(t, resourceEvents, resource.System, resource.Register)
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
	ctx := ctxapi.NewRootContext()
	require.NoError(t, envRegistry.Set(ctx, "POSTGRESQL_DEFAULT_HOST", "test-host"))
	require.NoError(t, envRegistry.Set(ctx, "POSTGRESQL_DEFAULT_PORT", "1234"))
	require.NoError(t, envRegistry.Set(ctx, "POSTGRESQL_DEFAULT_DATABASE", "test-db"))
	require.NoError(t, envRegistry.Set(ctx, "POSTGRESQL_DEFAULT_USERNAME", "test-user"))
	require.NoError(t, envRegistry.Set(ctx, "POSTGRESQL_DEFAULT_PASSWORD", "test-pwd"))

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
	postgresID := registry.NewID("test", "postgres-db")
	sqliteID := registry.NewID("test", "sqlite-db")

	// Add PostgreSQL service
	require.NoError(t, manager.Add(ctx, registry.Entry{
		ID:   postgresID,
		Kind: apiconfig.Postgres,
		Data: payload.New(map[string]string{"test": "data"}),
	}))

	// Add SQLite service
	require.NoError(t, manager.Add(ctx, registry.Entry{
		ID:   sqliteID,
		Kind: apiconfig.SQLite,
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
			kind:          apiconfig.Postgres,
			id:            postgresID,
			expectSuccess: true,
		},
		{
			name:          "Update SQLite database",
			kind:          apiconfig.SQLite,
			id:            sqliteID,
			expectSuccess: true,
		},
		{
			name:          "Update non-existent service",
			kind:          apiconfig.Postgres,
			id:            registry.NewID("test", "nonexistent-db"),
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
					assert.Equal(t, supervisor.ServiceUpdate, evt.Kind)
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
	ctx := ctxapi.NewRootContext()
	require.NoError(t, envRegistry.Set(ctx, "POSTGRESQL_DEFAULT_HOST", "test-host"))
	require.NoError(t, envRegistry.Set(ctx, "POSTGRESQL_DEFAULT_PORT", "1234"))
	require.NoError(t, envRegistry.Set(ctx, "POSTGRESQL_DEFAULT_DATABASE", "test-db"))
	require.NoError(t, envRegistry.Set(ctx, "POSTGRESQL_DEFAULT_USERNAME", "test-user"))
	require.NoError(t, envRegistry.Set(ctx, "POSTGRESQL_DEFAULT_PASSWORD", "test-pwd"))

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
		resource.System,
		"*",
		func(evt event.Event) {
			resourceEvents <- evt
		},
	)
	require.NoError(t, err)
	defer resourceSub.Close()

	// Add a service to delete
	dbID := registry.NewID("test", "db-to-delete")
	require.NoError(t, manager.Add(ctx, registry.Entry{
		ID:   dbID,
		Kind: apiconfig.Postgres,
		Data: payload.New(map[string]string{"test": "data"}),
	}))
	expectEvent(t, supervisorEvents, supervisor.System, supervisor.ServiceRegister)
	expectEvent(t, resourceEvents, resource.System, resource.Register)

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
			id:            registry.NewID("test", "nonexistent-db"),
			expectSuccess: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := registry.Entry{
				ID:   tt.id,
				Kind: apiconfig.Postgres,
			}

			err := manager.Delete(ctx, entry)

			if tt.expectSuccess {
				assert.NoError(t, err)
				assert.NotContains(t, manager.services, entry.ID)

				expectEvent(t, supervisorEvents, supervisor.System, supervisor.ServiceRemove)
				expectEvent(t, resourceEvents, resource.System, resource.Delete)
			} else {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "not found")
			}
		})
	}
}

func TestDecode_NilPayload(t *testing.T) {
	ctx := ctxapi.NewRootContext()

	transcoder := &TestTranscoder{}

	entry := registry.Entry{
		Kind: apiconfig.Postgres,
		Data: nil,
	}

	_, err := entryutil.DecodeEntryConfig[apiconfig.DBConfig](ctx, transcoder, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuration data is required")
}

func TestManager_ResolveEnv(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := ctxapi.NewRootContext()

	// Create env registry with test values
	envRegistry := NewMockEnvRegistry()
	require.NoError(t, envRegistry.Set(ctx, "TEST_HOST", "test-host-value"))
	require.NoError(t, envRegistry.Set(ctx, "TEST_PORT", "5432"))
	manager.env = envRegistry

	tests := []struct {
		name     string
		envVar   string
		field    string
		expected string
	}{
		{
			name:     "Empty env var returns empty",
			envVar:   "",
			field:    "host",
			expected: "",
		},
		{
			name:     "Found env var returns value",
			envVar:   "TEST_HOST",
			field:    "host",
			expected: "test-host-value",
		},
		{
			name:     "Not found env var returns empty",
			envVar:   "NONEXISTENT_VAR",
			field:    "database",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manager.resolveEnv(ctx, tt.envVar, tt.field)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestManager_AddWithEnvVars(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := ctxapi.NewRootContext()

	// Create env registry with test values
	envRegistry := NewMockEnvRegistry()
	require.NoError(t, envRegistry.Set(ctx, "DB_HOST", "env-host"))
	require.NoError(t, envRegistry.Set(ctx, "DB_PORT", "9999"))
	require.NoError(t, envRegistry.Set(ctx, "DB_NAME", "env-db"))
	require.NoError(t, envRegistry.Set(ctx, "DB_USER", "env-user"))
	require.NoError(t, envRegistry.Set(ctx, "DB_PASS", "env-pass"))
	manager.env = envRegistry

	// Create a custom transcoder that uses env var fields
	manager.dtt = &EnvConfigTranscoder{}

	entry := registry.Entry{
		ID:   registry.NewID("test", "env-db"),
		Kind: apiconfig.Postgres,
		Data: payload.New(map[string]string{"test": "data"}),
	}

	err := manager.Add(ctx, entry)
	assert.NoError(t, err)
}

// EnvConfigTranscoder returns a config with env var fields set
type EnvConfigTranscoder struct{}

func (t *EnvConfigTranscoder) Marshal(v any) (payload.Payload, error) {
	return payload.New(v), nil
}

func (t *EnvConfigTranscoder) Unmarshal(_ payload.Payload, v any) error {
	switch target := v.(type) {
	case *apiconfig.DBConfig:
		*target = apiconfig.DBConfig{
			HostEnv:     "DB_HOST",
			PortEnv:     "DB_PORT",
			DatabaseEnv: "DB_NAME",
			UsernameEnv: "DB_USER",
			PasswordEnv: "DB_PASS",
			Pool: apiconfig.PoolConfig{
				MaxOpen:     10,
				MaxIdle:     5,
				MaxLifetime: time.Hour,
			},
		}
	default:
		return fmt.Errorf("unsupported type: %T", v)
	}
	return nil
}

func (t *EnvConfigTranscoder) Transcode(p payload.Payload, format payload.Format) (payload.Payload, error) {
	return payload.NewPayload(p.Data(), format), nil
}
