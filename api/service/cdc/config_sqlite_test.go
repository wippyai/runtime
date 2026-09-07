// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLiteConfigValidate(t *testing.T) {
	missing := &SQLiteConfig{}
	assert.ErrorIs(t, missing.Validate(), ErrDBResourceRequired)

	badInterval := &SQLiteConfig{DBResource: "app:db", StatusInterval: "nope"}
	assert.ErrorIs(t, badInterval.Validate(), ErrInvalidInterval)

	negative := &SQLiteConfig{DBResource: "app:db", StatusInterval: "-5s"}
	assert.ErrorIs(t, negative.Validate(), ErrInvalidInterval)

	ok := &SQLiteConfig{DBResource: "app:db", StatusInterval: "5s", Tables: []string{"users"}, Snapshot: true}
	require.NoError(t, ok.Validate())

	d, err := ok.StatusDuration()
	require.NoError(t, err)
	assert.Equal(t, "5s", d.String())
}

func TestSQLiteConfigZeroInterval(t *testing.T) {
	cfg := &SQLiteConfig{DBResource: "app:db"}
	d, err := cfg.StatusDuration()
	require.NoError(t, err)
	assert.Equal(t, int64(0), int64(d))
}
