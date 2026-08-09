// SPDX-License-Identifier: MPL-2.0

//go:build sqlite_preupdate_hook

package sqlite

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeSchema(t *testing.T) {
	assert.Equal(t, "main", normalizeSchema(""))
	assert.Equal(t, "temp", normalizeSchema("temp"))
	assert.Equal(t, "audit", normalizeSchema("audit"))
}

func TestValuesByColumnRequiresCapturedShape(t *testing.T) {
	got, err := valuesByColumn([]string{"id", "name"}, []any{int64(1), "one"})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"id": int64(1), "name": "one"}, got)

	_, err = valuesByColumn([]string{"id"}, []any{int64(1), "extra"})
	assert.Error(t, err)
	_, err = valuesByColumn([]string{"id", "id"}, []any{int64(1), int64(2)})
	assert.Error(t, err)
}

func TestValuesByColumnCopiesBytes(t *testing.T) {
	value := []byte{1, 2, 3}
	got, err := valuesByColumn([]string{"blob"}, []any{value})
	require.NoError(t, err)
	value[0] = 9
	assert.Equal(t, []byte{1, 2, 3}, got["blob"])
}
