// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	config "github.com/wippyai/runtime/api/service/cdc"
	entryutil "github.com/wippyai/runtime/system/entry"
)

func decodeConfig(t *testing.T, raw map[string]any) *config.Config {
	t.Helper()
	b, err := json.Marshal(raw)
	require.NoError(t, err)
	var cfg config.Config
	require.NoError(t, json.Unmarshal(b, &cfg))
	cfg.InitDefaults()
	return &cfg
}

func TestConfigWireFormatMapsAndBuildsDSN(t *testing.T) {
	cfg := decodeConfig(t, map[string]any{
		"host":                    "db.internal",
		"port":                    5432,
		"username":                "cdc_repl",
		"password":                "secret",
		"database":                "appdb",
		"slot_name":               "wippy_slot",
		"publication":             "wippy_pub",
		"snapshot":                true,
		"max_transaction_changes": 1234,
		"max_transaction_bytes":   65536,
		"max_inflight_changes":    2345,
		"max_inflight_bytes":      131072,
		"standby_interval":        "5s",
		"status_interval":         "1m",
		"tables":                  []any{"public.accounts", "public.orders"},
	})
	require.NoError(t, cfg.Validate())

	assert.Equal(t, "db.internal", cfg.Host)
	assert.Equal(t, 5432, cfg.Port)
	assert.Equal(t, "cdc_repl", cfg.Username)
	assert.Equal(t, "appdb", cfg.Database)
	assert.Equal(t, "wippy_slot", cfg.SlotName)
	assert.Equal(t, "wippy_pub", cfg.Publication)
	assert.True(t, cfg.Snapshot)
	assert.Equal(t, 1234, cfg.MaxTransactionChanges)
	assert.Equal(t, int64(65536), cfg.MaxTransactionBytes)
	assert.Equal(t, 2345, cfg.MaxInflightChanges)
	assert.Equal(t, int64(131072), cfg.MaxInflightBytes)
	assert.Equal(t, "5s", cfg.StandbyInterval)
	assert.Equal(t, []string{"public.accounts", "public.orders"}, cfg.Tables)

	standby, err := cfg.StandbyDuration()
	require.NoError(t, err)
	assert.Equal(t, 5*time.Second, standby)

	repl, admin, err := buildDSNs(cfg)
	require.NoError(t, err)
	ru, err := url.Parse(repl)
	require.NoError(t, err)
	assert.Equal(t, "database", ru.Query().Get("replication"))
	assert.Equal(t, "db.internal:5432", ru.Host)
	au, err := url.Parse(admin)
	require.NoError(t, err)
	assert.Equal(t, "", au.Query().Get("replication"))
}

func TestConfigWireFormatEnvFields(t *testing.T) {
	ctx := ctxWithEnv(&mockEnvRegistry{vars: map[string]string{
		"PGHOST": "wire-host", "PGPORT": "5544", "PGUSER": "wire-user",
		"PGPASS": "wire-pass", "PGDB": "wire-db",
	}})
	data := map[string]any{
		"host_env":     "PGHOST",
		"port_env":     "PGPORT",
		"username_env": "PGUSER",
		"password_env": "PGPASS",
		"database_env": "PGDB",
		"slot_name":    "s",
		"tables":       []any{"accounts"},
		"temporary":    true,
	}

	cfg, err := entryutil.DecodeEntryConfig[config.Config](ctx, &jsonConfigTranscoder{}, cdcEntryWithData(data))
	require.NoError(t, err)
	assert.Equal(t, "wire-host", cfg.Host)
	assert.Equal(t, 5544, cfg.Port)
	assert.Equal(t, "wire-user", cfg.Username)
	assert.Equal(t, "wire-pass", cfg.Password)
	assert.Equal(t, "wire-db", cfg.Database)
	assert.True(t, cfg.Temporary)
}
