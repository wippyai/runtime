package code_test

import (
	"testing"

	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/engine"
	processmod "github.com/wippyai/runtime/runtime/lua/modules/process"
	timemod "github.com/wippyai/runtime/runtime/lua/modules/time"
	"github.com/yuin/gopher-lua/types/diag"
)

func TestChannelSelectNarrowing_ProcessEvent(t *testing.T) {
	source := `
local time = require("time")

local function main()
    local events_ch = process.events()
    local timeout = time.after("3s")
    local result = channel.select {
        events_ch:case_receive(),
        timeout:case_receive(),
    }

    if result.channel == timeout then
        return false, "timeout"
    end

    local event = result.value
    local result_value = event.result
    local msg = result_value.value
    return msg
end

return { main = main }
`

	tc := code.NewTypeChecker(code.TypeCheckConfig{Enabled: true, Strict: true}, []*api.ModuleDef{
		engine.ChannelModule,
		processmod.Module,
		timemod.Module,
	})

	_, diags, err := tc.Check(source, "test.lua", nil)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	for _, d := range diags {
		if d.Severity == diag.SeverityError {
			t.Fatalf("unexpected error: %s", d.Message)
		}
	}
}
