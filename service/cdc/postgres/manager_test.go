// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	config "github.com/wippyai/runtime/api/service/cdc"
)

func TestBuildDSNs(t *testing.T) {
	cfg := &config.Config{
		Host:     "db.internal",
		Port:     5432,
		Username: "cdc_repl",
		Password: "p@ss/word",
		Database: "appdb",
	}
	repl, admin := buildDSNs(cfg)

	ru, err := url.Parse(repl)
	require.NoError(t, err)
	assert.Equal(t, "postgres", ru.Scheme)
	assert.Equal(t, "db.internal:5432", ru.Host)
	assert.Equal(t, "/appdb", ru.Path)
	assert.Equal(t, "cdc_repl", ru.User.Username())
	pw, _ := ru.User.Password()
	assert.Equal(t, "p@ss/word", pw)
	assert.Equal(t, "database", ru.Query().Get("replication"))

	au, err := url.Parse(admin)
	require.NoError(t, err)
	assert.Equal(t, "", au.Query().Get("replication"))
	assert.Equal(t, "db.internal:5432", au.Host)
}

func TestBuildDSNsCarriesOptions(t *testing.T) {
	cfg := &config.Config{
		Host:     "h",
		Port:     1,
		Username: "u",
		Password: "p",
		Database: "d",
		Options:  map[string]string{"sslmode": "require"},
	}
	repl, admin := buildDSNs(cfg)

	ru, err := url.Parse(repl)
	require.NoError(t, err)
	assert.Equal(t, "require", ru.Query().Get("sslmode"))
	assert.Equal(t, "database", ru.Query().Get("replication"))

	au, err := url.Parse(admin)
	require.NoError(t, err)
	assert.Equal(t, "require", au.Query().Get("sslmode"))
}
