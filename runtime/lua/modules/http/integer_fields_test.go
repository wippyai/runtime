// SPDX-License-Identifier: MPL-2.0

package http

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/go-lua/types/io"
	httpservice "github.com/wippyai/runtime/api/service/http"
	"github.com/wippyai/runtime/runtime/lua/code"
)

func TestStatusConstantsAreIntegers(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bind(l)

	require.NoError(t, l.DoString(`
		for name, value in pairs(http.STATUS) do
			assert(math.type(value) == "integer", name)
		end
	`))
}

func TestContentLengthIsInteger(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bind(l)

	ctx, fc := newTestContext()
	req := httptest.NewRequestWithContext(ctx, "POST", "/test", strings.NewReader(`{"test": true}`))
	require.NoError(t, fc.Set(httpservice.RequestKey(), httpservice.NewRequestContext(req, httptest.NewRecorder())))
	l.SetContext(ctx)

	require.NoError(t, l.DoString(`
		local length = http.request():content_length()
		assert(math.type(length) == "integer", tostring(math.type(length)))
		assert(length == 14)
	`))
}

func TestMultipartFileSizeIsInteger(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bind(l)

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", "test.txt")
	require.NoError(t, err)
	_, err = part.Write([]byte("hello world!"))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	ctx, fc := newTestContext()
	req := httptest.NewRequestWithContext(ctx, "POST", "/upload", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	require.NoError(t, fc.Set(httpservice.RequestKey(), httpservice.NewRequestContext(req, httptest.NewRecorder())))
	l.SetContext(ctx)

	require.NoError(t, l.DoString(`
		local form = assert(http.request():parse_multipart())
		local size = form.files.file[1]:size()
		assert(math.type(size) == "integer", tostring(math.type(size)))
	`))
}

func TestIntegerFieldsTypecheck(t *testing.T) {
	tc := code.NewTypeChecker(code.TypeCheckConfig{Enabled: true, Strict: true}, nil)
	_, diagnostics, err := tc.Check(`
local http = require("http")
local req = http.request()
local length: integer = req:content_length()
local res = http.response()
res:set_status(http.STATUS.OK)
local status: integer = http.STATUS.NOT_FOUND
local form = req:parse_multipart()
`, "integer_fields.lua", map[string]*io.Manifest{"http": ModuleTypes()})
	require.NoError(t, err)
	require.False(t, code.HasErrors(diagnostics), "unexpected diagnostics: %v", diagnostics)
}
