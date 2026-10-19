// SPDX-License-Identifier: MPL-2.0

package code_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/engine"
	processmod "github.com/wippyai/runtime/runtime/lua/modules/process"
)

func TestProcessEventChannelAnnotations(t *testing.T) {
	for _, registration := range []string{"initial", "clone", "late"} {
		for _, annotation := range []string{"Channel<Event>", "Channel<process.Event>", "channel.Channel<process.Event>"} {
			t.Run(registration+"/"+annotation, func(t *testing.T) {
				cfg := code.TypeCheckConfig{Enabled: true, Strict: true}
				var checker *code.TypeChecker
				if registration == "late" {
					checker = code.NewTypeChecker(cfg, []*api.ModuleDef{engine.ChannelModule})
					checker.AddBuiltin(processmod.Module)
				} else {
					checker = code.NewTypeChecker(cfg, []*api.ModuleDef{engine.ChannelModule, processmod.Module})
					if registration == "clone" {
						checker = checker.Clone()
					}
				}
				source := `
type Event = process.Event
type EventChannel = ` + annotation + `
local function wait_for_exit(events: EventChannel)
    return events
end
local events = process.events()
wait_for_exit(events)
return events
`
				_, diagnostics, err := checker.Check(source, "app:channel_annotation", nil)
				require.NoError(t, err)
				require.False(t, code.HasErrors(diagnostics), "equivalent native channel types: %v", diagnostics)

				// Finalizing the declaration must preserve its element contract.
				_, diagnostics, err = checker.Check(`
local function accepts_strings(events: Channel<string>) return events end
accepts_strings(process.events())
`, "app:wrong_channel", nil)
				require.NoError(t, err)
				require.True(t, code.HasErrors(diagnostics), "an event channel must not satisfy Channel<string>")
			})
		}
	}
}
