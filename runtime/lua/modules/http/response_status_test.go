// SPDX-License-Identifier: MPL-2.0

package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	httpservice "github.com/wippyai/runtime/api/service/http"
)

func TestStatusBeforeHeadersAndBody(t *testing.T) {
	for _, test := range []struct{ name, script, contentType string }{
		{"text", `assert(res:set_content_type("text/plain") == nil); assert(res:write("body") == nil)`, "text/plain"},
		{"json", `assert(res:write_json({ok = true}) == nil)`, "application/json"},
		{"event", `assert(res:write_event({name = "test", data = {ok = true}}) == nil)`, "text/event-stream"},
		{"flush", `assert(res:set_content_type("text/plain") == nil); assert(res:flush() == nil)`, "text/plain"},
	} {
		t.Run(test.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()
			bind(l)
			ctx, frame := newTestContext()
			recorder := httptest.NewRecorder()
			reqCtx := httpservice.NewRequestContext(httptest.NewRequestWithContext(ctx, "GET", "/", nil), recorder)
			require.NoError(t, frame.Set(httpservice.RequestKey(), reqCtx))
			l.SetContext(ctx)
			require.NoError(t, l.DoString(`
				local res = http.response()
				assert(res:set_status(202) == nil)
				assert(res:set_status(201) == nil)
				assert(res:set_header("X-Test", "present") == nil)
			`+test.script+`
				assert(res:set_status(200) ~= nil, "status must be fixed after commit")
				assert(res:set_header("X-Late", "absent") ~= nil)
			`))
			// Result snapshots the actual transmitted headers, not the mutable map.
			result := recorder.Result()
			defer result.Body.Close()
			require.Equal(t, http.StatusCreated, result.StatusCode)
			require.Equal(t, "present", result.Header.Get("X-Test"))
			require.Equal(t, test.contentType, result.Header.Get("Content-Type"))
			require.Empty(t, result.Header.Get("X-Late"))
		})
	}
}
