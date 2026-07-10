// SPDX-License-Identifier: MPL-2.0

package queue

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
	"github.com/wippyai/runtime/runtime/lua/code"
)

func TestMessageHeaderTypesMatchNormalizedRuntimeValues(t *testing.T) {
	manifest := ModuleTypes()
	message, ok := manifest.LookupType("Message")
	if !ok {
		t.Fatal("Message type is not defined")
	}

	methods := make(map[string]*typ.Function)
	for _, method := range message.(*typ.Interface).Methods {
		methods[method.Name] = method.Type
	}

	header := methods["header"]
	if header == nil || len(header.Returns) != 2 {
		t.Fatal("header must return value and error")
	}
	if !typ.TypeEquals(header.Returns[0], typ.NewOptional(typ.String)) {
		t.Fatalf("header value = %s, want string?", header.Returns[0])
	}

	headers := methods["headers"]
	if headers == nil || len(headers.Returns) != 2 {
		t.Fatal("headers must return table and error")
	}
	if !typ.TypeEquals(headers.Returns[0], typ.NewMap(typ.String, typ.String)) {
		t.Fatalf("headers value = %s, want {[string]: string}", headers.Returns[0])
	}
}

func TestMessageHeaderTypesSupportNilNarrowing(t *testing.T) {
	tc := code.NewTypeChecker(code.TypeCheckConfig{Enabled: true, Strict: true}, nil)

	_, diagnostics, err := tc.Check(`
local queue = require("queue")
local msg = queue.message()
local value = msg:header("correlation_id")
if value then
local normalized: string = value
end
local headers = msg:headers()
local all_value = headers["correlation_id"]
if all_value then
    local normalized: string = all_value
end
`, "queue_header_types.lua", map[string]*io.Manifest{"queue": ModuleTypes()})
	require.NoError(t, err)
	require.False(t, code.HasErrors(diagnostics), "unexpected diagnostics: %v", diagnostics)
}
