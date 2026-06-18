// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"fmt"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	config "github.com/wippyai/runtime/api/service/sql"
	"github.com/wippyai/runtime/api/supervisor"
	"go.uber.org/zap"
)

func createTestDBConfig() *config.DBConfig {
	return &config.DBConfig{
		Host:     "localhost",
		Port:     5432,
		Database: "testdb",
		Username: "user",
		Password: "pass",
		Pool: config.PoolConfig{
			MaxOpen:     10,
			MaxIdle:     5,
			MaxLifetime: time.Hour,
		},
		Options: map[string]string{
			"sslmode": "disable",
		},
	}
}

// fixedTranscoder decodes registry entries into a preset configuration, letting
// factory tests exercise the engine lifecycle without a real registry payload.
type fixedTranscoder struct{ cfg any }

func (f fixedTranscoder) Marshal(v any) (payload.Payload, error) {
	return payload.New(v), nil
}

func (f fixedTranscoder) Unmarshal(_ payload.Payload, v any) error {
	switch target := v.(type) {
	case *config.DBConfig:
		if c, ok := f.cfg.(*config.DBConfig); ok {
			*target = *c
			return nil
		}
	case *config.SQLiteConfig:
		if c, ok := f.cfg.(*config.SQLiteConfig); ok {
			*target = *c
			return nil
		}
	}
	return fmt.Errorf("unexpected decode target %T", v)
}

func (f fixedTranscoder) Transcode(p payload.Payload, format payload.Format) (payload.Payload, error) {
	return payload.NewPayload(p.Data(), format), nil
}

func depsFor(cfg any) EngineDeps {
	return EngineDeps{Transcoder: fixedTranscoder{cfg: cfg}, Env: NewMockEnvRegistry(), Log: zap.NewNop()}
}

// TestDefaultPoolFactory_CreatePoolValidation tests pool creation validation through
// the registry-backed factory.
func TestDefaultPoolFactory_CreatePoolValidation(t *testing.T) {
	factory := &DefaultPoolFactory{}

	tests := []struct {
		cfg     any
		name    string
		kind    registry.Kind
		errMsg  string
		isError bool
	}{
		{
			name:    "Invalid configuration - empty host",
			kind:    config.Postgres,
			cfg:     &config.DBConfig{Host: "", Port: 5432, Database: "db", Username: "user", Password: "pass", Pool: config.PoolConfig{MaxLifetime: time.Hour}},
			isError: true,
			errMsg:  "invalid configuration",
		},
		{
			name:    "Invalid configuration - zero port",
			kind:    config.Postgres,
			cfg:     &config.DBConfig{Host: "localhost", Port: 0, Database: "db", Username: "user", Password: "pass", Pool: config.PoolConfig{MaxLifetime: time.Hour}},
			isError: true,
			errMsg:  "invalid configuration",
		},
		{
			name:    "Invalid configuration - empty database",
			kind:    config.Postgres,
			cfg:     &config.DBConfig{Host: "localhost", Port: 5432, Database: "", Username: "user", Password: "pass", Pool: config.PoolConfig{MaxLifetime: time.Hour}},
			isError: true,
			errMsg:  "invalid configuration",
		},
		{
			name:    "Invalid configuration - empty username",
			kind:    config.Postgres,
			cfg:     &config.DBConfig{Host: "localhost", Port: 5432, Database: "db", Username: "", Password: "pass", Pool: config.PoolConfig{MaxLifetime: time.Hour}},
			isError: true,
			errMsg:  "invalid configuration",
		},
		{
			name:    "Invalid configuration - empty password",
			kind:    config.Postgres,
			cfg:     &config.DBConfig{Host: "localhost", Port: 5432, Database: "db", Username: "user", Password: "", Pool: config.PoolConfig{MaxLifetime: time.Hour}},
			isError: true,
			errMsg:  "invalid configuration",
		},
		{
			name:    "Invalid configuration - SQLite empty file",
			kind:    config.SQLite,
			cfg:     &config.SQLiteConfig{File: "", Pool: config.PoolConfig{MaxLifetime: time.Hour}},
			isError: true,
			errMsg:  "invalid configuration",
		},
		{
			name:    "Unsupported entry kind",
			kind:    "db.unsupported",
			cfg:     createTestDBConfig(),
			isError: true,
			errMsg:  "unsupported entry kind",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := registry.Entry{ID: registry.NewID("test", "x"), Kind: tt.kind, Data: payload.New("x")}
			pool, _, err := factory.CreatePool(context.Background(), depsFor(tt.cfg), entry)

			if tt.isError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Nil(t, pool)
			}
		})
	}
}

// TestDefaultPoolFactory_CreatePoolSQLiteSuccess exercises the full create lifecycle
// (open, WAL prepare, tune, store) for a SQLite pool.
func TestDefaultPoolFactory_CreatePoolSQLiteSuccess(t *testing.T) {
	cfg := &config.SQLiteConfig{
		File:      ":memory:",
		Lifecycle: supervisor.LifecycleConfig{StartTimeout: time.Minute},
		Pool:      config.PoolConfig{MaxLifetime: time.Hour},
	}
	entry := registry.Entry{ID: registry.NewID("test", "lite"), Kind: config.SQLite, Data: payload.New("x")}

	pool, ec, err := (&DefaultPoolFactory{}).CreatePool(context.Background(), depsFor(cfg), entry)
	require.NoError(t, err)
	require.NotNil(t, pool)
	assert.Equal(t, config.SQLite, pool.kind)
	require.NotNil(t, ec)
	assert.Equal(t, time.Minute, ec.LifecycleConfig().StartTimeout)

	require.NoError(t, pool.Stop(context.Background()))
}
