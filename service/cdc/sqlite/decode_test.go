// SPDX-License-Identifier: MPL-2.0

package sqlite

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTextAffinity(t *testing.T) {
	cases := map[string]bool{
		"TEXT":         true,
		"VARCHAR(255)": true,
		"CLOB":         true,
		"nchar":        true,
		"INTEGER":      false,
		"REAL":         false,
		"BLOB":         false,
		"":             false,
		"NUMERIC":      false,
	}
	for decl, want := range cases {
		assert.Equalf(t, want, textAffinity(decl), "decl=%q", decl)
	}
}

func TestMapRowNilForMissingSide(t *testing.T) {
	assert.Nil(t, mapRow([]columnInfo{{name: "id"}}, nil))
}

func TestMapRowDecodesTextBytesByAffinity(t *testing.T) {
	cols := []columnInfo{
		{name: "id", text: false},
		{name: "email", text: true},
		{name: "payload", text: false},
	}
	vals := []any{int64(7), []byte("a@b.com"), []byte{0x00, 0x01, 0x02}}

	out := mapRow(cols, vals)

	assert.Equal(t, int64(7), out["id"])
	assert.Equal(t, "a@b.com", out["email"])
	assert.Equal(t, []byte{0x00, 0x01, 0x02}, out["payload"])
}

func TestMapRowKeepsInvalidUTF8AsBytes(t *testing.T) {
	cols := []columnInfo{{name: "data", text: true}}
	invalid := []byte{0xff, 0xfe, 0xfd}
	out := mapRow(cols, []any{invalid})
	assert.Equal(t, invalid, out["data"])
}

func TestMapRowFallbackColumnNames(t *testing.T) {
	out := mapRow(nil, []any{int64(1), "x"})
	assert.Equal(t, int64(1), out["column0"])
	assert.Equal(t, "x", out["column1"])
}

func TestNormalizeValuePassThrough(t *testing.T) {
	assert.Equal(t, int64(3), normalizeValue(int64(3), true))
	assert.Equal(t, 1.5, normalizeValue(1.5, true))
	assert.Nil(t, normalizeValue(nil, true))
}
