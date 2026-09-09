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

func TestMessageIngressHasStrictNativeTypes(t *testing.T) {
	tc := code.NewTypeChecker(code.TypeCheckConfig{Enabled: true, Strict: true}, nil)
	_, diagnostics, err := tc.Check(`
local process = require("process")
local function inspect(message: process.Message, previous: process.Ingress?): boolean
 local ingress = message:ingress()
 if ingress == nil then return false end
 local node: string = ingress:node()
 local verified: boolean = ingress:authenticated() and ingress:integrity_protected() and ingress:live()
 if previous ~= nil then
  return verified and ingress:same_connection(previous) and node ~= ""
 end
 return verified
end
return inspect
`, "process_ingress_types.lua", map[string]*io.Manifest{"process": ModuleTypes()})
	require.NoError(t, err)
	require.False(t, code.HasErrors(diagnostics), "unexpected diagnostics: %v", diagnostics)
}
