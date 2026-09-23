// SPDX-License-Identifier: MPL-2.0

package code_test

import (
	"testing"

	"github.com/wippyai/go-lua/types/diag"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/modules/time"
)

// Builtin module types resolve both unqualified and module-qualified, so a
// cast to Channel<Msg> types the received value as Msg.
func TestBuiltinModuleTypesResolveInTypePositions(t *testing.T) {
	tc := code.NewTypeChecker(code.TypeCheckConfig{Enabled: true, Strict: true}, []*luaapi.ModuleDef{
		engine.ChannelModule,
		time.Module,
	})

	for name, channelType := range map[string]string{
		"unqualified": "Channel<Msg>",
		"qualified":   "channel.Channel<Msg>",
	} {
		t.Run(name, func(t *testing.T) {
			source := `
type Msg = { field: string }

local function run(): (string?, string?)
    local ch = channel.new(1) :: ` + channelType + `
    local timeout = time.after("1s")
    local r = channel.select({ ch:case_receive(), timeout:case_receive() })
    if r.channel == timeout then
        return nil, "timeout"
    end
    local msg = r.value
    local wrong: number = msg.field
    return msg.field, nil
end

return { run = run }
`
			_, diags, err := tc.Check(source, "test.lua", nil)
			if err != nil {
				t.Fatalf("check error: %v", err)
			}
			var errs []diag.Diagnostic
			for _, d := range diags {
				if d.Severity == diag.SeverityError {
					errs = append(errs, d)
				}
			}
			if len(errs) != 1 || errs[0].Position.Line != 12 || errs[0].Message != "cannot assign string to number" {
				t.Fatalf("want only the string-to-number error on line 12, got %v", errs)
			}
		})
	}
}
