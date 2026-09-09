// SPDX-License-Identifier: MPL-2.0

package process

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/runtime/runtime/lua/code"
)

func TestSetOptionsTypeAcceptsPartialUpdates(t *testing.T) {
	tc := code.NewTypeChecker(code.TypeCheckConfig{Enabled: true, Strict: true}, nil)

	_, diagnostics, err := tc.Check(`
local process = require("process")
process.set_options({ trap_links = true })
process.set_options({ upgradable = true })
process.set_options({})

local options = process.get_options()
local trap_links: boolean = options.trap_links
local upgradable: boolean = options.upgradable
`, "process_options_types.lua", map[string]*io.Manifest{"process": ModuleTypes()})
	require.NoError(t, err)
	require.False(t, code.HasErrors(diagnostics), "unexpected diagnostics: %v", diagnostics)
}

func TestTypedListenMessageData(t *testing.T) {
	for _, test := range []struct {
		name, options, expression, want string
		invalid                         bool
	}{
		{"message", "{message = true, type = Request}", "item:data().count", "integer", false},
		{"raw", "{type = Request}", "item.count", "integer", false},
		{"wrong_message_field", "{message = true, type = Request}", "item:data().count", "string", true},
		{"wrong_raw_field", "{type = Request}", "item.count", "string", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			tc := code.NewTypeChecker(code.TypeCheckConfig{Enabled: true, Strict: true}, nil)
			source := `
local process = require("process")
type Request = {count: integer}
local stream, err = process.listen("request", ` + test.options + `)
if not stream then error(tostring(err)) end
local item, ok = stream:receive()
if ok then
 local count: ` + test.want + ` = ` + test.expression + `
end
`
			_, diagnostics, err := tc.Check(source, test.name+".lua", map[string]*io.Manifest{"process": ModuleTypes()})
			require.NoError(t, err)
			require.Equal(t, test.invalid, code.HasErrors(diagnostics), "diagnostics: %v", diagnostics)
		})
	}
}

func TestUntypedListenMessageCompatibility(t *testing.T) {
	tc := code.NewTypeChecker(code.TypeCheckConfig{Enabled: true, Strict: true}, nil)
	_, diagnostics, err := tc.Check(`
local process = require("process")
local messages = process.listen("request", {message = true})
local msg: process.Message = messages:receive()
local sender: string = msg:from()
local topic: string = msg:topic()
local raw = process.listen("raw")
local legacy = process.listen("legacy", {})
local function dynamic(message: boolean)
 return process.listen("dynamic", {message = message})
end
`, "listen_compatibility.lua", map[string]*io.Manifest{"process": ModuleTypes()})
	require.NoError(t, err)
	require.False(t, code.HasErrors(diagnostics), "diagnostics: %v", diagnostics)
}
