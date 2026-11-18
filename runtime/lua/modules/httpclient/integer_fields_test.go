// SPDX-License-Identifier: MPL-2.0

package httpclient

import (
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/go-lua/types/io"
	httpapi "github.com/wippyai/runtime/api/service/http"
	"github.com/wippyai/runtime/runtime/lua/code"
)

func requireIntegerField(t *testing.T, tbl lua.LValue, key string, want int64) {
	t.Helper()
	got := tbl.(*lua.LTable).RawGetString(key)
	integer, ok := got.(lua.LInteger)
	require.Truef(t, ok, "%s is %T, want lua.LInteger", key, got)
	require.Equal(t, want, int64(integer))
}

func TestRequestYieldResponseCountsAreIntegers(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	out := (&RequestYield{}).HandleResult(l, httpapi.Response{StatusCode: 200, Body: []byte("hello")}, nil)
	require.Len(t, out, 2)
	requireIntegerField(t, out[0], "status_code", 200)
	requireIntegerField(t, out[0], "body_size", 5)

	out = (&RequestYield{}).HandleResult(l, httpapi.Response{StatusCode: 200, StreamID: 1}, nil)
	requireIntegerField(t, out[0], "body_size", -1)
}

func TestBatchYieldResponseCountsAreIntegers(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	out := (&RequestBatchYield{}).HandleResult(l, httpapi.BatchResponse{Responses: []httpapi.Response{
		{StatusCode: 204, Body: []byte("abc")},
	}}, nil)
	responses := out[0].(*lua.LTable)
	first := responses.RawGetInt(1)
	requireIntegerField(t, first, "status_code", 204)
	requireIntegerField(t, first, "body_size", 3)
}

func TestResponseUserdataCountsAreIntegers(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bind(l)

	l.SetGlobal("res", NewResponse(l, 404, nil, nil, []byte("hello"), "http://example.com"))
	require.NoError(t, l.DoString(`
		assert(math.type(res.status_code) == "integer", "status_code")
		assert(math.type(res.body_size) == "integer", "body_size")
		assert(tostring(res) == "httpclient.Response{status=404}", tostring(res))
	`))
}

func TestResponseCountsTypecheckAsInteger(t *testing.T) {
	tc := code.NewTypeChecker(code.TypeCheckConfig{Enabled: true, Strict: true}, nil)
	_, diagnostics, err := tc.Check(`
local client = require("http_client")
local response, err = client.get("http://peer/")
if response then
    local status: integer = response.status_code
    local size: integer = response.body_size
end
`, "integer_fields.lua", map[string]*io.Manifest{"http_client": ModuleTypes()})
	require.NoError(t, err)
	require.False(t, code.HasErrors(diagnostics), "unexpected diagnostics: %v", diagnostics)
}
