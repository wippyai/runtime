// SPDX-License-Identifier: MPL-2.0

package value

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
)

func TestToGoAnyJSONNull(t *testing.T) {
	assert.Nil(t, ToGoAny(JSONNull))

	tbl := lua.CreateTable(2, 1)
	tbl.RawSetString("a", JSONNull)
	tbl.RawSetString("b", lua.LInteger(1))
	got := ToGoAny(tbl)
	m, ok := got.(map[string]any)
	require.True(t, ok, "got %T", got)
	value, present := m["a"]
	assert.True(t, present, "null member must stay present")
	assert.Nil(t, value)

	list := lua.CreateTable(3, 0)
	list.Append(JSONNull)
	list.Append(lua.LInteger(2))
	list.Append(JSONNull)
	assert.Equal(t, []any{nil, int64(2), nil}, ToGoAny(list))
}

func TestToGoAnyJSONRaw(t *testing.T) {
	raw, err := NewJSONRaw([]byte(`{"z":1,"a":[1.5,"x",null,{"n":9223372036854775807}],"e":{}}`))
	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"z": int64(1),
		"a": []any{1.5, "x", nil, map[string]any{"n": int64(9223372036854775807)}},
		"e": map[string]any{},
	}, ToGoAny(raw))
}

func TestNewJSONRawCompactsAndValidates(t *testing.T) {
	raw, err := NewJSONRaw([]byte(" { \"b\" : 1 ,\n \"a\" : [ 2 ] } "))
	require.NoError(t, err)
	text, ok := JSONRawText(raw)
	require.True(t, ok)
	assert.Equal(t, `{"b":1,"a":[2]}`, text)
	assert.Equal(t, lua.LNil, raw.Metatable)

	for _, input := range []string{``, ` `, `{"a":`, `1 2`, `{"a":1,"a":2}`, `1e400`, "\"\xff\""} {
		_, err := NewJSONRaw([]byte(input))
		assert.Error(t, err, "input %q", input)
	}
}

func TestIsJSONValue(t *testing.T) {
	raw, err := NewJSONRaw([]byte(`[]`))
	require.NoError(t, err)
	assert.True(t, IsJSONValue(JSONNull))
	assert.True(t, IsJSONValue(raw))
	assert.False(t, IsJSONValue(&lua.LUserData{Value: "x", Metatable: lua.LNil}))
	assert.False(t, IsJSONValue(lua.LNil))
}
