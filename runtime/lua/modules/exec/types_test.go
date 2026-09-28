// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"testing"

	"github.com/stretchr/testify/require"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/engine"
)

func TestTerminalProcessResultPreservesSelectValueType(t *testing.T) {
	config := code.DefaultTypeCheckConfig()
	config.Enabled = true
	config.SkipUntyped = false
	checker := code.NewTypeChecker(config, []*luaapi.ModuleDef{engine.ChannelModule, Module})
	_, diagnostics, err := checker.Check(`
local channel = require("channel")
local exec = require("exec")

local function completion_value(terminal: exec.TerminalProcess): integer
    local done = terminal:done()
    local selected = channel.select({done:case_receive()})
    if selected.channel == done then
        local result: exec.TerminalResult = selected.value
        if result.exit then return result.exit.code end
    end
    return 0
end

return completion_value
`, "terminal_result_select_types.lua", nil)
	require.NoError(t, err)
	require.False(t, code.HasErrors(diagnostics), "terminal result select type was lost: %v", diagnostics)
}

func TestProcessMountOptionsAreTyped(t *testing.T) {
	config := code.DefaultTypeCheckConfig()
	config.Enabled = true
	config.SkipUntyped = false
	checker := code.NewTypeChecker(config, []*luaapi.ModuleDef{Module})
	_, diagnostics, err := checker.Check(`
local exec = require("exec")
local function options() : exec.ProcessOptions
    return {mounts = {{source = "/host", target = "/workspace", read_only = true}}}
end
return options
`, "process_mount_options.lua", nil)
	require.NoError(t, err)
	require.False(t, code.HasErrors(diagnostics), "process mount options type was rejected: %v", diagnostics)
}

func TestProcessConfinementOptionsAreTyped(t *testing.T) {
	config := code.DefaultTypeCheckConfig()
	config.Enabled = true
	config.SkipUntyped = false
	checker := code.NewTypeChecker(config, []*luaapi.ModuleDef{Module})
	_, diagnostics, err := checker.Check(`
local exec = require("exec")
local confine: exec.ConfinementPatch = {
    fs = {read = {"/workspace", "{tmp}"}, write = {"{tmp}"}, exec = {}},
    env = {allow = {"PATH"}},
    network = "none",
    limits = {mem_mb = 128, pids = 16, wall_s = 30},
    tree = {kill_on_owner_exit = true},
}
local function options() : exec.ProcessOptions
    return {confine = confine}
end
return options
`, "process_confinement_options.lua", nil)
	require.NoError(t, err)
	require.False(t, code.HasErrors(diagnostics), "process confinement options type was rejected: %v", diagnostics)
}

func TestProcessConfinementTypesRejectInvalidNarrowingOption(t *testing.T) {
	config := code.DefaultTypeCheckConfig()
	config.Enabled = true
	config.SkipUntyped = false
	checker := code.NewTypeChecker(config, []*luaapi.ModuleDef{Module})
	_, diagnostics, err := checker.Check(`
local exec = require("exec")
local options: exec.ProcessOptions = {
    confine = {network = "host"}
}
return options
`, "process_confinement_invalid_network.lua", nil)
	require.NoError(t, err)
	require.True(t, code.HasErrors(diagnostics), "invalid confinement option was accepted: %v", diagnostics)
}
