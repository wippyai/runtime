// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
)

func TestDependencyResolutionDetailsRemainStructuredInLua(t *testing.T) {
	for _, count := range []int{0, 1, 3} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			failures := make([]ResolutionError, count)
			for i := range failures {
				failures[i] = ResolutionError{Org: "acme", Name: fmt.Sprintf("module%d", i), Constraint: "^1.0.0", Message: "no matching version"}
			}
			l := lua.NewState()
			defer l.Close()
			failure := NewDependencyResolutionErrors(failures)
			// Exercise the Lua details converter without changing its global
			// metadata extractor. Native registry tests cover the outer wrapper.
			l.SetGlobal("failure", lua.NewLuaError(l, failure.Error()).WithDetails(map[string]any(failure.Details().(attrs.Bag))))
			require.NoError(t, l.DoString(fmt.Sprintf(`
				local details = failure:details()
				assert(type(details.errors) == "table", "errors must remain an array")
				assert(details.count == %d and #details.errors == %d)
				assert(type(details.summary) == "string")
				for i, row in ipairs(details.errors) do
					assert(type(row) == "table")
					assert(row.module == "acme/module" .. tostring(i - 1))
					assert(row.constraint == "^1.0.0")
					assert(row.message == "no matching version")
				end
			`, count, count)))
		})
	}
}
