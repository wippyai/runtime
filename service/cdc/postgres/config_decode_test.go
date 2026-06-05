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
		"host":             "db.internal",
		"port":             5432,
		"username":         "cdc_repl",
		"password":         "secret",
		"database":         "appdb",
		"slot_name":        "wippy_slot",
		"publication":      "wippy_pub",
		"snapshot":         true,
		"standby_interval": "5s",
		"status_interval":  "1m",
		"tables":           []any{"public.accounts", "public.orders"},
	})
	require.NoError(t, cfg.Validate())

	assert.Equal(t, "db.internal", cfg.Host)
	assert.Equal(t, 5432, cfg.Port)
	assert.Equal(t, "cdc_repl", cfg.Username)
	assert.Equal(t, "appdb", cfg.Database)
	assert.Equal(t, "wippy_slot", cfg.SlotName)
	assert.Equal(t, "wippy_pub", cfg.Publication)
	assert.True(t, cfg.Snapshot)
	assert.Equal(t, "5s", cfg.StandbyInterval)
	assert.Equal(t, []string{"public.accounts", "public.orders"}, cfg.Tables)
	assert.Equal(t, config.DefaultEventSystem, cfg.EventSystem)

	standby, err := cfg.StandbyDuration()
	require.NoError(t, err)
	assert.Equal(t, 5*time.Second, standby)

	repl, admin := buildDSNs(cfg)
	ru, err := url.Parse(repl)
	require.NoError(t, err)
	assert.Equal(t, "database", ru.Query().Get("replication"))
	assert.Equal(t, "db.internal:5432", ru.Host)
	au, err := url.Parse(admin)
	require.NoError(t, err)
	assert.Equal(t, "", au.Query().Get("replication"))
}

func TestConfigWireFormatEnvFields(t *testing.T) {
	cfg := decodeConfig(t, map[string]any{
		"host_env":     "PGHOST",
		"port_env":     "PGPORT",
		"username_env": "PGUSER",
		"password_env": "PGPASS",
		"database_env": "PGDB",
		"slot_name":    "s",
		"tables":       []any{"accounts"},
		"temporary":    true,
	})
	require.NoError(t, cfg.Validate())
	assert.Equal(t, "PGHOST", cfg.HostEnv)
	assert.Equal(t, "PGPASS", cfg.PasswordEnv)
	assert.True(t, cfg.Temporary)
}
