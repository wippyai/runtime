// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	systemtopology "github.com/wippyai/runtime/system/topology"
)

func TestRegistryScopeNumericSubtypes(t *testing.T) {
	for _, scope := range []string{"LOCAL", "EVENTUAL", "CONSISTENT", "STRONG"} {
		for _, integer := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/integer=%t", scope, integer), func(t *testing.T) {
				holder := pid.PID{Node: "test", Host: "app", UniqID: "owner"}
				local := systemtopology.NewPIDRegistry()
				global := newFakeScopedRegistry()
				eventual := &fakeEventualRegistry{entries: map[string]pid.PID{}}
				l := newLuaWithScopedRegistries(t, holder, local, global, eventual)
				expr := "process.registry." + scope
				if integer {
					expr = "tonumber(tostring(" + expr + "))"
				}
				require.NoError(t, l.DoString(fmt.Sprintf(`
					local scope = %s
					local ok, err = process.registry.register("numeric-scope", nil, scope)
					assert(ok, tostring(err))
					local owner, lookup_err = process.registry.lookup("numeric-scope", scope)
					assert(owner == %q, tostring(lookup_err))
					local removed, remove_err = process.registry.unregister("numeric-scope", scope)
					assert(removed, tostring(remove_err))
				`, expr, holder.String())))
				_, found := local.LookupLocal("numeric-scope")
				require.False(t, found)
				eventualResult, err := eventual.Lookup(context.Background(), "numeric-scope")
				require.NoError(t, err)
				require.False(t, eventualResult.Found)
				result, err := global.Lookup(context.Background(), "numeric-scope")
				require.NoError(t, err)
				require.False(t, result.Found)
			})
		}
	}
}
