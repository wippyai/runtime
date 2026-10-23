// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/process"
)

// Exercise the cached standard library and string metatable used by real actors,
// including a second native call after a protected replacement error.
func TestProcessGsubNativeReplacement(t *testing.T) {
	cases := map[string]string{
		"chained native callback": `
			local label, count = ("home_city"):gsub("_", " "):gsub("^%l", string.upper)
			assert(type(label) == "string" and label == "Home city", tostring(label))
			assert(count == 1)
		`,
		"native captures": `
			local value, count = ("a1 b2"):gsub("(%a)%d", string.upper)
			assert(value == "A B", tostring(value))
			assert(count == 2)
		`,
		"unchanged matches": `
			for _, callback in ipairs({function() return nil end, function() return false end}) do
				local value, count = ("abc"):gsub("b", callback)
				assert(value == "abc" and count == 1)
			end
			local value, count = ("123"):gsub("%a", string.upper)
			assert(value == "123" and count == 0)
		`,
		"protected invalid result": `
			local ok, err = pcall(string.gsub, "abc", "b", function() return true end)
			assert(not ok, "invalid callback result must raise an error")
			assert(tostring(err):find("invalid replacement value", 1, true), tostring(err))
			assert(("a"):gsub("a", string.upper) == "A")
		`,
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			ctx, frame := ctxapi.OpenFrameContext(context.Background())
			defer ctxapi.ReleaseFrameContext(frame)

			factory := NewFactory(FactoryConfig{
				Script:     "return {main = function()\n" + source + "\nend}",
				ScriptName: "gsub_callback.lua",
			})
			actor, err := factory()
			require.NoError(t, err)
			defer actor.Close()
			require.NoError(t, actor.Init(ctx, "main", nil))
			var output process.StepOutput
			require.NoError(t, actor.Step(nil, &output))
			require.Equal(t, process.StepDone, output.Status())
		})
	}
}
