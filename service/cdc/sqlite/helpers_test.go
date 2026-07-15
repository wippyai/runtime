// SPDX-License-Identifier: MPL-2.0

//go:build sqlite_preupdate_hook

package sqlite

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSchemaAllowed(t *testing.T) {
	assert.True(t, schemaAllowed(""))
	assert.True(t, schemaAllowed("main"))
	assert.True(t, schemaAllowed("MAIN"))
	assert.False(t, schemaAllowed("temp"))
	assert.False(t, schemaAllowed("attached"))
}

func TestNormalizeSchema(t *testing.T) {
	assert.Equal(t, "main", normalizeSchema(""))
	assert.Equal(t, "temp", normalizeSchema("temp"))
	assert.Equal(t, "audit", normalizeSchema("audit"))
}

func TestOpString(t *testing.T) {
	assert.Equal(t, "insert", opString(cdcInsert))
	assert.Equal(t, "update", opString(cdcUpdate))
	assert.Equal(t, "delete", opString(cdcDelete))
	assert.Equal(t, "unknown", opString(9999))
}

func TestApproxRowSize(t *testing.T) {
	assert.Equal(t, 0, approxRowSize(nil))
	assert.Equal(t, 8, approxRowSize([]any{int64(1)}))
	assert.Equal(t, 3, approxRowSize([]any{[]byte{1, 2, 3}}))
	assert.Equal(t, 5, approxRowSize([]any{"hello"}))
	assert.Equal(t, 8+3+5, approxRowSize([]any{1.5, []byte("abc"), "hello"}))
	assert.Equal(t, 8, approxRowSize([]any{nil}))
}
