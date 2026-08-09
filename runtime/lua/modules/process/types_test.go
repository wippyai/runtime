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
