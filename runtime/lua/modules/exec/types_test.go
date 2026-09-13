// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"testing"

	"github.com/stretchr/testify/require"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/engine"
)

func TestTerminalCompletionPreservesSelectValueType(t *testing.T) {
	config := code.DefaultTypeCheckConfig()
	config.Enabled = true
	config.SkipUntyped = false
	checker := code.NewTypeChecker(config, []*luaapi.ModuleDef{engine.ChannelModule, Module})
	_, diagnostics, err := checker.Check(`
local channel = require("channel")
local exec = require("exec")

local function completion_value(session: exec.TerminalSession): boolean
    local done = session:done()
    local selected = channel.select({done:case_receive()})
    if selected.channel == done then
        local completed: boolean = selected.value
        return completed
    end
    return false
end

return completion_value
`, "terminal_completion_select_types.lua", nil)
	require.NoError(t, err)
	require.False(t, code.HasErrors(diagnostics), "terminal completion select type was lost: %v", diagnostics)
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
