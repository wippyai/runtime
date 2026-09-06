// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"testing"

	"github.com/stretchr/testify/require"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
)

func TestAgentSurfaceSDKTypes(t *testing.T) {
	config := code.DefaultTypeCheckConfig()
	config.Enabled = true
	config.SkipUntyped = false
	checker := code.NewTypeChecker(config, []*luaapi.ModuleDef{Module})
	_, diagnostics, err := checker.Check(`
local tty = require("tty")
local owner = tty.viewport({width=120,height=40})
local ref = owner:mount("{remote@agents|agent}", {observe=true,input=true,resize=true})
local view = tty.attach(ref)
view:send({type="key",key="enter",key_type="enter",action="press"})
view:send({type="mouse",action="press",button="left",x=1,y=1})
view:send({type="paste",text="echo hello"})
view:resize(100,30)
local changes = view:updates()
local revision, open = changes:receive()
local update_case = changes:case_receive()
local events = tty.events()
local input_case = events:case_receive()
local event, input_open = events:receive()
local handle, handle_error = owner:handle()
owner:revoke(ref)
view:close()
`, "agent_surface_types.lua", nil)
	require.NoError(t, err)
	require.False(t, code.HasErrors(diagnostics), "SDK rejects documented agent flow: %v", diagnostics)
}

func TestAgentSurfaceSDKRejectsInvalidInput(t *testing.T) {
	config := code.DefaultTypeCheckConfig()
	config.Enabled = true
	config.SkipUntyped = false
	checker := code.NewTypeChecker(config, []*luaapi.ModuleDef{Module})
	_, diagnostics, err := checker.Check(`
 local tty=require("tty")
 local view=tty.attach("ref")
 view:send({type="key",key="enter",key_type="enter",action="click"})
 `, "invalid_surface_input.lua", nil)
	require.NoError(t, err)
	require.True(t, code.HasErrors(diagnostics), "invalid input must be rejected by SDK types")
}
