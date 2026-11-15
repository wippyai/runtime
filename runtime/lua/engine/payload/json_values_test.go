// SPDX-License-Identifier: MPL-2.0

package payload

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	luavalue "github.com/wippyai/runtime/runtime/lua/engine/value"
	jsonlua "github.com/wippyai/runtime/runtime/lua/modules/json"
)

func jsonValuesTable(t *testing.T) (*lua.LTable, *lua.LUserData) {
	t.Helper()
	raw, err := luavalue.NewJSONRaw([]byte(`{"z":1,"a":2}`))
	require.NoError(t, err)
	list := lua.CreateTable(2, 0)
	list.Append(luavalue.JSONNull)
	list.Append(lua.LInteger(2))
	tbl := lua.CreateTable(0, 3)
	tbl.RawSetString("a", luavalue.JSONNull)
	tbl.RawSetString("list", list)
	tbl.RawSetString("schema", raw)
	return tbl, raw
}

func TestExportPayloadKeepsJSONValues(t *testing.T) {
	tbl, raw := jsonValuesTable(t)

	exported := ExportPayload(tbl)
	copied, ok := exported.Data().(*lua.LTable)
	require.True(t, ok)
	assert.NotSame(t, tbl, copied)
	assert.Same(t, luavalue.JSONNull, copied.RawGetString("a"))
	assert.Same(t, raw, copied.RawGetString("schema"))
	assert.Same(t, luavalue.JSONNull, copied.RawGetString("list").(*lua.LTable).RawGetInt(1))

	encoded, err := jsonlua.Encode(copied)
	require.NoError(t, err)
	assert.Equal(t, `{"a":null,"list":[null,2],"schema":{"z":1,"a":2}}`, string(encoded))
}

func TestProcessAndImmutabilizeKeepsJSONValues(t *testing.T) {
	tbl, raw := jsonValuesTable(t)

	result := processAndImmutabilize(tbl).(*lua.LTable)
	assert.Same(t, luavalue.JSONNull, result.RawGetString("a"))
	assert.Same(t, raw, result.RawGetString("schema"))
}

func TestLuaToGolangTranscodesJSONValues(t *testing.T) {
	tbl, _ := jsonValuesTable(t)

	transcoded, err := (&ToGolang{}).Transcode(ExportPayload(tbl))
	require.NoError(t, err)
	assert.Equal(t, payload.Golang, transcoded.Format())
	assert.Equal(t, map[string]any{
		"a":      nil,
		"list":   []any{nil, int64(2)},
		"schema": map[string]any{"z": int64(1), "a": int64(2)},
	}, transcoded.Data())
}
